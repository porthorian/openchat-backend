package rtc

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSignalingJoinErrorDeliveredBeforeClose(t *testing.T) {
	const secret = "contract-test-secret"
	tokens := NewTokenService(secret, time.Minute)
	service := NewSignalingService(slog.New(slog.NewTextHandler(io.Discard, nil)), tokens, SignalingConfig{})
	server := httptest.NewServer(http.HandlerFunc(service.ServeWS))
	defer server.Close()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)

	expired, _, err := NewTokenService(secret, -time.Second).Issue(IssueTicketInput{
		ServerID: "server", ChannelID: "voice", UserUID: "user", DeviceID: "device",
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, _, err := tokens.Issue(IssueTicketInput{
		ServerID: "server", ChannelID: "voice", UserUID: "user", DeviceID: "device",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokens.ParseAndConsume(replayed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ticket, code string
		retryable          bool
	}{
		{"denied", "invalid", "rtc_join_denied", false},
		{"expired", expired, "rtc_ticket_expired", true},
		{"replayed", replayed, "rtc_ticket_replayed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := dialWebSocket(t, wsURL)
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if err := conn.WriteJSON(NewEnvelope("rtc.join", "voice", "join_request", map[string]any{"ticket": tc.ticket})); err != nil {
				t.Fatal(err)
			}
			var envelope Envelope
			if err := conn.ReadJSON(&envelope); err != nil {
				t.Fatalf("rtc.error was not delivered: %v", err)
			}
			if envelope.Type != "rtc.error" || envelope.RequestID != "join_request" || envelope.ChannelID != "" {
				t.Fatalf("unexpected error envelope: %+v", envelope)
			}
			var payload struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Code != tc.code || payload.Retryable != tc.retryable {
				t.Fatalf("unexpected error semantics: %+v", payload)
			}
			if err := conn.ReadJSON(&envelope); err == nil {
				t.Fatal("expected socket close after rtc.error")
			} else if _, ok := err.(*websocket.CloseError); !ok {
				t.Fatalf("expected close frame after rtc.error, got %v", err)
			}
		})
	}

	for attempt := 0; attempt < 2; attempt++ {
		conn := dialWebSocket(t, wsURL)
		ticket, _, err := tokens.Issue(IssueTicketInput{
			ServerID: "server", ChannelID: "voice", UserUID: "user", DeviceID: "device",
			Permissions: Permissions{Speak: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.WriteJSON(NewEnvelope("rtc.join", "voice", "fresh_join", map[string]any{"ticket": ticket})); err != nil {
			t.Fatal(err)
		}
		var envelope Envelope
		if err := conn.ReadJSON(&envelope); err != nil || envelope.Type != "rtc.joined" {
			t.Fatalf("fresh ticket did not join: envelope=%+v err=%v", envelope, err)
		}
		_ = conn.Close()
	}
}
