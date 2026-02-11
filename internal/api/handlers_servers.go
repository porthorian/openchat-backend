package api

import "net/http"

func (s *Server) listServers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"servers": s.chat.ListServers(),
	})
}
