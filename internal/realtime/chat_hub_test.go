package realtime

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openchat/openchat-backend/internal/chat"
)

func TestBroadcastChannelCreatedScopedByServerID(t *testing.T) {
	hub := NewHub(slog.Default())
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	connect := func(serverID string) *websocket.Conn {
		wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
		wsURL = wsURL + "/v1/realtime?user_uid=uid_test&device_id=desktop_test&server_id=" + serverID
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial realtime websocket failed for %s: %v", serverID, err)
		}
		return conn
	}

	harborConn := connect(chat.SeedServerIDHarbor)
	defer harborConn.Close()
	testlabConn := connect(chat.SeedServerIDTestLab)
	defer testlabConn.Close()

	hub.BroadcastChannelCreated(chat.ChannelCreatedEvent{
		ServerID:     chat.SeedServerIDHarbor,
		GroupID:      "grp_general",
		CreatedByUID: "uid_owner",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Channel: chat.Channel{
			ID:   "ch_created",
			Name: "created",
			Type: chat.ChannelTypeText,
		},
	})

	var harborEnvelope Envelope
	_ = harborConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := harborConn.ReadJSON(&harborEnvelope); err != nil {
		t.Fatalf("expected harbor event, got read error: %v", err)
	}
	if harborEnvelope.Type != "chat.channel.created" {
		t.Fatalf("expected chat.channel.created event, got %s", harborEnvelope.Type)
	}

	var payload struct {
		ServerID string `json:"server_id"`
		GroupID  string `json:"group_id"`
		Channel  struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(harborEnvelope.Payload, &payload); err != nil {
		t.Fatalf("decode envelope payload: %v", err)
	}
	if payload.ServerID != chat.SeedServerIDHarbor {
		t.Fatalf("expected payload harbor server id, got %s", payload.ServerID)
	}
	if payload.GroupID != "grp_general" {
		t.Fatalf("expected payload group_id grp_general, got %s", payload.GroupID)
	}
	if payload.Channel.ID != "ch_created" {
		t.Fatalf("expected payload channel id ch_created, got %s", payload.Channel.ID)
	}

	var testlabEnvelope Envelope
	_ = testlabConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	err := testlabConn.ReadJSON(&testlabEnvelope)
	if err == nil {
		t.Fatalf("did not expect chat.channel.created on non-matching server connection")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout waiting for unrelated server event, got %v", err)
	}
}

func TestBroadcastCategoryCreatedAndServerUpdatedScopedByServerID(t *testing.T) {
	hub := NewHub(slog.Default())
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	connect := func(serverID string) *websocket.Conn {
		wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
		wsURL = wsURL + "/v1/realtime?user_uid=uid_test&device_id=desktop_test&server_id=" + serverID
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial realtime websocket failed for %s: %v", serverID, err)
		}
		return conn
	}

	harborConn := connect(chat.SeedServerIDHarbor)
	defer harborConn.Close()
	testlabConn := connect(chat.SeedServerIDTestLab)
	defer testlabConn.Close()

	hub.BroadcastCategoryCreated(chat.CategoryCreatedEvent{
		ServerID:     chat.SeedServerIDHarbor,
		CreatedByUID: "uid_owner",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Group: chat.ChannelGroup{
			ID:       "grp_new",
			Label:    "new category",
			Kind:     "text",
			Channels: []chat.Channel{},
		},
	})

	var harborCategory Envelope
	_ = harborConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := harborConn.ReadJSON(&harborCategory); err != nil {
		t.Fatalf("expected harbor category event, got read error: %v", err)
	}
	if harborCategory.Type != "chat.category.created" {
		t.Fatalf("expected chat.category.created event, got %s", harborCategory.Type)
	}

	hub.BroadcastServerUpdated(chat.ServerUpdatedEvent{
		ServerID:     chat.SeedServerIDHarbor,
		DisplayName:  "Harbor Prime",
		Description:  "Updated description",
		BannerPreset: "ocean",
		UpdatedByUID: "uid_owner",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	})

	var harborUpdated Envelope
	_ = harborConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := harborConn.ReadJSON(&harborUpdated); err != nil {
		t.Fatalf("expected harbor server update event, got read error: %v", err)
	}
	if harborUpdated.Type != "chat.server.updated" {
		t.Fatalf("expected chat.server.updated event, got %s", harborUpdated.Type)
	}

	var unrelated Envelope
	_ = testlabConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	err := testlabConn.ReadJSON(&unrelated)
	if err == nil {
		t.Fatalf("did not expect scoped events on non-matching server connection")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout waiting for unrelated server events, got %v", err)
	}
}

func TestBroadcastCategoryUpdatedAndChannelLayoutUpdatedScopedByServerID(t *testing.T) {
	hub := NewHub(slog.Default())
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer server.Close()

	connect := func(serverID string) *websocket.Conn {
		wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
		wsURL = wsURL + "/v1/realtime?user_uid=uid_test&device_id=desktop_test&server_id=" + serverID
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial realtime websocket failed for %s: %v", serverID, err)
		}
		return conn
	}

	harborConn := connect(chat.SeedServerIDHarbor)
	defer harborConn.Close()
	testlabConn := connect(chat.SeedServerIDTestLab)
	defer testlabConn.Close()

	hub.BroadcastCategoryUpdated(chat.CategoryUpdatedEvent{
		ServerID:     chat.SeedServerIDHarbor,
		UpdatedByUID: "uid_owner",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		Group: chat.ChannelGroup{
			ID:       "grp_general",
			Label:    "General Updated",
			Kind:     "text",
			Channels: []chat.Channel{{ID: "ch_general", Name: "general", Type: chat.ChannelTypeText}},
		},
	})

	var harborCategory Envelope
	_ = harborConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := harborConn.ReadJSON(&harborCategory); err != nil {
		t.Fatalf("expected harbor category update event, got read error: %v", err)
	}
	if harborCategory.Type != "chat.category.updated" {
		t.Fatalf("expected chat.category.updated event, got %s", harborCategory.Type)
	}

	hub.BroadcastChannelLayoutUpdated(chat.ChannelLayoutUpdatedEvent{
		ServerID:     chat.SeedServerIDHarbor,
		UpdatedByUID: "uid_owner",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
		Groups: []chat.ChannelGroup{
			{
				ID:       "grp_general",
				Label:    "General Updated",
				Kind:     "text",
				Channels: []chat.Channel{{ID: "ch_general", Name: "general", Type: chat.ChannelTypeText}},
			},
		},
	})

	var harborLayout Envelope
	_ = harborConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := harborConn.ReadJSON(&harborLayout); err != nil {
		t.Fatalf("expected harbor channel layout update event, got read error: %v", err)
	}
	if harborLayout.Type != "chat.channel.layout.updated" {
		t.Fatalf("expected chat.channel.layout.updated event, got %s", harborLayout.Type)
	}

	_ = testlabConn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	err := testlabConn.ReadJSON(&Envelope{})
	if err == nil {
		t.Fatalf("did not expect scoped update events on non-matching server connection")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout waiting for unrelated server events, got %v", err)
	}
}
