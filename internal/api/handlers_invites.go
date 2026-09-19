package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/auth"
	"github.com/openchat/openchat-backend/internal/serveractions"
)

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresAt time.Time `json:"expires_at"`
		MaxUses   int       `json:"max_uses"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, 400, "invalid_payload", "invalid invite request", false)
		return
	}
	serverID := chi.URLParam(r, "serverID")
	uid := requesterFromContext(r.Context()).UserUID
	invite, err := s.actions.CreateInvite(r.Context(), serverID, uid, body.ExpiresAt, body.MaxUses)
	if err != nil {
		writeInviteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, invite)
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	uid := requesterFromContext(r.Context()).UserUID
	var before *time.Time
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			writeError(w, 400, "invalid_cursor", "invalid invite cursor", false)
			return
		}
		parsed, err := time.Parse(time.RFC3339Nano, string(decoded))
		if err != nil {
			writeError(w, 400, "invalid_cursor", "invalid invite cursor", false)
			return
		}
		before = &parsed
	}
	invites, err := s.actions.ListInvites(r.Context(), serverID, uid, 50, before)
	if err != nil {
		writeInviteError(w, err)
		return
	}
	next := ""
	if len(invites) == 50 {
		next = base64.RawURLEncoding.EncodeToString([]byte(invites[len(invites)-1].CreatedAt.Format(time.RFC3339Nano)))
	}
	writeJSON(w, 200, map[string]any{"invites": invites, "next_cursor": next})
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	err := s.actions.RevokeInvite(r.Context(), chi.URLParam(r, "serverID"), requesterFromContext(r.Context()).UserUID, chi.URLParam(r, "inviteID"))
	if err != nil {
		writeInviteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value(sessionContextKey{}).(auth.Session)
	if !ok {
		writeError(w, 403, "verified_session_required", "verified session required to redeem invite", false)
		return
	}
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	serverID, err := s.actions.Redeem(r.Context(), code, session.UserUID, session.ServerID)
	if err != nil {
		writeInviteError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"server_id": serverID, "membership_state": "active"})
}

func writeInviteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serveractions.ErrForbidden):
		writeError(w, 403, "forbidden", "invite action forbidden", false)
	case errors.Is(err, serveractions.ErrConflict):
		writeError(w, 409, "invite_conflict", "invalid invite limits or expiry", false)
	case errors.Is(err, serveractions.ErrExpired):
		writeError(w, 410, "invite_expired", "invite expired, revoked, or exhausted", false)
	case errors.Is(err, serveractions.ErrNotFound):
		writeError(w, 404, "invite_not_found", "invite not found", false)
	default:
		writeError(w, 503, "invites_unavailable", "invite service unavailable", true)
	}
}
