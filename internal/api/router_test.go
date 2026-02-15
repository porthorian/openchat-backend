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

func TestLeaveServerMembershipRemovesServerFromRequesterDirectory(t *testing.T) {
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

	httpClient := &http.Client{}
	requesterUID := "uid_leave_test"
	requesterDeviceID := "desktop_test"

	leaveReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/v1/servers/srv_testlab/membership", nil)
	if err != nil {
		t.Fatalf("build leave request: %v", err)
	}
	leaveReq.Header.Set("X-OpenChat-User-UID", requesterUID)
	leaveReq.Header.Set("X-OpenChat-Device-ID", requesterDeviceID)

	leaveResp, err := httpClient.Do(leaveReq)
	if err != nil {
		t.Fatalf("leave membership request failed: %v", err)
	}
	defer leaveResp.Body.Close()
	if leaveResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(leaveResp.Body)
		t.Fatalf("unexpected leave status: %d body=%s", leaveResp.StatusCode, string(body))
	}

	serversReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/servers", nil)
	if err != nil {
		t.Fatalf("build servers request: %v", err)
	}
	serversReq.Header.Set("X-OpenChat-User-UID", requesterUID)
	serversReq.Header.Set("X-OpenChat-Device-ID", requesterDeviceID)

	serversResp, err := httpClient.Do(serversReq)
	if err != nil {
		t.Fatalf("servers request failed: %v", err)
	}
	defer serversResp.Body.Close()
	if serversResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(serversResp.Body)
		t.Fatalf("unexpected servers status: %d body=%s", serversResp.StatusCode, string(body))
	}

	var requesterPayload struct {
		Servers []struct {
			ServerID string `json:"server_id"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(serversResp.Body).Decode(&requesterPayload); err != nil {
		t.Fatalf("decode requester payload: %v", err)
	}
	if len(requesterPayload.Servers) != 1 {
		t.Fatalf("expected 1 server after leave action, got %d", len(requesterPayload.Servers))
	}
	if requesterPayload.Servers[0].ServerID != "srv_harbor" {
		t.Fatalf("expected remaining server srv_harbor, got %s", requesterPayload.Servers[0].ServerID)
	}

	otherReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/servers", nil)
	if err != nil {
		t.Fatalf("build second requester request: %v", err)
	}
	otherReq.Header.Set("X-OpenChat-User-UID", "uid_leave_other")
	otherReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")

	otherResp, err := httpClient.Do(otherReq)
	if err != nil {
		t.Fatalf("second requester servers request failed: %v", err)
	}
	defer otherResp.Body.Close()
	if otherResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(otherResp.Body)
		t.Fatalf("unexpected second requester servers status: %d body=%s", otherResp.StatusCode, string(body))
	}

	var otherPayload struct {
		Servers []struct {
			ServerID string `json:"server_id"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(otherResp.Body).Decode(&otherPayload); err != nil {
		t.Fatalf("decode second requester payload: %v", err)
	}
	if len(otherPayload.Servers) != 2 {
		t.Fatalf("expected 2 servers for second requester, got %d", len(otherPayload.Servers))
	}
}
