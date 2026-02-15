package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	requester := requesterFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"servers": s.chat.ListServersForUser(requester.UserUID),
	})
}

func (s *Server) leaveServerMembership(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "invalid_server", "server id is required", false)
		return
	}

	requester := requesterFromContext(r.Context())
	if err := s.chat.LeaveServer(serverID, requester.UserUID); err != nil {
		writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": serverID,
		"user_uid":  requester.UserUID,
		"left":      true,
		"left_at":   time.Now().UTC().Format(time.RFC3339),
	})
}
