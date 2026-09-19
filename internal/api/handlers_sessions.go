package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/auth"
)

func (s *Server) beginSessionChallenge(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	var input struct {
		PublicKey string `json:"public_key"`
		UserUID   string `json:"user_uid"`
		DeviceID  string `json:"device_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid challenge request", false)
		return
	}
	pub, err := base64.RawURLEncoding.DecodeString(input.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "invalid_public_key", "expected 32-byte Ed25519 public key", false)
		return
	}
	challenge, err := s.auth.BeginChallenge(r.Context(), serverID, input.UserUID, input.DeviceID, ed25519.PublicKey(pub))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrBindingRequired):
			writeError(w, http.StatusForbidden, "binding_required", "existing UID requires verified key binding", false)
		case errors.Is(err, auth.ErrInvalidKey):
			writeError(w, http.StatusBadRequest, "invalid_identity", "invalid key or device ID", false)
		default:
			writeError(w, http.StatusServiceUnavailable, "challenge_unavailable", "verified session service unavailable", true)
		}
		return
	}
	writeJSON(w, http.StatusCreated, challenge)
}

func (s *Server) completeSessionChallenge(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	var input struct {
		ChallengeID string `json:"challenge_id"`
		Signature   string `json:"signature"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid session request", false)
		return
	}
	signature, err := base64.RawURLEncoding.DecodeString(input.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		writeError(w, http.StatusBadRequest, "invalid_signature", "expected Ed25519 signature", false)
		return
	}
	session, err := s.auth.CompleteChallenge(r.Context(), serverID, input.ChallengeID, signature)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrChallengeExpired):
			writeError(w, http.StatusGone, "challenge_expired", "challenge expired or already used", false)
		case errors.Is(err, auth.ErrInvalidSignature):
			writeError(w, http.StatusForbidden, "signature_invalid", "challenge signature invalid", false)
		case errors.Is(err, auth.ErrBindingRequired):
			writeError(w, http.StatusForbidden, "binding_required", "verified key binding required", false)
		default:
			writeError(w, http.StatusServiceUnavailable, "session_unavailable", "verified session service unavailable", true)
		}
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) approveIdentityBinding(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	targetUID := strings.TrimSpace(chi.URLParam(r, "userUID"))
	var input struct {
		PublicKey             string `json:"public_key"`
		VerificationReference string `json:"verification_reference"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid binding approval request", false)
		return
	}
	pub, err := base64.RawURLEncoding.DecodeString(input.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "invalid_public_key", "expected Ed25519 public key", false)
		return
	}
	err = s.auth.ApproveLegacyBinding(r.Context(), serverID, requesterFromContext(r.Context()).UserUID, targetUID, ed25519.PublicKey(pub), input.VerificationReference)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrBindingForbidden):
			writeError(w, http.StatusForbidden, "binding_forbidden", "binding approval forbidden", false)
		case errors.Is(err, auth.ErrBindingConflict):
			writeError(w, http.StatusConflict, "binding_conflict", "identity already bound", false)
		case errors.Is(err, auth.ErrInvalidKey):
			writeError(w, http.StatusBadRequest, "invalid_binding", "invalid key or verification reference", false)
		default:
			writeError(w, http.StatusServiceUnavailable, "binding_unavailable", "binding approval unavailable", true)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server_id": serverID, "user_uid": targetUID, "verified": true})
}
