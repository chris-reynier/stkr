package httpapi

import (
	"errors"
	"image/color"
	"io"
	"net/http"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/keyimg"
	"github.com/chris-reynier/stkr/internal/service"
)

func (s *Server) dailyRoutes() {
	s.mux.HandleFunc("POST /api/day/start", s.handleDayStart)
	s.mux.HandleFunc("POST /api/close/prompt", s.handleClosePrompt)
	s.mux.HandleFunc("POST /api/import/clipboard", s.handleImportClipboard)
	s.mux.HandleFunc("POST /api/import", s.handleImport)
	s.mux.HandleFunc("POST /api/paste", s.handlePaste)
	s.mux.HandleFunc("GET /img/key/close.png", s.handleCloseImage)
	s.mux.HandleFunc("GET /img/key/import.png", s.handleImportImage)
}

// flag reads a 0/1/true/false query parameter.
func flag(r *http.Request, name string, def bool) bool {
	switch r.URL.Query().Get(name) {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	return def
}

func (s *Server) handleDayStart(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.StartDay(r.Context(), service.StartDayOptions{Source: source(r), Clipboard: flag(r, "clipboard", true)})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleClosePrompt(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := s.svc.ClosePrompt(r.Context(), service.CloseOptions{
		Clipboard: flag(r, "clipboard", true),
		Paste:     flag(r, "paste", false),
		Window:    q.Get("window"),
		Enter:     flag(r, "enter", false),
	})
	if err != nil {
		s.writePasteErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// writePasteErr answers a missing target window with the documented
// {"error":"window_not_found"}; every other error uses the common mapping.
func (s *Server) writePasteErr(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errBody{"window_not_found", err.Error()})
		return
	}
	s.writeErr(w, err)
}

func (s *Server) handlePaste(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Paste(r.Context(), r.URL.Query().Get("window"), flag(r, "enter", false)); err != nil {
		s.writePasteErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleImportClipboard(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.ImportClipboard(r.Context())
	s.writeImport(w, res, err)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	res, err := s.svc.Import(r.Context(), string(body), service.ViaAPI)
	s.writeImport(w, res, err)
}

// importFailure is the body of a rejected import: the result plus the error.
type importFailure struct {
	service.ImportResult
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (s *Server) writeImport(w http.ResponseWriter, res service.ImportResult, err error) {
	var nb *service.NoValidBlockError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, res)
	case errors.As(err, &nb):
		writeJSON(w, http.StatusUnprocessableEntity, importFailure{ImportResult: nb.Result, Error: "no_valid_block", Message: nb.Error()})
	default:
		s.writeErr(w, err)
	}
}

var (
	colKeyOK      = color.RGBA{21, 128, 61, 255}
	colKeyWarn    = color.RGBA{250, 204, 21, 255}
	colKeyFail    = color.RGBA{185, 28, 28, 255}
	colKeyNeutral = color.RGBA{75, 85, 99, 255}
	colKeyDark    = color.RGBA{17, 24, 39, 255}
)

// closeSpec builds the close key. While the close is due it alternates
// between yellow and dark every second.
func closeSpec(st service.CloseState) keyimg.KeySpec {
	switch {
	case st.Saved:
		return keyimg.KeySpec{Background: colKeyOK, Lines: []string{"CLOSE", "saved"}}
	case st.Due && st.Blink:
		return keyimg.KeySpec{Background: colKeyWarn, Foreground: colKeyDark, Lines: []string{"CLOSE", "DAY", "due"}}
	case st.Due:
		return keyimg.KeySpec{Background: colKeyDark, Lines: []string{"CLOSE", "DAY", "due"}}
	}
	return keyimg.KeySpec{Background: colKeyNeutral, Lines: []string{"CLOSE", "DAY"}}
}

// importSpec builds the import key: green, yellow or red for ten seconds after
// an import, neutral otherwise.
func importSpec(state string) keyimg.KeySpec {
	switch state {
	case service.ImportOK:
		return keyimg.KeySpec{Background: colKeyOK, Lines: []string{"IMPORT", "OK"}}
	case service.ImportWarn:
		return keyimg.KeySpec{Background: colKeyWarn, Foreground: colKeyDark, Lines: []string{"IMPORT", "WARN"}}
	case service.ImportFail:
		return keyimg.KeySpec{Background: colKeyFail, Lines: []string{"IMPORT", "FAILED"}}
	}
	return keyimg.KeySpec{Background: colKeyNeutral, Lines: []string{"IMPORT", "clipboard"}}
}

func (s *Server) handleCloseImage(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.CloseState(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writePNG(w, keyimg.Render(closeSpec(st)))
}

func (s *Server) handleImportImage(w http.ResponseWriter, r *http.Request) {
	state, err := s.svc.ImportKeyState(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writePNG(w, keyimg.Render(importSpec(state)))
}
