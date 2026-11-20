package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func init() {
	Register(Page{ID: "ideas", Title: "Ideas", Path: "/ui/ideas", Order: 40, Mount: mountIdeas})
	Register(Page{ID: "links", Title: "Links", Path: "/ui/links", Order: 50, Mount: mountLinks})
}

type ideasData struct {
	Ideas    []domain.Idea
	Projects []domain.Context
	Health   service.CaptureHealth
	Status   string
	Tag      string
	Self     string
	Statuses []domain.IdeaStatus
	NewCount int
}

func formID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PostFormValue(name), 10, 64)
	if err != nil {
		return 0, domain.New(domain.ErrInvalid, "invalid id")
	}
	return id, nil
}

func mountIdeas(ui *UI) {
	ui.Get("GET /ui/ideas", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := r.URL.Query()
		d := ideasData{Status: q.Get("status"), Tag: q.Get("tag"), Statuses: domain.IdeaStatuses}
		v := url.Values{}
		if d.Status != "" {
			v.Set("status", d.Status)
		}
		if d.Tag != "" {
			v.Set("tag", d.Tag)
		}
		d.Self = "/ui/ideas"
		if len(v) > 0 {
			d.Self += "?" + v.Encode()
		}
		var err error
		if d.Ideas, err = ui.Svc.ListIdeas(ctx, service.IdeaFilter{Status: domain.IdeaStatus(d.Status), Tag: d.Tag}); err != nil {
			ui.Fail(w, err)
			return
		}
		cs, err := ui.Svc.ListContexts(ctx, service.ContextFilter{Kind: domain.KindProject})
		if err != nil {
			ui.Fail(w, err)
			return
		}
		d.Projects = cs
		d.Health = ui.Svc.CaptureHealth(ctx)
		if d.NewCount, err = ui.Svc.IdeasInboxCount(ctx); err != nil {
			ui.Fail(w, err)
			return
		}
		ui.Render(w, r, "ideas", "Ideas", d)
	})
	ui.Get("GET /ui/ideas/{id}/audio", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path, err := ui.Svc.AudioFile(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	ui.Post("POST /ui/ideas/add", func(r *http.Request, _ string) (string, error) {
		tag := ""
		if r.PostFormValue("personal") == "1" {
			tag = "personal"
		}
		res, err := ui.Svc.AddIdea(r.Context(), service.IdeaInput{Text: r.PostFormValue("text"), Tag: tag, Type: r.PostFormValue("type"), Via: service.ViaUI})
		if err != nil {
			return "", err
		}
		if res.Item != nil {
			return "Saved " + string(res.Item.Type) + ".", nil
		}
		return "Idea saved.", nil
	})
	ui.Post("POST /ui/ideas/record", func(r *http.Request, _ string) (string, error) {
		res, err := ui.Svc.ToggleRecording(r.Context(), r.PostFormValue("tag"))
		if err != nil {
			return "", err
		}
		if res.Recording {
			return "Recording. Press the button again to stop.", nil
		}
		return "Recording saved. It will be transcribed in the background.", nil
	})
	ui.Post("POST /ui/ideas/triage", func(r *http.Request, _ string) (string, error) {
		id, err := formID(r, "id")
		if err != nil {
			return "", err
		}
		st := domain.IdeaStatus(r.PostFormValue("status"))
		proj, tag, title := r.PostFormValue("project"), r.PostFormValue("tag"), r.PostFormValue("title")
		_, err = ui.Svc.TriageIdea(r.Context(), id, service.IdeaPatch{Status: &st, Project: &proj, Tag: &tag, Title: &title})
		if err != nil {
			return "", err
		}
		return "Idea updated.", nil
	})
}

type linksData struct {
	Links []domain.Bookmark
	Query string
	Self  string
}

func mountLinks(ui *UI) {
	ui.Get("GET /ui/links", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		list, err := ui.Svc.ListBookmarks(r.Context(), q)
		if err != nil {
			ui.Fail(w, err)
			return
		}
		self := "/ui/links"
		if q != "" {
			self += "?q=" + url.QueryEscape(q)
		}
		ui.Render(w, r, "links", "Links", linksData{Links: list, Query: q, Self: self})
	})
	ui.Post("POST /ui/links/add", func(r *http.Request, _ string) (string, error) {
		b, err := ui.Svc.AddBookmark(r.Context(), r.PostFormValue("url"), []string{r.PostFormValue("tags")})
		if err != nil {
			return "", err
		}
		return "Saved: " + b.Title, nil
	})
	ui.Post("POST /ui/links/pin", func(r *http.Request, _ string) (string, error) {
		id, err := formID(r, "id")
		if err != nil {
			return "", err
		}
		return "Updated.", ui.Svc.PinBookmark(r.Context(), id, r.PostFormValue("pinned") == "1")
	})
	ui.Post("POST /ui/links/delete", func(r *http.Request, _ string) (string, error) {
		id, err := formID(r, "id")
		if err != nil {
			return "", err
		}
		return "Link deleted.", ui.Svc.DeleteBookmark(r.Context(), id)
	})
}
