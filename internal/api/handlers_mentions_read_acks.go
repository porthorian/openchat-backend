package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/chat"
)

func (s *Server) resolveMentions(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}

	limit := 20
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	requester := requesterFromContext(r.Context())

	results, err := s.chat.ResolveMentionCandidates(channelID, requester.UserUID, query, limit)
	if err != nil {
		if isUnknownChannelError(err) {
			writeError(w, http.StatusNotFound, "channel_not_found", err.Error(), false)
			return
		}
		writeError(w, http.StatusBadRequest, "mention_resolve_failed", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"query":      query,
		"results":    results,
	})
}

func (s *Server) getReadAck(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}

	requester := requesterFromContext(r.Context())
	readAck, err := s.chat.GetReadAck(channelID, requester.UserUID)
	if err != nil {
		if isUnknownChannelError(err) {
			writeError(w, http.StatusNotFound, "channel_not_found", err.Error(), false)
			return
		}
		writeError(w, http.StatusBadRequest, "read_ack_failed", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"read_ack":   readAck,
	})
}

func (s *Server) putReadAck(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}

	var payload struct {
		LastReadMessageID string `json:"last_read_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid read ack payload", false)
		return
	}

	requester := requesterFromContext(r.Context())
	readAck, applied, err := s.chat.UpdateReadAck(channelID, requester.UserUID, payload.LastReadMessageID)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrReadAckMessageNotFound):
			writeError(w, http.StatusBadRequest, "read_ack_cursor_not_found", "last_read_message_id is not in channel history", false)
		case isUnknownChannelError(err):
			writeError(w, http.StatusNotFound, "channel_not_found", err.Error(), false)
		default:
			writeError(w, http.StatusBadRequest, "read_ack_failed", err.Error(), false)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"read_ack":   readAck,
		"applied":    applied,
	})
}

func isUnknownChannelError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unknown channel id")
}
