package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) listChannelGroups(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	groups, err := s.chat.ListChannelGroups(serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": serverID,
		"groups":    groups,
	})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	members, err := s.chat.ListMembers(serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": serverID,
		"members":   members,
	})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}

	messages, err := s.chat.ListMessages(channelID, limit)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel_not_found", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"messages":   messages,
	})
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}

	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid message payload", false)
		return
	}

	requester := requesterFromContext(r.Context())
	message, err := s.chat.CreateMessage(channelID, requester.UserUID, body.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "message_create_failed", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": message,
	})
}

func (s *Server) realtimeWS(w http.ResponseWriter, r *http.Request) {
	s.realtime.ServeWS(w, r)
}
