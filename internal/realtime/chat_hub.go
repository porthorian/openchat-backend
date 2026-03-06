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
	"github.com/openchat/openchat-backend/internal/profile"
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

type presenceMember struct {
	ClientID string `json:"client_id"`
	UserUID  string `json:"user_uid"`
	DeviceID string `json:"device_id"`
}

type channelDeparture struct {
	channelID string
	peers     []*client
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
	serverID := strings.TrimSpace(r.Header.Get("X-OpenChat-Server-ID"))
	if serverID == "" {
		serverID = strings.TrimSpace(r.URL.Query().Get("server_id"))
	}

	client := &client{
		id:            uuid.NewString(),
		userUID:       userUID,
		deviceID:      deviceID,
		serverID:      serverID,
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
	envelope := newEnvelope("chat.message.created", "", map[string]any{"event_id": "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""), "message": message})
	for _, client := range room {
		client.enqueue(envelope)
	}
}

func (h *Hub) BroadcastReadAck(update chat.ChannelReadAckUpdate) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room := h.subscribersByRoom[update.ChannelID]
	if len(room) == 0 {
		return
	}
	payload := map[string]any{
		"event_id":             "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"channel_id":           update.ChannelID,
		"user_uid":             update.UserUID,
		"last_read_message_id": update.LastReadMessageID,
		"acked_at":             update.AckedAt,
	}
	if update.CursorIndex != nil {
		payload["cursor_index"] = *update.CursorIndex
	}
	envelope := newEnvelope("chat.read_ack.updated", "", payload)
	for _, client := range room {
		client.enqueue(envelope)
	}
}

func (h *Hub) BroadcastChannelCreated(event chat.ChannelCreatedEvent) {
	serverID := strings.TrimSpace(event.ServerID)
	if serverID == "" {
		return
	}
	clients := h.clientsForServer(serverID)
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("chat.channel.created", "", map[string]any{
		"event_id":       "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"server_id":      event.ServerID,
		"group_id":       event.GroupID,
		"channel":        event.Channel,
		"created_by_uid": event.CreatedByUID,
		"created_at":     event.CreatedAt,
	})
	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) BroadcastCategoryCreated(event chat.CategoryCreatedEvent) {
	serverID := strings.TrimSpace(event.ServerID)
	if serverID == "" {
		return
	}
	clients := h.clientsForServer(serverID)
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("chat.category.created", "", map[string]any{
		"event_id":       "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"server_id":      event.ServerID,
		"group":          event.Group,
		"created_by_uid": event.CreatedByUID,
		"created_at":     event.CreatedAt,
	})
	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) BroadcastCategoryUpdated(event chat.CategoryUpdatedEvent) {
	serverID := strings.TrimSpace(event.ServerID)
	if serverID == "" {
		return
	}
	clients := h.clientsForServer(serverID)
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("chat.category.updated", "", map[string]any{
		"event_id":       "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"server_id":      event.ServerID,
		"group":          event.Group,
		"updated_by_uid": event.UpdatedByUID,
		"updated_at":     event.UpdatedAt,
	})
	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) BroadcastChannelLayoutUpdated(event chat.ChannelLayoutUpdatedEvent) {
	serverID := strings.TrimSpace(event.ServerID)
	if serverID == "" {
		return
	}
	clients := h.clientsForServer(serverID)
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("chat.channel.layout.updated", "", map[string]any{
		"event_id":       "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"server_id":      event.ServerID,
		"groups":         event.Groups,
		"updated_by_uid": event.UpdatedByUID,
		"updated_at":     event.UpdatedAt,
	})
	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) BroadcastServerUpdated(event chat.ServerUpdatedEvent) {
	serverID := strings.TrimSpace(event.ServerID)
	if serverID == "" {
		return
	}
	clients := h.clientsForServer(serverID)
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("chat.server.updated", "", map[string]any{
		"event_id":       "evt_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		"server_id":      event.ServerID,
		"display_name":   event.DisplayName,
		"description":    event.Description,
		"banner_preset":  event.BannerPreset,
		"updated_by_uid": event.UpdatedByUID,
		"updated_at":     event.UpdatedAt,
	})
	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) clientsForServer(serverID string) []*client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := make([]*client, 0, len(h.clientsByID))
	for _, c := range h.clientsByID {
		if strings.TrimSpace(c.serverID) != serverID {
			continue
		}
		clients = append(clients, c)
	}
	return clients
}

func (h *Hub) BroadcastProfileUpdated(updated profile.CanonicalProfile) {
	h.mu.RLock()
	clients := make([]*client, 0, len(h.clientsByID))
	for _, c := range h.clientsByID {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	if len(clients) == 0 {
		return
	}

	envelope := newEnvelope("profile_updated", "", map[string]any{
		"user_uid":         updated.UserUID,
		"profile_version":  updated.ProfileVersion,
		"display_name":     updated.DisplayName,
		"avatar_mode":      updated.AvatarMode,
		"avatar_preset_id": updated.AvatarPresetID,
		"avatar_asset_id":  updated.AvatarAssetID,
		"avatar_url":       updated.AvatarURL,
		"updated_at":       updated.UpdatedAt,
	})

	for _, c := range clients {
		c.enqueue(envelope)
	}
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clientsByID[c.id] = c
}

func (h *Hub) unregister(c *client) []channelDeparture {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clientsByID, c.id)
	departures := make([]channelDeparture, 0, len(c.subscriptions))
	for channelID := range c.subscriptions {
		room := h.subscribersByRoom[channelID]
		if room == nil {
			continue
		}
		if _, subscribed := room[c.id]; !subscribed {
			continue
		}
		delete(room, c.id)
		peers := make([]*client, 0, len(room))
		for _, peer := range room {
			peers = append(peers, peer)
		}
		departures = append(departures, channelDeparture{
			channelID: channelID,
			peers:     peers,
		})
		if len(room) == 0 {
			delete(h.subscribersByRoom, channelID)
		}
	}
	c.subscriptions = make(map[string]struct{})
	return departures
}

func (h *Hub) subscribe(c *client, channelID string) ([]presenceMember, []*client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.subscribersByRoom[channelID]
	if room == nil {
		room = make(map[string]*client)
		h.subscribersByRoom[channelID] = room
	}
	_, alreadySubscribed := c.subscriptions[channelID]
	room[c.id] = c
	c.subscriptions[channelID] = struct{}{}
	snapshot := make([]presenceMember, 0, len(room))
	peers := make([]*client, 0, len(room))
	for _, member := range room {
		snapshot = append(snapshot, presenceMemberFromClient(member))
		if member.id != c.id {
			peers = append(peers, member)
		}
	}
	return snapshot, peers, !alreadySubscribed
}

func (h *Hub) unsubscribe(c *client, channelID string) ([]*client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, subscribed := c.subscriptions[channelID]; !subscribed {
		return nil, false
	}
	delete(c.subscriptions, channelID)
	room := h.subscribersByRoom[channelID]
	if room == nil {
		return nil, true
	}
	delete(room, c.id)
	peers := make([]*client, 0, len(room))
	for _, peer := range room {
		peers = append(peers, peer)
	}
	if len(room) == 0 {
		delete(h.subscribersByRoom, channelID)
	}
	return peers, true
}

func (h *Hub) typingPeers(c *client, channelID string) ([]*client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, subscribed := c.subscriptions[channelID]; !subscribed {
		return nil, false
	}
	room := h.subscribersByRoom[channelID]
	if len(room) == 0 {
		return nil, true
	}
	peers := make([]*client, 0, len(room))
	for _, peer := range room {
		if peer.id == c.id {
			continue
		}
		peers = append(peers, peer)
	}
	return peers, true
}

type client struct {
	id       string
	userUID  string
	deviceID string
	serverID string
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
		snapshot, peers, joined := c.hub.subscribe(c, channelID)
		c.enqueue(newEnvelope("chat.subscribed", envelope.RequestID, map[string]any{"channel_id": channelID}))
		c.enqueue(newEnvelope("chat.presence.snapshot", "", map[string]any{
			"channel_id": channelID,
			"members":    snapshot,
		}))
		if joined {
			joinedEnvelope := newEnvelope("chat.presence.joined", "", map[string]any{
				"channel_id": channelID,
				"member":     presenceMemberFromClient(c),
			})
			for _, peer := range peers {
				peer.enqueue(joinedEnvelope)
			}
		}
	case "chat.unsubscribe":
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		_ = json.Unmarshal(envelope.Payload, &payload)
		channelID := strings.TrimSpace(payload.ChannelID)
		if channelID == "" {
			return
		}
		peers, removed := c.hub.unsubscribe(c, channelID)
		c.enqueue(newEnvelope("chat.unsubscribed", envelope.RequestID, map[string]any{"channel_id": channelID}))
		if removed {
			leftEnvelope := newEnvelope("chat.presence.left", "", map[string]any{
				"channel_id": channelID,
				"member":     presenceMemberFromClient(c),
			})
			for _, peer := range peers {
				peer.enqueue(leftEnvelope)
			}
		}
	case "chat.typing.update":
		var payload struct {
			ChannelID string `json:"channel_id"`
			IsTyping  bool   `json:"is_typing"`
		}
		_ = json.Unmarshal(envelope.Payload, &payload)
		channelID := strings.TrimSpace(payload.ChannelID)
		if channelID == "" {
			c.enqueue(errorEnvelope(envelope.RequestID, "chat_channel_required", "channel_id is required", false))
			return
		}
		peers, subscribed := c.hub.typingPeers(c, channelID)
		if !subscribed {
			c.enqueue(errorEnvelope(envelope.RequestID, "chat_not_subscribed", "channel subscription is required", false))
			return
		}
		typingEnvelope := newEnvelope("chat.typing.updated", "", map[string]any{
			"channel_id": channelID,
			"member":     presenceMemberFromClient(c),
			"is_typing":  payload.IsTyping,
		})
		for _, peer := range peers {
			peer.enqueue(typingEnvelope)
		}
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
	defer func() {
		_ = recover()
	}()
	select {
	case c.send <- envelope:
	default:
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		departures := c.hub.unregister(c)
		member := presenceMemberFromClient(c)
		for _, departure := range departures {
			leftEnvelope := newEnvelope("chat.presence.left", "", map[string]any{
				"channel_id": departure.channelID,
				"member":     member,
			})
			for _, peer := range departure.peers {
				peer.enqueue(leftEnvelope)
			}
		}
		close(c.closed)
		close(c.send)
		_ = c.conn.Close()
	})
}

func presenceMemberFromClient(c *client) presenceMember {
	return presenceMember{
		ClientID: c.id,
		UserUID:  c.userUID,
		DeviceID: c.deviceID,
	}
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
