package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/app"
)

func TestCapabilitiesEndpoint(t *testing.T) {
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

	resp, err := http.Get(ts.URL + "/v1/client/capabilities")
	if err != nil {
		t.Fatalf("capabilities request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(body))
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload["server_id"] == nil {
		t.Fatalf("expected server_id in capabilities response")
	}
	if payload["rtc"] == nil {
		t.Fatalf("expected rtc payload in capabilities response")
	}
	if payload["profile"] == nil {
		t.Fatalf("expected profile payload in capabilities response")
	}
	if payload["build_version"] == nil {
		t.Fatalf("expected build_version in capabilities response")
	}
	if payload["build_commit"] == nil {
		t.Fatalf("expected build_commit in capabilities response")
	}
}

func TestServerDirectoryEndpoint(t *testing.T) {
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

	resp, err := http.Get(ts.URL + "/v1/servers")
	if err != nil {
		t.Fatalf("servers request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(body))
	}

	var payload struct {
		Servers []struct {
			ServerID string `json:"server_id"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Servers) == 0 {
		t.Fatalf("expected non-empty servers payload")
	}

	var hasHarbor bool
	var hasTestLab bool
	for _, server := range payload.Servers {
		if server.ServerID == "srv_harbor" {
			hasHarbor = true
		}
		if server.ServerID == "srv_testlab" {
			hasTestLab = true
		}
	}
	if !hasHarbor {
		t.Fatalf("expected srv_harbor in servers payload")
	}
	if !hasTestLab {
		t.Fatalf("expected srv_testlab in servers payload")
	}
}
