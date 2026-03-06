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

	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/chat"
)

func TestCreateCategoryAndServerSettingsLifecycle(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	createCategoryBody, _ := json.Marshal(map[string]any{
		"name": "product",
		"kind": "voice",
	})
	createCategoryReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories",
		bytes.NewReader(createCategoryBody),
	)
	if err != nil {
		t.Fatalf("build create category request: %v", err)
	}
	createCategoryReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	createCategoryReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	createCategoryReq.Header.Set("Content-Type", "application/json")

	createCategoryResp, err := http.DefaultClient.Do(createCategoryReq)
	if err != nil {
		t.Fatalf("create category request failed: %v", err)
	}
	defer createCategoryResp.Body.Close()
	if createCategoryResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createCategoryResp.Body)
		t.Fatalf("unexpected create category status: %d body=%s", createCategoryResp.StatusCode, string(body))
	}

	var createdCategory struct {
		ServerID string `json:"server_id"`
		Group    struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Kind  string `json:"kind"`
		} `json:"group"`
	}
	if err := json.NewDecoder(createCategoryResp.Body).Decode(&createdCategory); err != nil {
		t.Fatalf("decode create category response: %v", err)
	}
	if createdCategory.ServerID != chat.SeedServerIDHarbor {
		t.Fatalf("expected harbor server id, got %s", createdCategory.ServerID)
	}
	if createdCategory.Group.ID == "" || createdCategory.Group.Label != "product" || createdCategory.Group.Kind != "voice" {
		t.Fatalf("unexpected category payload: %+v", createdCategory.Group)
	}

	updateSettingsBody, _ := json.Marshal(map[string]any{
		"display_name":  "Harbor Prime",
		"description":   "Updated server profile description.",
		"banner_preset": "sunset",
	})
	updateSettingsReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/settings",
		bytes.NewReader(updateSettingsBody),
	)
	if err != nil {
		t.Fatalf("build update settings request: %v", err)
	}
	updateSettingsReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	updateSettingsReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	updateSettingsReq.Header.Set("Content-Type", "application/json")

	updateSettingsResp, err := http.DefaultClient.Do(updateSettingsReq)
	if err != nil {
		t.Fatalf("update settings request failed: %v", err)
	}
	defer updateSettingsResp.Body.Close()
	if updateSettingsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateSettingsResp.Body)
		t.Fatalf("unexpected update settings status: %d body=%s", updateSettingsResp.StatusCode, string(body))
	}

	getSettingsReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/settings", nil)
	if err != nil {
		t.Fatalf("build get settings request: %v", err)
	}
	getSettingsReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	getSettingsReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")

	getSettingsResp, err := http.DefaultClient.Do(getSettingsReq)
	if err != nil {
		t.Fatalf("get settings request failed: %v", err)
	}
	defer getSettingsResp.Body.Close()
	if getSettingsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getSettingsResp.Body)
		t.Fatalf("unexpected get settings status: %d body=%s", getSettingsResp.StatusCode, string(body))
	}

	var settingsPayload struct {
		DisplayName  string `json:"display_name"`
		Description  string `json:"description"`
		BannerPreset string `json:"banner_preset"`
	}
	if err := json.NewDecoder(getSettingsResp.Body).Decode(&settingsPayload); err != nil {
		t.Fatalf("decode get settings response: %v", err)
	}
	if settingsPayload.DisplayName != "Harbor Prime" || settingsPayload.Description != "Updated server profile description." || settingsPayload.BannerPreset != "sunset" {
		t.Fatalf("unexpected settings payload: %+v", settingsPayload)
	}
}

func TestCreateCategoryAndServerSettingsOwnerGate(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	ownerCategoryBody, _ := json.Marshal(map[string]any{
		"name": "owner-seeded",
		"kind": "text",
	})
	ownerCategoryReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories",
		bytes.NewReader(ownerCategoryBody),
	)
	if err != nil {
		t.Fatalf("build owner category request: %v", err)
	}
	ownerCategoryReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	ownerCategoryReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	ownerCategoryReq.Header.Set("Content-Type", "application/json")

	ownerCategoryResp, err := http.DefaultClient.Do(ownerCategoryReq)
	if err != nil {
		t.Fatalf("owner category request failed: %v", err)
	}
	defer ownerCategoryResp.Body.Close()
	if ownerCategoryResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(ownerCategoryResp.Body)
		t.Fatalf("expected owner category create success, got %d body=%s", ownerCategoryResp.StatusCode, string(body))
	}

	otherCategoryReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories",
		bytes.NewReader(ownerCategoryBody),
	)
	if err != nil {
		t.Fatalf("build non-owner category request: %v", err)
	}
	otherCategoryReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	otherCategoryReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	otherCategoryReq.Header.Set("Content-Type", "application/json")

	otherCategoryResp, err := http.DefaultClient.Do(otherCategoryReq)
	if err != nil {
		t.Fatalf("non-owner category request failed: %v", err)
	}
	defer otherCategoryResp.Body.Close()
	if otherCategoryResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(otherCategoryResp.Body)
		t.Fatalf("expected category create forbidden, got %d body=%s", otherCategoryResp.StatusCode, string(body))
	}

	updateSettingsBody, _ := json.Marshal(map[string]any{
		"display_name":  "forbidden",
		"description":   "",
		"banner_preset": "ocean",
	})
	otherSettingsReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/settings",
		bytes.NewReader(updateSettingsBody),
	)
	if err != nil {
		t.Fatalf("build non-owner settings request: %v", err)
	}
	otherSettingsReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	otherSettingsReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	otherSettingsReq.Header.Set("Content-Type", "application/json")

	otherSettingsResp, err := http.DefaultClient.Do(otherSettingsReq)
	if err != nil {
		t.Fatalf("non-owner settings request failed: %v", err)
	}
	defer otherSettingsResp.Body.Close()
	if otherSettingsResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(otherSettingsResp.Body)
		t.Fatalf("expected server settings forbidden, got %d body=%s", otherSettingsResp.StatusCode, string(body))
	}
}
