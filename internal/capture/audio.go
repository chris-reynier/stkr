// Package capture holds the building blocks of idea capture: recording with
// ffmpeg, transcription with whisper, the phone inbox scanner and bookmark
// title fetching. It has no knowledge of the database; the service wires it.
package capture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// MaxRecording caps one recording; ffmpeg stops by itself after this long, so
// a forgotten recording cannot fill the disk.
const MaxRecording = 30 * time.Minute

// Recorder records microphone audio to a WAV file.
type Recorder interface {
	// Start begins recording into outPath. It returns domain.ErrUnavailable
	// when the recorder program is missing.
	Start(ctx context.Context, outPath string) error
	// Stop finalizes the file. It is safe to call when not recording.
	Stop() error
	// Recording reports whether a recording is running.
	Recording() bool
}

// Which reports whether the program bin can be found (a path or a name on PATH).
func Which(bin string) bool {
	if strings.TrimSpace(bin) == "" {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func unavailable(format string, a ...any) error {
	return domain.New(domain.ErrUnavailable, fmt.Sprintf(format, a...))
}

// tail is a bounded buffer that keeps the last bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 1024 {
		t.buf = t.buf[len(t.buf)-1024:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// FFmpegRecorder records with ffmpeg: DirectShow on Windows, PulseAudio on
// Linux and AVFoundation on macOS. Device is the mic_device setting.
type FFmpegRecorder struct {
	Bin    string
	Device string
	// StopGrace is how long Stop waits for ffmpeg to finish after "q" before
	// killing it (default 3 s).
	StopGrace time.Duration

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan struct{}
	cancel context.CancelFunc
	errOut *tail
}

// NewFFmpegRecorder returns a recorder using the ffmpeg program bin.
func NewFFmpegRecorder(bin, device string) *FFmpegRecorder {
	return &FFmpegRecorder{Bin: bin, Device: device, StopGrace: 3 * time.Second}
}

// inputArgs returns the ffmpeg input arguments for the current OS.
func (r *FFmpegRecorder) inputArgs() ([]string, error) {
	switch runtime.GOOS {
	case "windows":
		if r.Device == "" {
			return nil, domain.New(domain.ErrInvalid, "set mic_device to your microphone name (list devices with: ffmpeg -list_devices true -f dshow -i dummy)")
		}
		return []string{"-f", "dshow", "-i", "audio=" + r.Device}, nil
	case "darwin":
		dev := r.Device
		if dev == "" {
			dev = "0"
		}
		return []string{"-f", "avfoundation", "-i", ":" + dev}, nil
	}
	dev := r.Device
	if dev == "" {
		dev = "default"
	}
	return []string{"-f", "pulse", "-i", dev}, nil
}

// Start implements Recorder.
func (r *FFmpegRecorder) Start(_ context.Context, outPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return domain.New(domain.ErrConflict, "already recording")
	}
	if !Which(r.Bin) {
		return unavailable("ffmpeg not found (%q): install it or set the ffmpeg config key", r.Bin)
	}
	in, err := r.inputArgs()
	if err != nil {
		return err
	}
	args := append([]string{"-y", "-loglevel", "error", "-t", fmt.Sprint(int(MaxRecording.Seconds()))}, in...)
	args = append(args, "-ac", "1", "-ar", "16000", outPath)
	// The process outlives the request, so it has its own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), MaxRecording+time.Minute)
	cmd := exec.CommandContext(ctx, r.Bin, args...) //nolint:gosec // argv only, program from config
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return err
	}
	errOut := &tail{}
	cmd.Stderr = errOut
	if err := cmd.Start(); err != nil {
		cancel()
		return unavailable("cannot start ffmpeg: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	r.cmd, r.stdin, r.done, r.cancel, r.errOut = cmd, stdin, done, cancel, errOut
	return nil
}

// Stop implements Recorder: "q" on stdin makes ffmpeg write the WAV header
// and exit; it is killed after StopGrace.
func (r *FFmpegRecorder) Stop() error {
	r.mu.Lock()
	cmd, stdin, done, cancel, errOut := r.cmd, r.stdin, r.done, r.cancel, r.errOut
	r.cmd = nil
	r.mu.Unlock()
	if cmd == nil {
		return nil
	}
	defer cancel()
	_, _ = stdin.Write([]byte("q"))
	_ = stdin.Close()
	grace := r.StopGrace
	if grace <= 0 {
		grace = 3 * time.Second
	}
	killed := false
	select {
	case <-done:
	case <-time.After(grace):
		killed = true
		cancel()
		<-done
	}
	if msg := errOut.String(); msg != "" && !killed && cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		return fmt.Errorf("ffmpeg: %s", firstLine(msg))
	}
	return nil
}

// Recording implements Recorder.
func (r *FFmpegRecorder) Recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// Transcriber turns a WAV file into text.
type Transcriber interface {
	Transcribe(ctx context.Context, wavPath string) (string, error)
}

// Whisper runs a whisper.cpp compatible program (whisper-cli).
type Whisper struct {
	Bin     string
	Model   string
	Timeout time.Duration // default 5 minutes
}

// Transcribe implements Transcriber. The text is read from standard output.
// A missing program is domain.ErrUnavailable. The transcript is never put in
// an error message.
func (w Whisper) Transcribe(ctx context.Context, wavPath string) (string, error) {
	if !Which(w.Bin) {
		return "", unavailable("whisper not found (%q): install whisper.cpp or set whisper.bin", w.Bin)
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{}
	if w.Model != "" {
		args = append(args, "-m", w.Model)
	}
	args = append(args, "-f", wavPath, "-nt", "-np")
	cmd := exec.CommandContext(ctx, w.Bin, args...) //nolint:gosec // argv only, program from config
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("whisper exited with status %d", ee.ExitCode())
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("whisper: %w", ctx.Err())
		}
		return "", fmt.Errorf("whisper: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// ConvertToWAV converts any audio file to 16 kHz mono WAV with ffmpeg.
func ConvertToWAV(ctx context.Context, ffmpeg, src, dst string) error {
	if !Which(ffmpeg) {
		return unavailable("ffmpeg not found (%q): install it or set the ffmpeg config key", ffmpeg)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-loglevel", "error", "-i", src, "-ac", "1", "-ar", "16000", dst) //nolint:gosec // argv only
	if err := cmd.Run(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("ffmpeg could not convert the audio: %w", err)
	}
	return nil
}
