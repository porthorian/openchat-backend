package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openchat/openchat-backend/internal/app"
)

func TestCreateServerAndClaimOwnershipLifecycle(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:            ":0",
		PublicBaseURL:       "http://localhost:8080",
		SignalingPath:       "/v1/rtc/signaling",
		TicketTTL:           60 * time.Second,
		TicketSecret:        "test-secret",
		Environment:         "test",
		AllowServerCreation: true,
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	createBody, _ := json.Marshal(map[string]any{
		"display_name":  "Penny Lab",
		"description":   "Server created from API",
		"banner_preset": "ocean",
	})
	createReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/servers", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("build create server request: %v", err)
	}
	createReq.Header.Set("X-OpenChat-User-UID", "uid_backend_scope")
	createReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create server request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("unexpected create status: %d body=%s", createResp.StatusCode, string(body))
	}

	var createPayload struct {
		Server struct {
			ServerID    string `json:"server_id"`
			DisplayName string `json:"display_name"`
		} `json:"server"`
		OwnershipClaim struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expires_at"`
		} `json:"ownership_claim"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Server.ServerID == "" || createPayload.OwnershipClaim.Token == "" {
		t.Fatalf("expected server id and ownership claim token")
	}
	if _, parseErr := uuid.Parse(createPayload.Server.ServerID); parseErr != nil {
		t.Fatalf("expected uuid server id, got %s", createPayload.Server.ServerID)
	}

	createCategoryBody, _ := json.Marshal(map[string]any{"name": "ops", "kind": "text"})
	blockedReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+createPayload.Server.ServerID+"/categories",
		bytes.NewReader(createCategoryBody),
	)
	if err != nil {
		t.Fatalf("build blocked category request: %v", err)
	}
	blockedReq.Header.Set("X-OpenChat-User-UID", "uid_server_scope")
	blockedReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	blockedReq.Header.Set("Content-Type", "application/json")

	blockedResp, err := http.DefaultClient.Do(blockedReq)
	if err != nil {
		t.Fatalf("blocked category request failed: %v", err)
	}
	defer blockedResp.Body.Close()
	if blockedResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(blockedResp.Body)
		t.Fatalf("expected ownership claim required before category create, got %d body=%s", blockedResp.StatusCode, string(body))
	}
	var blockedErr APIError
	if err := json.NewDecoder(blockedResp.Body).Decode(&blockedErr); err != nil {
		t.Fatalf("decode blocked error: %v", err)
	}
	if blockedErr.Code != "ownership_claim_required" {
		t.Fatalf("expected ownership_claim_required code, got %s", blockedErr.Code)
	}

	claimBody, _ := json.Marshal(map[string]any{"claim_token": createPayload.OwnershipClaim.Token})
	claimReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+createPayload.Server.ServerID+"/ownership:claim",
		bytes.NewReader(claimBody),
	)
	if err != nil {
		t.Fatalf("build claim request: %v", err)
	}
	claimReq.Header.Set("X-OpenChat-User-UID", "uid_server_scope")
	claimReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	claimReq.Header.Set("Content-Type", "application/json")

	claimResp, err := http.DefaultClient.Do(claimReq)
	if err != nil {
		t.Fatalf("claim ownership request failed: %v", err)
	}
	defer claimResp.Body.Close()
	if claimResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(claimResp.Body)
		t.Fatalf("unexpected claim status: %d body=%s", claimResp.StatusCode, string(body))
	}

	allowedReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+createPayload.Server.ServerID+"/categories",
		bytes.NewReader(createCategoryBody),
	)
	if err != nil {
		t.Fatalf("build allowed category request: %v", err)
	}
	allowedReq.Header.Set("X-OpenChat-User-UID", "uid_server_scope")
	allowedReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	allowedReq.Header.Set("Content-Type", "application/json")

	allowedResp, err := http.DefaultClient.Do(allowedReq)
	if err != nil {
		t.Fatalf("allowed category request failed: %v", err)
	}
	defer allowedResp.Body.Close()
	if allowedResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(allowedResp.Body)
		t.Fatalf("expected created category after claim, got %d body=%s", allowedResp.StatusCode, string(body))
	}

	forbiddenSettingsBody, _ := json.Marshal(map[string]any{
		"display_name":  "Blocked Update",
		"description":   "",
		"banner_preset": "ocean",
	})
	forbiddenSettingsReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+createPayload.Server.ServerID+"/settings",
		bytes.NewReader(forbiddenSettingsBody),
	)
	if err != nil {
		t.Fatalf("build forbidden settings request: %v", err)
	}
	forbiddenSettingsReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	forbiddenSettingsReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	forbiddenSettingsReq.Header.Set("Content-Type", "application/json")

	forbiddenSettingsResp, err := http.DefaultClient.Do(forbiddenSettingsReq)
	if err != nil {
		t.Fatalf("forbidden settings request failed: %v", err)
	}
	defer forbiddenSettingsResp.Body.Close()
	if forbiddenSettingsResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(forbiddenSettingsResp.Body)
		t.Fatalf("expected forbidden non-owner settings update, got %d body=%s", forbiddenSettingsResp.StatusCode, string(body))
	}
}

func TestCreateServerDisabledByPolicy(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:            ":0",
		PublicBaseURL:       "http://localhost:8080",
		SignalingPath:       "/v1/rtc/signaling",
		TicketTTL:           60 * time.Second,
		TicketSecret:        "test-secret",
		Environment:         "test",
		AllowServerCreation: false,
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	createBody, _ := json.Marshal(map[string]any{"display_name": "Disabled"})
	createReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/servers", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	createReq.Header.Set("X-OpenChat-User-UID", "uid_backend_scope")
	createReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected forbidden status, got %d body=%s", createResp.StatusCode, string(body))
	}

	var payload APIError
	if err := json.NewDecoder(createResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Code != "server_create_disabled" {
		t.Fatalf("expected server_create_disabled code, got %s", payload.Code)
	}
}

func TestClaimServerOwnershipInvalidToken(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:            ":0",
		PublicBaseURL:       "http://localhost:8080",
		SignalingPath:       "/v1/rtc/signaling",
		TicketTTL:           60 * time.Second,
		TicketSecret:        "test-secret",
		Environment:         "test",
		AllowServerCreation: true,
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	createBody, _ := json.Marshal(map[string]any{"display_name": "Invalid Claim"})
	createReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/servers", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	createReq.Header.Set("X-OpenChat-User-UID", "uid_backend_scope")
	createReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("unexpected create status: %d body=%s", createResp.StatusCode, string(body))
	}
	var createPayload struct {
		Server struct {
			ServerID string `json:"server_id"`
		} `json:"server"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	claimBody, _ := json.Marshal(map[string]any{"claim_token": "claim_bad_token"})
	claimReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+createPayload.Server.ServerID+"/ownership:claim",
		bytes.NewReader(claimBody),
	)
	if err != nil {
		t.Fatalf("build claim request: %v", err)
	}
	claimReq.Header.Set("X-OpenChat-User-UID", "uid_server_scope")
	claimReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	claimReq.Header.Set("Content-Type", "application/json")

	claimResp, err := http.DefaultClient.Do(claimReq)
	if err != nil {
		t.Fatalf("claim request failed: %v", err)
	}
	defer claimResp.Body.Close()
	if claimResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(claimResp.Body)
		t.Fatalf("expected forbidden invalid claim status, got %d body=%s", claimResp.StatusCode, string(body))
	}

	var payload APIError
	if err := json.NewDecoder(claimResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Code != "ownership_claim_invalid" {
		t.Fatalf("expected ownership_claim_invalid code, got %s", payload.Code)
	}
}
