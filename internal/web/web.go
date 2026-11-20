// Package web is the server-rendered UI: html/template pages enhanced with
// HTMX, all embedded in the binary. Pages register themselves with Register;
// the navigation only lists registered pages, so later features add a page by
// calling Register from their own file.
package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Page describes one page of the UI.
type Page struct {
	// ID names the page and its template file "<ID>.html", which must define
	// the template "content".
	ID string
	// Title and Path are shown in the navigation.
	Title string
	Path  string
	// Order sorts the navigation (Today 10, Week 20, Agents 30, Ideas 40,
	// Links 50, Settings 90).
	Order int
	// Templates holds "<ID>.html" at its root. Nil uses the templates embedded
	// in this package.
	Templates fs.FS
	// Mount registers the page's routes. Use ui.Get for pages and ui.Post for
	// form actions; both get the security headers.
	Mount func(ui *UI)
}

var (
	regMu    sync.Mutex
	registry []Page
)

// Register adds a page. Call it from init.
func Register(p Page) {
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, p)
}

// UI is the mounted web interface handed to pages.
type UI struct {
	Svc   *service.Service
	log   *slog.Logger
	mux   *http.ServeMux
	pages []Page
	tmpl  map[string]*template.Template
}

// Options configure Mount.
type Options struct {
	Logger *slog.Logger
}

// NavItem is one navigation entry.
type NavItem struct {
	Title, Path string
	Active      bool
}

// View is the data every page template receives.
type View struct {
	Title string
	Page  string
	Nav   []NavItem
	Flash *Flash
	// Banner is the text of the update banner slot; empty hides it.
	Banner string
	Data   any
}

// Flash is a one-time message shown after a redirect.
type Flash struct {
	Error bool
	Text  string
}

var funcs = template.FuncMap{
	"dur":   func(d domain.Duration) string { return domain.FormatDuration(d.D()) },
	"hm":    func(t time.Time) string { return t.Format("15:04") },
	"hours": func(d domain.Duration) string { return fmt.Sprintf("%.2f", d.D().Hours()) },
	"hh": func(h float64) string {
		if h == 0 {
			return ""
		}
		return fmt.Sprintf("%.2f", h)
	},
	"join": strings.Join,
	"inc":  func(i int) int { return i + 1 },
}

// Mount registers the UI routes on mux. Register every page before calling it.
func Mount(mux *http.ServeMux, svc *service.Service, opts Options) (*UI, error) {
	lg := opts.Logger
	if lg == nil {
		lg = slog.Default()
	}
	regMu.Lock()
	pages := append([]Page(nil), registry...)
	regMu.Unlock()
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Order < pages[j].Order })

	ui := &UI{Svc: svc, log: lg, mux: mux, pages: pages, tmpl: map[string]*template.Template{}}
	for _, p := range pages {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html")
		if err != nil {
			return nil, err
		}
		if p.Templates == nil {
			t, err = t.ParseFS(templateFS, "templates/"+p.ID+".html")
		} else {
			t, err = t.ParseFS(p.Templates, p.ID+".html")
		}
		if err != nil {
			return nil, fmt.Errorf("page %s: %w", p.ID, err)
		}
		ui.tmpl[p.ID] = t
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	static := http.StripPrefix("/static/", http.FileServerFS(sub))
	ui.Get("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})
	home := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ui/today", http.StatusSeeOther) }
	ui.Get("GET /{$}", home)
	ui.Get("GET /ui/{$}", home)
	for _, p := range pages {
		if p.Mount != nil {
			p.Mount(ui)
		}
	}
	return ui, nil
}

// secure sets the response headers every UI response carries.
func secure(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
}

// Get registers a read-only route ("GET /ui/x").
func (u *UI) Get(pattern string, h http.HandlerFunc) {
	u.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		secure(w)
		h(w, r)
	})
}

// Post registers a form action. The action runs, then the browser is
// redirected (303) to the form's "next" field, so pages work without
// JavaScript. A returned error is shown as a message after the redirect; ok is
// the success message. next is always a local /ui/ path.
func (u *UI) Post(pattern string, act func(r *http.Request, next string) (ok string, err error)) {
	u.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		secure(w)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		next := safeNext(r.PostFormValue("next"))
		ok, err := act(r, next)
		switch {
		case err != nil:
			setFlash(w, Flash{Error: true, Text: u.userMessage(err)})
		case ok != "":
			setFlash(w, Flash{Text: ok})
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	})
}

// safeNext only allows local UI paths, so a form cannot redirect elsewhere.
func safeNext(n string) string {
	if strings.HasPrefix(n, "/ui/") && !strings.ContainsAny(n, "\\\r\n") {
		return n
	}
	return "/ui/today"
}

// userMessage turns an error into text safe to show. Domain errors carry
// user-facing messages; anything else is logged and hidden.
func (u *UI) userMessage(err error) string {
	for _, e := range []error{domain.ErrInvalid, domain.ErrNotFound, domain.ErrConflict, domain.ErrUnavailable} {
		if errors.Is(err, e) {
			return err.Error()
		}
	}
	u.log.Error("ui action failed", "err", err)
	return "internal error"
}

// Fail writes a plain error response for a failed page load.
func (u *UI) Fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrUnavailable):
		status = http.StatusServiceUnavailable
	}
	http.Error(w, u.userMessage(err), status)
}

const flashCookie = "stkr_flash"

// putFlashCookie sets (or with maxAge < 0 clears) the flash cookie. It is
// HttpOnly and SameSite=Strict; Secure is not set because the server speaks
// plain HTTP on loopback.
func putFlashCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // loopback HTTP, Secure would drop the cookie in some browsers
		Name: flashCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func setFlash(w http.ResponseWriter, f Flash) {
	kind := "o"
	if f.Error {
		kind = "e"
	}
	putFlashCookie(w, url.QueryEscape(kind+f.Text), 60)
}

func takeFlash(w http.ResponseWriter, r *http.Request) *Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	putFlashCookie(w, "", -1)
	v, err := url.QueryUnescape(c.Value)
	if err != nil || len(v) < 2 {
		return nil
	}
	return &Flash{Error: v[0] == 'e', Text: v[1:]}
}

// Render writes a page. pageID selects the template set; data is available as
// .Data in the template.
func (u *UI) Render(w http.ResponseWriter, r *http.Request, pageID, title string, data any) {
	t := u.tmpl[pageID]
	if t == nil {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	v := View{Title: title, Page: pageID, Flash: takeFlash(w, r), Banner: u.Svc.UpdateBanner(r.Context()), Data: data}
	for _, p := range u.pages {
		v.Nav = append(v.Nav, NavItem{Title: p.Title, Path: p.Path, Active: p.ID == pageID})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := t.ExecuteTemplate(w, "layout", v); err != nil {
		u.log.Error("render failed", "page", pageID, "err", err)
	}
}
