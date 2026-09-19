package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/chat"
)

func (s *Server) putBulkReadAcks(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var body struct {
		ReadAcks []chat.BulkReadAckInput `json:"read_acks"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&body); err != nil {
		writeError(w, 400, "invalid_payload", "invalid read-ack batch", false)
		return
	}
	result, err := s.chat.UpdateReadAcksBulk(serverID, requesterFromContext(r.Context()).UserUID, body.ReadAcks)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrReadAckMessageNotFound):
			writeError(w, 400, "read_ack_cursor_not_found", "message cursor not found", false)
		case errors.Is(err, chat.ErrBulkReadAckInvalid):
			writeError(w, 400, "invalid_read_acks", "batch contains an invalid channel or cursor", false)
		default:
			writeError(w, 503, "read_acks_unavailable", "bulk read acks unavailable", true)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"server_id": serverID, "read_acks": result})
}
