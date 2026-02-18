package rtc

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type SignalingService struct {
	logger    *slog.Logger
	tokens    *TokenService
	upgrader  websocket.Upgrader
	rooms     *roomHub
	readLimit int64
}

func NewSignalingService(logger *slog.Logger, tokens *TokenService) *SignalingService {
	return &SignalingService{
		logger: logger,
		tokens: tokens,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
		rooms:     newRoomHub(),
		readLimit: 1 << 20,
	}
}

func (s *SignalingService) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("rtc websocket upgrade failed", "error", err)
		return
	}
	client := &wsClient{
		id:      uuid.NewString(),
		conn:    conn,
		service: s,
		send:    make(chan Envelope, 64),
		closed:  make(chan struct{}),
	}
	go client.writePump()
	client.readPump()
}

type wsClient struct {
	id          string
	conn        *websocket.Conn
	service     *SignalingService
	participant Participant
	send        chan Envelope
	closed      chan struct{}
	closeOnce   sync.Once
}

func (c *wsClient) readPump() {
	defer c.closeConnection()
	c.conn.SetReadLimit(c.service.readLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(40 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(40 * time.Second))
		return nil
	})

	if err := c.waitForJoin(); err != nil {
		c.sendError("", "rtc_join_denied", err.Error(), false)
		return
	}

	for {
		var envelope Envelope
		if err := c.conn.ReadJSON(&envelope); err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				return
			}
			if !errors.Is(err, websocket.ErrCloseSent) {
				c.service.logger.Debug("rtc read loop ended", "participant_id", c.participant.ParticipantID, "error", err)
			}
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(40 * time.Second))
		c.handleEnvelope(envelope)
	}
}

func (c *wsClient) waitForJoin() error {
	_ = c.conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	var envelope Envelope
	if err := c.conn.ReadJSON(&envelope); err != nil {
		return err
	}
	if envelope.Type != "rtc.join" {
		return errors.New("first signaling message must be rtc.join")
	}

	var payload struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return errors.New("invalid rtc.join payload")
	}

	claims, err := c.service.tokens.ParseAndConsume(strings.TrimSpace(payload.Ticket))
	if err != nil {
		return err
	}
	participant := Participant{
		ParticipantID: c.id,
		ChannelID:     claims.ChannelID,
		UserUID:       claims.UserUID,
		DeviceID:      claims.DeviceID,
		Permissions:   claims.Permissions,
		JoinedAt:      time.Now().UTC(),
	}
	c.participant = participant

	existing := c.service.rooms.register(c)

	joinPayload := map[string]any{
		"participant_id": participant.ParticipantID,
		"channel_id":     participant.ChannelID,
		"participants":   participantsToSummaries(existing),
		"joined_at":      participant.JoinedAt.Format(time.RFC3339),
	}
	c.enqueue(NewEnvelope("rtc.joined", participant.ChannelID, envelope.RequestID, joinPayload))

	c.service.rooms.broadcast(
		participant.ChannelID,
		NewEnvelope(
			"rtc.participant.joined",
			participant.ChannelID,
			"",
			map[string]any{"participant": participantSummaryFromParticipant(participant)},
		),
		participant.ParticipantID,
	)

	_ = c.conn.SetReadDeadline(time.Now().Add(40 * time.Second))
	return nil
}

func (c *wsClient) handleEnvelope(envelope Envelope) {
	switch envelope.Type {
	case "rtc.ping":
		c.enqueue(NewEnvelope("rtc.pong", c.participant.ChannelID, envelope.RequestID, map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano)}))
	case "rtc.leave":
		c.closeConnection()
	case "rtc.media.state":
		c.relayMediaState(envelope)
	case "rtc.offer.publish", "rtc.offer.subscribe", "rtc.answer.publish", "rtc.answer.subscribe", "rtc.ice.candidate":
		c.forwardSignal(envelope)
	default:
		c.sendError(envelope.RequestID, "rtc_unknown_event", "unsupported signaling event type", false)
	}
}

func (c *wsClient) relayMediaState(envelope Envelope) {
	var payload map[string]any
	if len(envelope.Payload) > 0 {
		_ = json.Unmarshal(envelope.Payload, &payload)
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	streamKind, _ := payload["stream_kind"].(string)
	streamKind = strings.TrimSpace(streamKind)
	switch streamKind {
	case "":
		// Presence-only media state updates are allowed without stream checks.
	case "video_camera":
		if !c.participant.Permissions.Video {
			c.sendError(envelope.RequestID, "rtc_media_denied", "participant is not allowed to publish camera video", false)
			return
		}
	case "video_screen":
		if !c.participant.Permissions.Screenshare {
			c.sendError(envelope.RequestID, "rtc_media_denied", "participant is not allowed to publish screen share", false)
			return
		}
	default:
		if strings.HasPrefix(streamKind, "audio") && !c.participant.Permissions.Speak {
			c.sendError(envelope.RequestID, "rtc_media_denied", "participant is not allowed to publish audio", false)
			return
		}
	}

	payload["participant_id"] = c.participant.ParticipantID
	payload["user_uid"] = c.participant.UserUID
	c.service.rooms.broadcast(c.participant.ChannelID, NewEnvelope("rtc.media.state", c.participant.ChannelID, envelope.RequestID, payload), "")
}

func (c *wsClient) forwardSignal(envelope Envelope) {
	var payload map[string]any
	if len(envelope.Payload) > 0 {
		_ = json.Unmarshal(envelope.Payload, &payload)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["from_participant_id"] = c.participant.ParticipantID

	targetID, _ := payload["target_participant_id"].(string)
	targetID = strings.TrimSpace(targetID)
	forward := NewEnvelope(envelope.Type, c.participant.ChannelID, envelope.RequestID, payload)

	if targetID != "" {
		if ok := c.service.rooms.sendToParticipant(c.participant.ChannelID, targetID, forward); !ok {
			c.sendError(envelope.RequestID, "rtc_target_not_found", "target participant is not available", true)
		}
		return
	}

	c.service.rooms.broadcast(c.participant.ChannelID, forward, c.participant.ParticipantID)
}

func (c *wsClient) relayToRoom(eventType string, envelope Envelope) {
	var payload map[string]any
	if len(envelope.Payload) > 0 {
		_ = json.Unmarshal(envelope.Payload, &payload)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["participant_id"] = c.participant.ParticipantID
	payload["user_uid"] = c.participant.UserUID

	c.service.rooms.broadcast(c.participant.ChannelID, NewEnvelope(eventType, c.participant.ChannelID, envelope.RequestID, payload), "")
}

func (c *wsClient) sendError(requestID string, code string, message string, retryable bool) {
	c.enqueue(NewEnvelope("rtc.error", c.participant.ChannelID, requestID, map[string]any{
		"code":      code,
		"message":   message,
		"retryable": retryable,
	}))
}

func (c *wsClient) enqueue(envelope Envelope) {
	select {
	case c.send <- envelope:
	default:
		c.service.logger.Warn("dropping signaling message due to full send queue", "participant_id", c.participant.ParticipantID, "type", envelope.Type)
	}
}

func (c *wsClient) writePump() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case envelope, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
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

func (c *wsClient) closeConnection() {
	c.closeOnce.Do(func() {
		if c.participant.ChannelID != "" {
			c.service.rooms.unregister(c.participant.ChannelID, c.participant.ParticipantID)
			c.service.rooms.broadcast(
				c.participant.ChannelID,
				NewEnvelope(
					"rtc.participant.left",
					c.participant.ChannelID,
					"",
					map[string]any{
						"participant": map[string]any{
							"participant_id": c.participant.ParticipantID,
							"user_uid":       c.participant.UserUID,
						},
					},
				),
				"",
			)
		}
		close(c.closed)
		close(c.send)
		_ = c.conn.Close()
	})
}

type roomHub struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*wsClient
}

func newRoomHub() *roomHub {
	return &roomHub{rooms: make(map[string]map[string]*wsClient)}
}

func (h *roomHub) register(client *wsClient) []Participant {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[client.participant.ChannelID]
	if room == nil {
		room = make(map[string]*wsClient)
		h.rooms[client.participant.ChannelID] = room
	}
	existing := make([]Participant, 0, len(room))
	for _, peer := range room {
		existing = append(existing, peer.participant)
	}
	room[client.participant.ParticipantID] = client
	return existing
}

func (h *roomHub) unregister(channelID string, participantID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[channelID]
	if room == nil {
		return
	}
	delete(room, participantID)
	if len(room) == 0 {
		delete(h.rooms, channelID)
	}
}

func (h *roomHub) broadcast(channelID string, envelope Envelope, exceptParticipantID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room := h.rooms[channelID]
	for participantID, client := range room {
		if exceptParticipantID != "" && participantID == exceptParticipantID {
			continue
		}
		client.enqueue(envelope)
	}
}

func (h *roomHub) sendToParticipant(channelID string, participantID string, envelope Envelope) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room := h.rooms[channelID]
	if room == nil {
		return false
	}
	client, ok := room[participantID]
	if !ok {
		return false
	}
	client.enqueue(envelope)
	return true
}

func participantsToSummaries(participants []Participant) []map[string]any {
	result := make([]map[string]any, 0, len(participants))
	for _, participant := range participants {
		result = append(result, participantSummaryFromParticipant(participant))
	}
	return result
}

func participantSummaryFromParticipant(participant Participant) map[string]any {
	return map[string]any{
		"participant_id": participant.ParticipantID,
		"channel_id":     participant.ChannelID,
		"user_uid":       participant.UserUID,
		"device_id":      participant.DeviceID,
		"permissions":    participant.Permissions,
		"joined_at":      participant.JoinedAt.Format(time.RFC3339),
	}
}
