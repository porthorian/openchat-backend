package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/profile"
)

const maxProfileBatchSize = 100

func (s *Server) getMyProfile(w http.ResponseWriter, r *http.Request) {
	requester := requesterFromContext(r.Context())
	writeJSON(w, http.StatusOK, s.profiles.GetOrCreate(requester.UserUID))
}

func (s *Server) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	requester := requesterFromContext(r.Context())

	var body struct {
		DisplayName   string `json:"display_name"`
		AvatarMode    string `json:"avatar_mode"`
		AvatarPreset  string `json:"avatar_preset_id"`
		AvatarAssetID string `json:"avatar_asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid profile update payload", false)
		return
	}

	expectedVersion, err := parseIfMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be an integer profile version", false)
		return
	}

	updated, updateErr := s.profiles.Update(requester.UserUID, profile.UpdateInput{
		DisplayName:   body.DisplayName,
		AvatarMode:    profile.AvatarMode(strings.TrimSpace(body.AvatarMode)),
		AvatarPreset:  body.AvatarPreset,
		AvatarAssetID: body.AvatarAssetID,
	}, expectedVersion)
	if updateErr != nil {
		switch {
		case errors.Is(updateErr, profile.ErrDisplayNameInvalid):
			writeError(w, http.StatusBadRequest, "display_name_invalid", "display name does not meet policy", false)
		case errors.Is(updateErr, profile.ErrAvatarModeUnsupported):
			writeError(w, http.StatusBadRequest, "avatar_mode_unsupported", "avatar mode is not supported", false)
		case errors.Is(updateErr, profile.ErrAvatarPresetInvalid):
			writeError(w, http.StatusBadRequest, "avatar_mode_unsupported", "avatar preset is invalid", false)
		case errors.Is(updateErr, profile.ErrAvatarAssetNotFound):
			writeError(w, http.StatusBadRequest, "avatar_asset_not_found", "avatar asset not found", false)
		case errors.Is(updateErr, profile.ErrProfileConflict):
			writeError(w, http.StatusConflict, "profile_conflict", "profile update conflict", true)
		default:
			writeError(w, http.StatusInternalServerError, "profile_update_failed", "unable to update profile", true)
		}
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) uploadProfileAvatar(w http.ResponseWriter, r *http.Request) {
	maxBytes, _, _, _ := s.profiles.AvatarUploadRules()
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes+1024))
	if err := r.ParseMultipartForm(int64(maxBytes + 1024)); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar exceeds max upload size", false)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "missing multipart file field 'file'", false)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, int64(maxBytes+1)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "unable to read avatar upload", false)
		return
	}
	if len(content) > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar exceeds max upload size", false)
		return
	}

	contentType := ""
	if header != nil {
		contentType = strings.TrimSpace(header.Header.Get("Content-Type"))
	}
	asset, uploadErr := s.profiles.UploadAvatar(contentType, content)
	if uploadErr != nil {
		switch {
		case errors.Is(uploadErr, profile.ErrAvatarTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "avatar_too_large", "avatar exceeds max upload size", false)
		case errors.Is(uploadErr, profile.ErrAvatarTypeUnsupported):
			writeError(w, http.StatusUnsupportedMediaType, "avatar_type_unsupported", "avatar mime type is unsupported", false)
		case errors.Is(uploadErr, profile.ErrAvatarDimensions):
			writeError(w, http.StatusBadRequest, "avatar_dimensions_exceeded", "avatar dimensions exceed limits", false)
		default:
			writeError(w, http.StatusInternalServerError, "avatar_upload_failed", "unable to upload avatar", true)
		}
		return
	}

	writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) getProfileAvatar(w http.ResponseWriter, r *http.Request) {
	assetID := strings.TrimSpace(chi.URLParam(r, "assetID"))
	asset, content, err := s.profiles.AvatarContent(assetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "avatar_asset_not_found", "avatar asset not found", false)
		return
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) batchProfiles(w http.ResponseWriter, r *http.Request) {
	userUIDs := r.URL.Query()["user_uid"]
	if len(userUIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "at least one user_uid is required", false)
		return
	}
	if len(userUIDs) > maxProfileBatchSize {
		writeError(w, http.StatusBadRequest, "invalid_query", "too many user_uid values", false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profiles": s.profiles.BatchGet(userUIDs),
	})
}

func parseIfMatchVersion(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, errors.New("invalid if-match")
	}
	return &parsed, nil
}
