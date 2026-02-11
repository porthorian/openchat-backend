package api

import "net/http"

func (s *Server) getCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.capabilities.Build())
}
