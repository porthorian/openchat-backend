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
)

func TestResolveMentionsReturnsTokensAndUserCandidates(t *testing.T) {
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

	resolveReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/channels/ch_general/mentions:resolve?query=", nil)
	if err != nil {
		t.Fatalf("build resolve request: %v", err)
	}
	resolveReq.Header.Set("X-OpenChat-User-UID", "uid_resolve_test")
	resolveReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")

	resolveResp, err := http.DefaultClient.Do(resolveReq)
	if err != nil {
		t.Fatalf("send resolve request: %v", err)
	}
	defer resolveResp.Body.Close()
	if resolveResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resolveResp.Body)
		t.Fatalf("unexpected resolve status: %d body=%s", resolveResp.StatusCode, string(body))
	}

	var payload struct {
		Results []struct {
			Type     string `json:"type"`
			Token    string `json:"token"`
			TargetID string `json:"target_id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resolveResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode resolve payload: %v", err)
	}

	hasHere := false
	hasChannel := false
	hasRequester := false
	for _, result := range payload.Results {
		if result.Type == "channel" && result.Token == "@here" {
			hasHere = true
		}
		if result.Type == "channel" && result.Token == "@channel" {
			hasChannel = true
		}
		if result.Type == "user" && result.TargetID == "uid_resolve_test" {
			hasRequester = true
		}
	}

	if !hasHere {
		t.Fatalf("expected @here candidate")
	}
	if !hasChannel {
		t.Fatalf("expected @channel candidate")
	}
	if !hasRequester {
		t.Fatalf("expected requester uid in mention candidates")
	}
}

func TestReadAckMonotonicCursor(t *testing.T) {
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

	putReadAck := func(lastReadMessageID string) (int, map[string]any) {
		payload := map[string]string{"last_read_message_id": lastReadMessageID}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal read ack payload: %v", err)
		}
		req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/channels/ch_general/read-ack", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("build put read ack request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-OpenChat-User-UID", "uid_read_ack")
		req.Header.Set("X-OpenChat-Device-ID", "desktop_test")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("send put read ack request: %v", err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode put read ack payload: %v", err)
		}
		return resp.StatusCode, body
	}

	status, first := putReadAck("msg_seed_02")
	if status != http.StatusOK {
		t.Fatalf("expected first put status 200, got %d", status)
	}
	if applied, ok := first["applied"].(bool); !ok || !applied {
		t.Fatalf("expected first read ack write to apply")
	}

	status, second := putReadAck("msg_seed_01")
	if status != http.StatusOK {
		t.Fatalf("expected second put status 200, got %d", status)
	}
	if applied, ok := second["applied"].(bool); !ok || applied {
		t.Fatalf("expected stale read ack write to be ignored")
	}

	readAckPayload, ok := second["read_ack"].(map[string]any)
	if !ok {
		t.Fatalf("expected read_ack object in second response")
	}
	if got := readAckPayload["last_read_message_id"]; got != "msg_seed_02" {
		t.Fatalf("expected stale write to preserve msg_seed_02, got %#v", got)
	}

	getReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/channels/ch_general/read-ack", nil)
	if err != nil {
		t.Fatalf("build get read ack request: %v", err)
	}
	getReq.Header.Set("X-OpenChat-User-UID", "uid_read_ack")
	getReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")

	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("send get read ack request: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("unexpected get read ack status: %d body=%s", getResp.StatusCode, string(body))
	}

	var getPayload struct {
		ReadAck struct {
			LastReadMessageID string `json:"last_read_message_id"`
		} `json:"read_ack"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getPayload); err != nil {
		t.Fatalf("decode get read ack payload: %v", err)
	}
	if getPayload.ReadAck.LastReadMessageID != "msg_seed_02" {
		t.Fatalf("expected persisted read ack to be msg_seed_02, got %q", getPayload.ReadAck.LastReadMessageID)
	}
}

func TestReadAckRejectsUnknownMessageCursor(t *testing.T) {
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

	payload := map[string]string{"last_read_message_id": "msg_missing_404"}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal read ack payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/channels/ch_general/read-ack", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build read ack request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenChat-User-UID", "uid_read_ack")
	req.Header.Set("X-OpenChat-Device-ID", "desktop_test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send read ack request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 400, got %d body=%s", resp.StatusCode, string(body))
	}

	var apiErr struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode read ack error payload: %v", err)
	}
	if apiErr.Code != "read_ack_cursor_not_found" {
		t.Fatalf("expected read_ack_cursor_not_found code, got %s", apiErr.Code)
	}
}
