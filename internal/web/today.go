package web

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func init() {
	Register(Page{ID: "today", Title: "Today", Path: "/ui/today", Order: 10, Mount: mountToday})
}

type eventRow struct {
	Event  domain.Event
	Label  string
	Block  *domain.Block
	NextID int64 // the following event on the same day, 0 if none
}

type todayData struct {
	Rep        domain.DayReport
	IsToday    bool
	Prev, Next string
	Self       string // page URL; forms redirect back here
	Now        domain.NowStatus
	Rows       []eventRow
	Contexts   []domain.Context // all, for the edit select
	Switchable []domain.Context // active, not "off"
}

func mountToday(ui *UI) {
	ui.Get("GET /ui/today", ui.today)
	ui.Post("POST /ui/switch", func(r *http.Request, _ string) (string, error) {
		_, err := ui.Svc.Switch(r.Context(), r.PostFormValue("ctx"), domain.SourceUI, "")
		return "", err
	})
	ui.Post("POST /ui/stop", func(r *http.Request, _ string) (string, error) {
		_, err := ui.Svc.Stop(r.Context(), domain.SourceUI)
		return "", err
	})
	ui.Post("POST /ui/event/{id}/edit", ui.eventEdit)
	ui.Post("POST /ui/event/{id}/split", ui.eventSplit)
	ui.Post("POST /ui/event/{id}/merge", func(r *http.Request, _ string) (string, error) {
		id, err := eventID(r)
		if err != nil {
			return "", err
		}
		other, err := strconv.ParseInt(r.PostFormValue("other"), 10, 64)
		if err != nil {
			return "", invalidf("invalid event to merge with")
		}
		if _, err = ui.Svc.MergeEvents(r.Context(), id, other); err != nil {
			return "", err
		}
		return "Blocks merged.", nil
	})
	ui.Post("POST /ui/event/{id}/delete", func(r *http.Request, _ string) (string, error) {
		id, err := eventID(r)
		if err != nil {
			return "", err
		}
		if err := ui.Svc.DeleteEvent(r.Context(), id); err != nil {
			return "", err
		}
		return "Event deleted.", nil
	})
	ui.Post("POST /ui/event/{id}/confirm", func(r *http.Request, _ string) (string, error) {
		id, err := eventID(r)
		if err != nil {
			return "", err
		}
		_, err = ui.Svc.ConfirmEvent(r.Context(), id, r.PostFormValue("confirmed") == "1")
		return "", err
	})
}

func invalidf(msg string) error { return domain.New(domain.ErrInvalid, msg) }

func eventID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidf("invalid event id")
	}
	return id, nil
}

// formTime combines the form's date (YYYY-MM-DD) and time (HH:MM) in the
// configured time zone.
func (u *UI) formTime(r *http.Request) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", r.PostFormValue("date")+" "+r.PostFormValue("time"), u.Svc.Location())
	if err != nil {
		return time.Time{}, invalidf("invalid time (want HH:MM)")
	}
	return t, nil
}

func (u *UI) eventEdit(r *http.Request, _ string) (string, error) {
	id, err := eventID(r)
	if err != nil {
		return "", err
	}
	p := service.EventPatch{}
	// Only touch the timestamp when the user changed the minute, so a note
	// edit keeps the exact original time.
	if r.PostFormValue("time") != r.PostFormValue("orig_time") {
		t, err := u.formTime(r)
		if err != nil {
			return "", err
		}
		p.TS = &t
	}
	if c := r.PostFormValue("context"); c != "" {
		p.ContextID = &c
	}
	note := r.PostFormValue("note")
	p.Note = &note
	if _, err = u.Svc.EditEvent(r.Context(), id, p); err != nil {
		return "", err
	}
	return "Event saved.", nil
}

func (u *UI) eventSplit(r *http.Request, _ string) (string, error) {
	id, err := eventID(r)
	if err != nil {
		return "", err
	}
	t, err := u.formTime(r)
	if err != nil {
		return "", err
	}
	if _, err = u.Svc.SplitEvent(r.Context(), id, t); err != nil {
		return "", err
	}
	return "Block split.", nil
}

func (u *UI) today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rep, err := u.Svc.Day(ctx, r.URL.Query().Get("d"))
	if err != nil {
		u.Fail(w, err)
		return
	}
	day, _ := u.Svc.ParseDate(rep.Date)
	todayDate, _ := u.Svc.ParseDate("today")
	d := todayData{
		Rep:     rep,
		IsToday: rep.Date == todayDate.Format("2006-01-02"),
		Prev:    day.AddDate(0, 0, -1).Format("2006-01-02"),
		Next:    day.AddDate(0, 0, 1).Format("2006-01-02"),
		Self:    "/ui/today?d=" + url.QueryEscape(rep.Date),
	}
	if d.Now, err = u.Svc.Now(ctx); err != nil {
		u.Fail(w, err)
		return
	}
	if d.Contexts, err = u.Svc.ListContexts(ctx, service.ContextFilter{IncludeParked: true}); err != nil {
		u.Fail(w, err)
		return
	}
	labels := map[string]string{}
	for _, c := range d.Contexts {
		labels[c.ID] = c.Label
		if c.Active && c.ID != domain.ContextOff {
			d.Switchable = append(d.Switchable, c)
		}
	}
	blocks := map[int64]*domain.Block{}
	for i := range rep.Blocks {
		blocks[rep.Blocks[i].EventID] = &rep.Blocks[i]
	}
	for i, e := range rep.Events {
		row := eventRow{Event: e, Label: labels[e.ContextID], Block: blocks[e.ID]}
		if row.Label == "" {
			row.Label = e.ContextID
		}
		if i+1 < len(rep.Events) {
			row.NextID = rep.Events[i+1].ID
		}
		d.Rows = append(d.Rows, row)
	}
	u.Render(w, r, "today", "Today", d)
}
