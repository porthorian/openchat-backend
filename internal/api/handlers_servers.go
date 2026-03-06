package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/chat"
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

func (s *Server) getServerSettings(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "invalid_server", "server id is required", false)
		return
	}

	settings, err := s.chat.GetServerSettings(serverID)
	if err != nil {
		if isUnknownServerError(err) {
			writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
			return
		}
		writeError(w, http.StatusBadRequest, "server_settings_get_failed", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":     settings.ServerID,
		"display_name":  settings.DisplayName,
		"description":   settings.Description,
		"banner_preset": settings.BannerPreset,
	})
}

func (s *Server) putServerSettings(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "invalid_server", "server id is required", false)
		return
	}

	var payload struct {
		DisplayName  string `json:"display_name"`
		Description  string `json:"description"`
		BannerPreset string `json:"banner_preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid server settings payload", false)
		return
	}

	requester := requesterFromContext(r.Context())
	updated, err := s.chat.UpdateServerSettings(serverID, requester.UserUID, payload.DisplayName, payload.Description, payload.BannerPreset)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrServerSettingsForbidden):
			writeError(w, http.StatusForbidden, "server_settings_forbidden", "requester does not have permission to update server settings", false)
		case errors.Is(err, chat.ErrServerDisplayNameInvalid):
			writeError(w, http.StatusBadRequest, "invalid_display_name", "display name is invalid", false)
		case errors.Is(err, chat.ErrServerDescriptionInvalid):
			writeError(w, http.StatusBadRequest, "invalid_description", "description is invalid", false)
		case errors.Is(err, chat.ErrServerBannerPresetInvalid):
			writeError(w, http.StatusBadRequest, "invalid_banner_preset", "banner preset is invalid", false)
		case isUnknownServerError(err):
			writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		default:
			writeError(w, http.StatusBadRequest, "server_settings_update_failed", err.Error(), false)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":      updated.ServerID,
		"display_name":   updated.DisplayName,
		"description":    updated.Description,
		"banner_preset":  updated.BannerPreset,
		"updated_by_uid": updated.UpdatedByUID,
		"updated_at":     updated.UpdatedAt,
	})
}
