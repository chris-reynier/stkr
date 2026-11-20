package httpapi

import (
	"image/color"
	"net/http"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/keyimg"
)

func writePNG(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

var (
	colProject  = color.RGBA{37, 99, 235, 255}
	colMode     = color.RGBA{109, 40, 217, 255}
	colPersonal = color.RGBA{4, 120, 87, 255}
	colSystem   = color.RGBA{75, 85, 99, 255}
	colIdle     = color.RGBA{180, 83, 9, 255}
	colOff      = color.RGBA{31, 41, 55, 255}
)

// nowSpec builds the key image for the current tracking state.
func nowSpec(st domain.NowStatus) keyimg.KeySpec {
	if !st.Tracking || st.Context == nil {
		return keyimg.KeySpec{Background: colOff, Lines: []string{"OFF", "today " + domain.FormatDuration(st.Today.D())}}
	}
	bg := colProject
	switch st.Context.Kind {
	case domain.KindMode:
		bg = colMode
	case domain.KindPersonal:
		bg = colPersonal
	case domain.KindSystem:
		bg = colSystem
	}
	label := st.Context.Label
	if label == "" {
		label = st.Context.ID
	}
	spec := keyimg.KeySpec{
		Background: bg,
		Lines:      []string{label, domain.FormatDuration(st.Elapsed.D()), "today " + domain.FormatDuration(st.Today.D())},
	}
	if st.IdleSuspect {
		spec.Background = colIdle
		spec.Badge = "?"
	}
	return spec
}

func (s *Server) handleNowImage(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Now(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writePNG(w, keyimg.Render(nowSpec(st)))
}
