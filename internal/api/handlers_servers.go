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

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowServerCreation {
		writeError(w, http.StatusForbidden, "server_create_disabled", "server creation is disabled on this backend", false)
		return
	}

	var payload struct {
		DisplayName  string `json:"display_name"`
		Description  string `json:"description"`
		BannerPreset string `json:"banner_preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid server create payload", false)
		return
	}

	requester := requesterFromContext(r.Context())
	created, err := s.chat.CreateServer(requester.UserUID, payload.DisplayName, payload.Description, payload.BannerPreset)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrServerDisplayNameInvalid):
			writeError(w, http.StatusBadRequest, "invalid_display_name", "display name is invalid", false)
		case errors.Is(err, chat.ErrServerDescriptionInvalid):
			writeError(w, http.StatusBadRequest, "invalid_description", "description is invalid", false)
		case errors.Is(err, chat.ErrServerBannerPresetInvalid):
			writeError(w, http.StatusBadRequest, "invalid_banner_preset", "banner preset is invalid", false)
		default:
			writeError(w, http.StatusBadRequest, "server_create_failed", err.Error(), false)
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"server":         created.Server,
		"created_by_uid": created.CreatedByUID,
		"created_at":     created.CreatedAt,
		"ownership_claim": map[string]any{
			"token":      created.OwnershipClaim.Token,
			"expires_at": created.OwnershipClaim.ExpiresAt,
		},
	})
}

func (s *Server) claimServerOwnership(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	if serverID == "" {
		writeError(w, http.StatusBadRequest, "invalid_server", "server id is required", false)
		return
	}

	var payload struct {
		ClaimToken string `json:"claim_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid ownership claim payload", false)
		return
	}

	requester := requesterFromContext(r.Context())
	claimed, err := s.chat.ClaimServerOwnership(serverID, requester.UserUID, payload.ClaimToken)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrOwnershipClaimInvalid):
			writeError(w, http.StatusForbidden, "ownership_claim_invalid", "ownership claim token is invalid", false)
		case errors.Is(err, chat.ErrOwnershipClaimExpired):
			writeError(w, http.StatusForbidden, "ownership_claim_expired", "ownership claim token has expired", false)
		case errors.Is(err, chat.ErrOwnershipClaimForbidden):
			writeError(w, http.StatusForbidden, "ownership_claim_forbidden", "ownership already claimed by another owner", false)
		case isUnknownServerError(err):
			writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		default:
			writeError(w, http.StatusBadRequest, "ownership_claim_failed", err.Error(), false)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":      claimed.ServerID,
		"owner_user_uid": claimed.OwnerUserUID,
		"claimed_at":     claimed.ClaimedAt,
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
		case errors.Is(err, chat.ErrOwnershipClaimRequired):
			writeError(w, http.StatusForbidden, "ownership_claim_required", "ownership claim is required before updating server settings", false)
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
