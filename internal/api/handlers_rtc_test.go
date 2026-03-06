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

func TestIssueJoinTicketIncludesEffectiveSubscribeReceivePolicy(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:                   ":0",
		PublicBaseURL:              "http://localhost:8080",
		SignalingPath:              "/v1/rtc/signaling",
		TicketTTL:                  60 * time.Second,
		TicketSecret:               "test-secret",
		Environment:                "test",
		RTCSubscribeMaxVideoTracks: 8,
		RTCSubscribeMaxAudioTracks: 16,
		RTCSubscribeMaxVideoTracksByServer: map[string]int{
			"srv_harbor": 6,
		},
		RTCSubscribeMaxAudioTracksByServer: map[string]int{
			"srv_harbor": 12,
		},
		RTCSubscribeMaxVideoTracksByChannel: map[string]int{
			"vc_general": 3,
		},
		RTCSubscribeMaxAudioTracksByChannel: map[string]int{
			"vc_general": 9,
		},
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	resp := joinTicketRequestForTest(t, ts.URL, "vc_general", "srv_harbor")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(payload))
	}

	var payload struct {
		ServerID               string `json:"server_id"`
		SubscribeReceivePolicy struct {
			MaxVideoTracks int `json:"max_video_tracks"`
			MaxAudioTracks int `json:"max_audio_tracks"`
		} `json:"subscribe_receive_policy"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode join ticket response: %v", err)
	}
	if payload.ServerID != "srv_harbor" {
		t.Fatalf("expected server_id srv_harbor, got %s", payload.ServerID)
	}
	if payload.SubscribeReceivePolicy.MaxVideoTracks != 3 || payload.SubscribeReceivePolicy.MaxAudioTracks != 9 {
		t.Fatalf("expected channel overrides (3/9), got %+v", payload.SubscribeReceivePolicy)
	}

	serverOnlyResp := joinTicketRequestForTest(t, ts.URL, "vc_party", "srv_harbor")
	defer serverOnlyResp.Body.Close()
	if serverOnlyResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(serverOnlyResp.Body)
		t.Fatalf("unexpected server-only status: %d body=%s", serverOnlyResp.StatusCode, string(payload))
	}
	var serverOnlyPayload struct {
		SubscribeReceivePolicy struct {
			MaxVideoTracks int `json:"max_video_tracks"`
			MaxAudioTracks int `json:"max_audio_tracks"`
		} `json:"subscribe_receive_policy"`
	}
	if err := json.NewDecoder(serverOnlyResp.Body).Decode(&serverOnlyPayload); err != nil {
		t.Fatalf("decode server-only response: %v", err)
	}
	if serverOnlyPayload.SubscribeReceivePolicy.MaxVideoTracks != 6 || serverOnlyPayload.SubscribeReceivePolicy.MaxAudioTracks != 12 {
		t.Fatalf("expected server overrides (6/12), got %+v", serverOnlyPayload.SubscribeReceivePolicy)
	}
}

func TestIssueJoinTicketRejectsChannelServerMismatch(t *testing.T) {
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

	resp := joinTicketRequestForTest(t, ts.URL, "tl_vc_huddle", "srv_harbor")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 400, got %d body=%s", resp.StatusCode, string(payload))
	}
	var apiErr struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if apiErr.Code != "channel_server_mismatch" {
		t.Fatalf("expected channel_server_mismatch, got %s", apiErr.Code)
	}
}

func joinTicketRequestForTest(t *testing.T, baseURL string, channelID string, serverID string) *http.Response {
	t.Helper()
	requestBody, err := json.Marshal(map[string]any{
		"server_id": serverID,
	})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/v1/rtc/channels/"+channelID+"/join-ticket",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenChat-User-UID", "uid_rtc_join_test")
	req.Header.Set("X-OpenChat-Device-ID", "dev_rtc_join_test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send join ticket request: %v", err)
	}
	return resp
}
