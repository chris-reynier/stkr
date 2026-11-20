package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

// Limits of captured text.
const (
	MaxIdeaRunes  = 10000
	MaxTitleRunes = 200
)

// Idea sources stored in ideas.source.
const (
	IdeaSourceText    = "text"
	IdeaSourceVoice   = "voice"
	IdeaSourcePhone   = "phone"
	IdeaSourceMeeting = "meeting"
)

// MeetingOrigin is items.origin for actions and decisions captured with a type.
const MeetingOrigin = "meeting"

// meetingLookback is how far back the project before a meeting is searched.
const meetingLookback = 14 * 24 * time.Hour

// OtherContext is the catch-all project.
const OtherContext = "other"

type captureState struct {
	audioDir     string
	recorder     capture.Recorder
	transcriber  capture.Transcriber
	titleTimeout time.Duration

	mu      sync.Mutex // guards recPath and recTag
	recPath string
	recTag  string

	inboxMu     sync.Mutex
	inboxDone   map[string]bool // ingested but not yet moved
	inboxWarned map[string]bool

	wg     sync.WaitGroup
	bg     context.Context
	cancel context.CancelFunc
}

func newCaptureState(cfg *config.Config, d Deps) *captureState {
	c := &captureState{
		audioDir:     d.AudioDir,
		recorder:     d.Recorder,
		transcriber:  d.Transcriber,
		titleTimeout: d.TitleTimeout,
		inboxDone:    map[string]bool{},
		inboxWarned:  map[string]bool{},
	}
	if c.recorder == nil {
		c.recorder = capture.NewFFmpegRecorder(cfg.FFmpeg, cfg.MicDevice)
	}
	if c.transcriber == nil {
		c.transcriber = capture.Whisper{Bin: cfg.Whisper.Bin, Model: cfg.Whisper.Model}
	}
	if c.titleTimeout <= 0 {
		c.titleTimeout = capture.TitleTimeout
	}
	c.bg, c.cancel = context.WithCancel(context.Background())
	return c
}

// stop ends a running recording, cancels background transcriptions and waits
// for them.
func (c *captureState) stop() {
	_ = c.recorder.Stop()
	c.cancel()
	c.wg.Wait()
}

// WaitCapture blocks until background transcriptions have finished (tests).
func (s *Service) WaitCapture() { s.capt.wg.Wait() }

// IdeaInput is a text capture. Type is empty for a plain idea, or action,
// decision or note for meeting capture.
type IdeaInput struct {
	Text string
	Tag  string // pro (default) or personal
	Type string
	Via  string // api, cli, ui or deck; stored on meeting items
}

// IdeaResult is what a capture created: an idea, or a meeting item.
type IdeaResult struct {
	Idea *domain.Idea `json:"idea,omitempty"`
	Item *domain.Item `json:"item,omitempty"`
	// Created is false when the item already existed (same fingerprint).
	Created bool `json:"created"`
}

func checkTag(tag string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "", capture.TagPro:
		return capture.TagPro, nil
	case capture.TagPersonal:
		return capture.TagPersonal, nil
	}
	return "", invalid("tag must be pro or personal (got %q)", tag)
}

// AddIdea records a text idea, or a meeting action, decision or note. The
// capture is written first and never waits for AI or other side effects. The
// text is never logged.
func (s *Service) AddIdea(ctx context.Context, in IdeaInput) (IdeaResult, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return IdeaResult{}, invalid("text is empty")
	}
	if utf8.RuneCountInString(text) > MaxIdeaRunes {
		return IdeaResult{}, invalid("text is longer than %d characters", MaxIdeaRunes)
	}
	tag, err := checkTag(in.Tag)
	if err != nil {
		return IdeaResult{}, err
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	switch typ {
	case "", string(domain.ItemNote):
	case string(domain.ItemAction), string(domain.ItemDecision):
		if tag == capture.TagPersonal {
			return IdeaResult{}, invalid("meeting actions and decisions cannot be personal")
		}
		return s.addMeetingItem(ctx, domain.ItemType(typ), text, in.Via)
	default:
		return IdeaResult{}, invalid("type must be action, decision or note (got %q)", in.Type)
	}
	src := IdeaSourceText
	if typ == string(domain.ItemNote) {
		src = IdeaSourceMeeting
	}
	idea, err := s.insertIdea(ctx, domain.Idea{Tag: tag, RawText: text, Source: src})
	if err != nil {
		return IdeaResult{}, err
	}
	return IdeaResult{Idea: &idea, Created: true}, nil
}

// insertIdea stamps the time and the current context, then stores idea.
func (s *Service) insertIdea(ctx context.Context, idea domain.Idea) (domain.Idea, error) {
	now := s.clk.Now()
	idea.TS = now
	if last, err := s.st.LastEvent(ctx, now); err == nil && last.ContextID != domain.ContextOff {
		idea.ContextID = last.ContextID
	}
	idea, err := s.st.InsertIdea(ctx, idea)
	if err != nil {
		return idea, err
	}
	idea.TS = idea.TS.In(s.loc)
	s.log.Info("idea captured", "id", idea.ID, "tag", idea.Tag, "source", idea.Source, "chars", utf8.RuneCountInString(idea.RawText))
	return idea, nil
}

// meetingProject is the last project context tracked before now (the project
// a meeting belongs to), or "other".
func (s *Service) meetingProject(ctx context.Context) (string, error) {
	now := s.clk.Now()
	evs, err := s.st.EventsBetween(ctx, now.Add(-meetingLookback), now.Add(time.Minute))
	if err != nil {
		return "", err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return "", err
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if c, ok := ctxs[evs[i].ContextID]; ok && c.Kind == domain.KindProject {
			return c.ID, nil
		}
	}
	return OtherContext, nil
}

func (s *Service) addMeetingItem(ctx context.Context, typ domain.ItemType, text, via string) (IdeaResult, error) {
	if utf8.RuneCountInString(text) > daily.MaxSummaryRunes {
		return IdeaResult{}, invalid("action and decision text is longer than %d characters", daily.MaxSummaryRunes)
	}
	project, err := s.meetingProject(ctx)
	if err != nil {
		return IdeaResult{}, err
	}
	if via == "" {
		via = ViaAPI
	}
	it, created, err := s.st.InsertItem(ctx, domain.Item{
		Date: s.todayDate(), Project: project, Type: typ, Summary: text, Source: via,
		Origin: MeetingOrigin, Fingerprint: daily.Fingerprint(typ, project, text),
	})
	if err != nil {
		return IdeaResult{}, err
	}
	s.log.Info("meeting item captured", "id", it.ID, "type", string(typ), "project", project, "created", created, "chars", utf8.RuneCountInString(text))
	return IdeaResult{Item: &it, Created: created}, nil
}

// ---- voice ----

// RecordResult is the outcome of a record toggle.
type RecordResult struct {
	Recording bool         `json:"recording"`
	Idea      *domain.Idea `json:"idea,omitempty"` // set when a recording was stopped
}

// Recording reports whether a voice recording is running in this process.
func (s *Service) Recording() bool { return s.capt.recorder.Recording() }

// audioName returns an unused file name in the audio directory.
func (s *Service) audioName(prefix, stem string) string {
	ts := s.clk.Now().UTC().Format("20060102T150405Z")
	base := prefix + "-" + ts
	if stem = sanitizeStem(stem); stem != "" {
		base += "-" + stem
	}
	name := base + ".wav"
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(s.capt.audioDir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d.wav", base, n)
	}
}

var stemRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeStem(s string) string {
	s = strings.Trim(stemRe.ReplaceAllString(s, "-"), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// ToggleRecording starts a voice recording, or stops the running one. On
// stop the idea row is written at once (empty text, audio path set) and the
// transcription runs in the background; a missing whisper leaves the idea
// with ai_state skipped. tag applies to the next recording only (pro or
// personal). A missing ffmpeg is domain.ErrUnavailable.
func (s *Service) ToggleRecording(ctx context.Context, tag string) (RecordResult, error) {
	c := s.capt
	if c.audioDir == "" {
		return RecordResult{}, domain.New(domain.ErrUnavailable, "voice capture is not available: no audio folder")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.recorder.Recording() {
		t, err := checkTag(tag)
		if err != nil {
			return RecordResult{}, err
		}
		if err := os.MkdirAll(c.audioDir, 0o700); err != nil {
			return RecordResult{}, fmt.Errorf("create audio folder: %w", err)
		}
		path := filepath.Join(c.audioDir, s.audioName("idea", ""))
		if err := c.recorder.Start(ctx, path); err != nil {
			return RecordResult{}, err
		}
		c.recPath, c.recTag = path, t
		s.log.Info("recording started")
		return RecordResult{Recording: true}, nil
	}
	stopErr := c.recorder.Stop()
	path, tg := c.recPath, c.recTag
	c.recPath = ""
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		_ = os.Remove(path)
		msg := "no audio was recorded: check mic_device and the ffmpeg setting"
		if stopErr != nil {
			msg += " (" + stopErr.Error() + ")"
		}
		return RecordResult{}, domain.New(domain.ErrUnavailable, msg)
	}
	idea, err := s.insertIdea(ctx, domain.Idea{Tag: tg, AudioPath: filepath.Base(path), Source: IdeaSourceVoice})
	if err != nil {
		return RecordResult{}, err
	}
	s.transcribeAsync(idea.ID, path)
	return RecordResult{Idea: &idea}, nil
}

// transcribeAsync transcribes wavPath and writes the text into the idea.
func (s *Service) transcribeAsync(id int64, wavPath string) {
	c := s.capt
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		text, err := c.transcriber.Transcribe(c.bg, wavPath)
		if err != nil {
			s.log.Warn("transcription failed", "idea", id, "err", err)
		}
		if utf8.RuneCountInString(text) > MaxIdeaRunes {
			text = string([]rune(text)[:MaxIdeaRunes])
		}
		ctx := context.WithoutCancel(c.bg)
		err = s.st.InTx(ctx, func(tx *store.Store) error {
			idea, err := tx.GetIdea(ctx, id)
			if err != nil {
				return err
			}
			if text == "" {
				idea.AIState = domain.AISkipped
			} else {
				idea.RawText = text
			}
			return tx.UpdateIdea(ctx, idea)
		})
		if err != nil {
			s.log.Error("save transcription", "idea", id, "err", err)
			return
		}
		s.log.Info("transcribed", "idea", id, "chars", utf8.RuneCountInString(text))
	}()
}

// ---- phone inbox ----

// InboxResult counts the files handled by one scan.
type InboxResult struct {
	Ingested int `json:"ingested"`
	Failed   int `json:"failed"`
}

// ScanInbox takes the ready files of the phone inbox: text files become text
// ideas, audio is converted to WAV and goes through the voice pipeline. Each
// handled file is moved to processed/. A file that cannot be handled stays in
// place and is retried at the next scan. It does nothing when phone_inbox is
// empty.
func (s *Service) ScanInbox(ctx context.Context) (InboxResult, error) {
	var res InboxResult
	dir := s.cfg.PhoneInbox
	if dir == "" {
		return res, nil
	}
	files, err := capture.ScanInbox(dir, s.clk.Now())
	if err != nil {
		return res, fmt.Errorf("phone inbox: %w", err)
	}
	c := s.capt
	c.inboxMu.Lock()
	defer c.inboxMu.Unlock()
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		if !c.inboxDone[f.Path] {
			if err := s.ingestFile(ctx, f); err != nil {
				res.Failed++
				if !c.inboxWarned[f.Path] {
					c.inboxWarned[f.Path] = true
					s.log.Warn("phone inbox: file not processed", "ext", filepath.Ext(f.Path), "err", err)
				}
				continue
			}
			c.inboxDone[f.Path] = true
		}
		if _, err := capture.MoveProcessed(dir, f.Path); err != nil {
			s.log.Warn("phone inbox: cannot move file", "ext", filepath.Ext(f.Path), "err", err)
			res.Failed++
			continue
		}
		delete(c.inboxDone, f.Path)
		delete(c.inboxWarned, f.Path)
		res.Ingested++
	}
	return res, nil
}

func (s *Service) ingestFile(ctx context.Context, f capture.InboxFile) error {
	if !f.Audio {
		text, err := capture.ReadText(f.Path)
		if err != nil {
			return err
		}
		if text == "" {
			return nil // nothing to capture; the file is still moved away
		}
		_, err = s.insertIdea(ctx, domain.Idea{Tag: f.Tag, RawText: text, Source: IdeaSourcePhone})
		return err
	}
	if s.capt.audioDir == "" {
		return domain.New(domain.ErrUnavailable, "no audio folder")
	}
	if err := os.MkdirAll(s.capt.audioDir, 0o700); err != nil {
		return err
	}
	stem := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	name := s.audioName("phone", stem)
	dst := filepath.Join(s.capt.audioDir, name)
	if err := capture.ConvertToWAV(ctx, s.cfg.FFmpeg, f.Path, dst); err != nil {
		return err
	}
	idea, err := s.insertIdea(ctx, domain.Idea{Tag: f.Tag, AudioPath: name, Source: IdeaSourcePhone})
	if err != nil {
		return err
	}
	s.transcribeAsync(idea.ID, dst)
	return nil
}

// ---- health ----

// CaptureHealth reports which capture helpers are usable.
type CaptureHealth struct {
	FFmpeg     string `json:"ffmpeg"`      // ok | missing
	Whisper    string `json:"whisper"`     // ok | missing
	PhoneInbox string `json:"phone_inbox"` // ok | disabled | error
	Recording  bool   `json:"recording"`
}

// CaptureHealth checks the configured programs and the phone inbox folder.
func (s *Service) CaptureHealth(context.Context) CaptureHealth {
	h := CaptureHealth{FFmpeg: "missing", Whisper: "missing", PhoneInbox: "disabled", Recording: s.Recording()}
	if capture.Which(s.cfg.FFmpeg) {
		h.FFmpeg = "ok"
	}
	if capture.Which(s.cfg.Whisper.Bin) {
		h.Whisper = "ok"
	}
	if dir := s.cfg.PhoneInbox; dir != "" {
		h.PhoneInbox = "ok"
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			h.PhoneInbox = "error"
		}
	}
	return h
}

// ---- triage ----

// IdeaFilter selects ideas. Empty fields match everything.
type IdeaFilter struct {
	Status domain.IdeaStatus
	Tag    string
}

// ListIdeas returns ideas, newest first.
func (s *Service) ListIdeas(ctx context.Context, f IdeaFilter) ([]domain.Idea, error) {
	if f.Status != "" && !f.Status.Valid() {
		return nil, invalid("status must be one of new, triaged, done, archived (got %q)", f.Status)
	}
	if f.Tag != "" {
		t, err := checkTag(f.Tag)
		if err != nil {
			return nil, err
		}
		f.Tag = t
	}
	all, err := s.st.ListIdeas(ctx, f.Status)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, i := range all {
		if f.Tag == "" || i.Tag == f.Tag {
			i.TS = i.TS.In(s.loc)
			out = append(out, i)
		}
	}
	return out, nil
}

// GetIdea returns one idea.
func (s *Service) GetIdea(ctx context.Context, id int64) (domain.Idea, error) {
	i, err := s.st.GetIdea(ctx, id)
	i.TS = i.TS.In(s.loc)
	return i, err
}

// IdeaPatch changes the triage fields of an idea; nil fields are kept.
// Project is a project context id ("" clears it).
type IdeaPatch struct {
	Status  *domain.IdeaStatus
	Project *string
	Tag     *string
	Title   *string
}

// TriageIdea updates status, project, tag and title of an idea.
func (s *Service) TriageIdea(ctx context.Context, id int64, p IdeaPatch) (domain.Idea, error) {
	var out domain.Idea
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		i, err := tx.GetIdea(ctx, id)
		if err != nil {
			return err
		}
		if p.Status != nil {
			if !p.Status.Valid() {
				return invalid("status must be one of new, triaged, done, archived (got %q)", *p.Status)
			}
			i.Status = *p.Status
		}
		if p.Tag != nil {
			if i.Tag, err = checkTag(*p.Tag); err != nil {
				return err
			}
		}
		if p.Title != nil {
			t := strings.TrimSpace(*p.Title)
			if utf8.RuneCountInString(t) > MaxTitleRunes {
				return invalid("title is longer than %d characters", MaxTitleRunes)
			}
			i.Title = t
		}
		if p.Project != nil {
			proj := strings.TrimSpace(*p.Project)
			if proj != "" {
				c, err := tx.GetContext(ctx, proj)
				if errors.Is(err, domain.ErrNotFound) || (err == nil && c.Kind != domain.KindProject) {
					return invalid("%q is not a project context", proj)
				}
				if err != nil {
					return err
				}
			}
			i.SuggestedProject = proj
		}
		if err := tx.UpdateIdea(ctx, i); err != nil {
			return err
		}
		out = i
		return nil
	})
	out.TS = out.TS.In(s.loc)
	return out, err
}

// IdeasInboxCount is the number of untriaged (status new) pro ideas, shown
// on the ideas-inbox key.
func (s *Service) IdeasInboxCount(ctx context.Context) (int, error) {
	list, err := s.st.ListIdeas(ctx, domain.IdeaNew)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, i := range list {
		if i.Tag == capture.TagPro {
			n++
		}
	}
	return n, nil
}

// AudioFile returns the path of an idea's recording. The path is checked to
// be a file inside the audio folder, whatever the database holds.
func (s *Service) AudioFile(ctx context.Context, id int64) (string, error) {
	i, err := s.st.GetIdea(ctx, id)
	if err != nil {
		return "", err
	}
	missing := domain.New(domain.ErrNotFound, "this idea has no audio")
	if i.AudioPath == "" || s.capt.audioDir == "" {
		return "", missing
	}
	root, err := filepath.EvalSymlinks(s.capt.audioDir)
	if err != nil {
		return "", missing
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, i.AudioPath))
	if err != nil {
		return "", missing
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", missing
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", missing
	}
	return p, nil
}
