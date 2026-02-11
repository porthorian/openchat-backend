package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/openchat/openchat-backend/internal/chat"
)

type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Hub struct {
	logger   *slog.Logger
	upgrader websocket.Upgrader

	mu                sync.RWMutex
	clientsByID       map[string]*client
	subscribersByRoom map[string]map[string]*client
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
		clientsByID:       make(map[string]*client),
		subscribersByRoom: make(map[string]map[string]*client),
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("chat realtime websocket upgrade failed", "error", err)
		return
	}

	userUID := strings.TrimSpace(r.Header.Get("X-OpenChat-User-UID"))
	if userUID == "" {
		userUID = strings.TrimSpace(r.URL.Query().Get("user_uid"))
	}
	if userUID == "" {
		userUID = "uid_dev_local"
	}
	deviceID := strings.TrimSpace(r.Header.Get("X-OpenChat-Device-ID"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.URL.Query().Get("device_id"))
	}
	if deviceID == "" {
		deviceID = "dev_local"
	}

	client := &client{
		id:            uuid.NewString(),
		userUID:       userUID,
		deviceID:      deviceID,
		conn:          conn,
		hub:           h,
		send:          make(chan Envelope, 64),
		subscriptions: make(map[string]struct{}),
		closed:        make(chan struct{}),
	}

	h.register(client)
	go client.writeLoop()
	client.readLoop()
}

func (h *Hub) BroadcastMessage(message chat.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room := h.subscribersByRoom[message.ChannelID]
	if len(room) == 0 {
		return
	}
	envelope := newEnvelope("chat.message.created", "", map[string]any{"message": message})
	for _, client := range room {
		client.enqueue(envelope)
	}
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clientsByID[c.id] = c
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clientsByID, c.id)
	for channelID := range c.subscriptions {
		room := h.subscribersByRoom[channelID]
		if room == nil {
			continue
		}
		delete(room, c.id)
		if len(room) == 0 {
			delete(h.subscribersByRoom, channelID)
		}
	}
}

func (h *Hub) subscribe(c *client, channelID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.subscribersByRoom[channelID]
	if room == nil {
		room = make(map[string]*client)
		h.subscribersByRoom[channelID] = room
	}
	room[c.id] = c
	c.subscriptions[channelID] = struct{}{}
}

func (h *Hub) unsubscribe(c *client, channelID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(c.subscriptions, channelID)
	room := h.subscribersByRoom[channelID]
	if room == nil {
		return
	}
	delete(room, c.id)
	if len(room) == 0 {
		delete(h.subscribersByRoom, channelID)
	}
}

type client struct {
	id       string
	userUID  string
	deviceID string
	conn     *websocket.Conn
	hub      *Hub
	send     chan Envelope

	subscriptions map[string]struct{}
	closeOnce     sync.Once
	closed        chan struct{}
}

func (c *client) readLoop() {
	defer c.close()
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var envelope Envelope
		if err := c.conn.ReadJSON(&envelope); err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		c.handleEnvelope(envelope)
	}
}

func (c *client) handleEnvelope(envelope Envelope) {
	switch envelope.Type {
	case "chat.subscribe":
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		_ = json.Unmarshal(envelope.Payload, &payload)
		channelID := strings.TrimSpace(payload.ChannelID)
		if channelID == "" {
			c.enqueue(errorEnvelope(envelope.RequestID, "chat_channel_required", "channel_id is required", false))
			return
		}
		c.hub.subscribe(c, channelID)
		c.enqueue(newEnvelope("chat.subscribed", envelope.RequestID, map[string]any{"channel_id": channelID}))
	case "chat.unsubscribe":
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		_ = json.Unmarshal(envelope.Payload, &payload)
		channelID := strings.TrimSpace(payload.ChannelID)
		if channelID == "" {
			return
		}
		c.hub.unsubscribe(c, channelID)
		c.enqueue(newEnvelope("chat.unsubscribed", envelope.RequestID, map[string]any{"channel_id": channelID}))
	case "chat.ping":
		c.enqueue(newEnvelope("chat.pong", envelope.RequestID, map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano)}))
	default:
		c.enqueue(errorEnvelope(envelope.RequestID, "chat_unknown_event", "unsupported realtime event", false))
	}
}

func (c *client) writeLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case envelope, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteJSON(envelope); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second)); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *client) enqueue(envelope Envelope) {
	select {
	case c.send <- envelope:
	default:
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		c.hub.unregister(c)
		close(c.closed)
		close(c.send)
		_ = c.conn.Close()
	})
}

func newEnvelope(eventType string, requestID string, payload any) Envelope {
	rawPayload := json.RawMessage("{}")
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err == nil {
			rawPayload = encoded
		}
	}
	return Envelope{
		Type:      eventType,
		RequestID: requestID,
		Payload:   rawPayload,
	}
}

func errorEnvelope(requestID string, code string, message string, retryable bool) Envelope {
	return newEnvelope("chat.error", requestID, map[string]any{
		"code":      code,
		"message":   message,
		"retryable": retryable,
	})
}
