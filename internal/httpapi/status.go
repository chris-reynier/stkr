package httpapi

import "net/http"

// handleStatus serves the compact state polled by the desktop app
// (contract status-api.md).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Status(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	st.HostedBy = s.hostedBy
	writeJSON(w, http.StatusOK, st)
}
