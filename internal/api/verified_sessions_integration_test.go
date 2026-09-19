package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/auth"
	"github.com/openchat/openchat-backend/internal/chat"
	"github.com/openchat/openchat-backend/internal/store/postgres"
)

func TestProductionSessionRejectsForgedUIDHeader(t *testing.T) {
	dsn := os.Getenv("OPENCHAT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCHAT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	serverID := chat.SeedServerIDHarbor
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Harbor') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM servers WHERE server_id=$1`, serverID)
	svc := auth.New(pool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := svc.BeginChallenge(ctx, serverID, "", "test-device", pub)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(priv, []byte(challenge.Payload)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role) VALUES($1,$2,'member')`, serverID, session.UserUID); err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Environment: "production", PublicBaseURL: "https://chat.example.test", SignalingPath: "/v1/rtc/signaling", TicketSecret: "test-secret", TicketTTL: time.Minute}
	api := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAuthService(svc)
	server := httptest.NewServer(api.Router())
	defer server.Close()
	endpoint := server.URL + "/v1/profile/me"
	forged, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	forged.Header.Set("X-OpenChat-User-UID", session.UserUID)
	response, err := http.DefaultClient.Do(forged)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("header alone returned %d", response.StatusCode)
	}
	signed, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	signed.Header.Set("Authorization", "Bearer "+session.Token)
	signed.Header.Set("X-OpenChat-User-UID", "uid_impersonated")
	response, err = http.DefaultClient.Do(signed)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || bytes.Contains(body, []byte("uid_impersonated")) {
		t.Fatalf("verified request returned %d: %s", response.StatusCode, body)
	}
	var profile map[string]any
	if err := json.Unmarshal(body, &profile); err != nil {
		t.Fatal(err)
	}
}

func TestVerifiedRealtimeCannotSubscribeAcrossServers(t *testing.T) {
	dsn := os.Getenv("OPENCHAT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCHAT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	serverID := chat.SeedServerIDHarbor
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Harbor') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM servers WHERE server_id=$1`, serverID)
	svc := auth.New(pool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := svc.BeginChallenge(ctx, serverID, "", "ws-device", pub)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(priv, []byte(challenge.Payload)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role) VALUES($1,$2,'member')`, serverID, session.UserUID); err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Environment: "production", PublicBaseURL: "https://chat.example.test", SignalingPath: "/v1/rtc/signaling", TicketSecret: "test-secret", TicketTTL: time.Minute}
	api := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAuthService(svc)
	server := httptest.NewServer(api.Router())
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"openchat.session." + session.Token}}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime?user_uid=uid_forged&server_id="+chat.SeedServerIDTestLab, nil)
	if err != nil {
		t.Fatalf("realtime dial: %v status=%v", err, response)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "chat.subscribe", "request_id": "cross", "payload": map[string]string{"channel_id": "tl_ch_general"}}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var envelope struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "chat.error" || envelope.Payload["code"] != "chat_channel_forbidden" {
		t.Fatalf("cross-server subscribe: %+v", envelope)
	}
}

func TestRemovedMemberCannotUseHTTPOrRTCJoinTicket(t *testing.T) {
	dsn := os.Getenv("OPENCHAT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCHAT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	serverID := chat.SeedServerIDHarbor
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Harbor') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM servers WHERE server_id=$1`, serverID)
	svc := auth.New(pool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := svc.BeginChallenge(ctx, serverID, "", "removed-device", pub)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(priv, []byte(challenge.Payload)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role) VALUES($1,$2,'member')`, serverID, session.UserUID); err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Environment: "production", PublicBaseURL: "https://chat.example.test", SignalingPath: "/v1/rtc/signaling", TicketSecret: "test-secret", TicketTTL: time.Minute}
	api := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAuthService(svc)
	server := httptest.NewServer(api.Router())
	defer server.Close()
	ticketReq, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/rtc/channels/vc_general/join-ticket", strings.NewReader(`{"server_id":"`+serverID+`"}`))
	ticketReq.Header.Set("Authorization", "Bearer "+session.Token)
	ticketReq.Header.Set("Content-Type", "application/json")
	ticketResp, err := http.DefaultClient.Do(ticketReq)
	if err != nil {
		t.Fatal(err)
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(ticketResp.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	ticketResp.Body.Close()
	if ticketResp.StatusCode != 200 || ticket.Ticket == "" {
		t.Fatalf("ticket status=%d", ticketResp.StatusCode)
	}
	if _, err := pool.Exec(ctx, `UPDATE server_memberships SET membership_state='removed',removed_at=now() WHERE server_id=$1 AND user_uid=$2`, serverID, session.UserUID); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/servers/"+serverID+"/channels", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("removed member read channels: %d", resp.StatusCode)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/rtc/signaling", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "rtc.join", "request_id": "removed", "channel_id": "vc_general", "payload": map[string]string{"ticket": ticket.Ticket}}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var envelope struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "rtc.error" || envelope.Payload["code"] != "rtc_join_denied" {
		t.Fatalf("removed member joined RTC: %+v", envelope)
	}
}
