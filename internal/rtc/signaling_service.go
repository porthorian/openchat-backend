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
	logger     *slog.Logger
	tokens     *TokenService
	webrtc     *PionEngine
	upgrader   websocket.Upgrader
	rooms      *roomHub
	readLimit  int64
	mediaHints *mediaHintRegistry
}

type participantMediaHints struct {
	trackKindByID  map[string]TrackStreamKind
	streamKindByID map[string]TrackStreamKind
}

type mediaHintRegistry struct {
	mu        sync.RWMutex
	byChannel map[string]map[string]*participantMediaHints
}

func newMediaHintRegistry() *mediaHintRegistry {
	return &mediaHintRegistry{
		byChannel: make(map[string]map[string]*participantMediaHints),
	}
}

func NewSignalingService(logger *slog.Logger, tokens *TokenService, cfg SignalingConfig) *SignalingService {
	service := &SignalingService{
		logger: logger,
		tokens: tokens,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
		rooms:      newRoomHub(),
		readLimit:  1 << 20,
		mediaHints: newMediaHintRegistry(),
	}
	service.webrtc = NewPionEngine(
		logger,
		cfg,
		service.emitLocalICECandidate,
		service.emitSubscribeRefresh,
		service.emitTrackPublished,
		service.emitTrackUnpublished,
	)
	return service
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
	c.service.webrtc.RegisterParticipant(participant)

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
	case "rtc.offer.publish":
		c.handleOffer(envelope, SignalDirectionPublish, "rtc.answer.publish")
	case "rtc.offer.subscribe":
		c.handleOffer(envelope, SignalDirectionSubscribe, "rtc.answer.subscribe")
	case "rtc.answer.publish", "rtc.answer.subscribe":
		c.forwardSignal(envelope)
	case "rtc.ice.candidate":
		c.handleICECandidate(envelope)
	default:
		c.sendError(envelope.RequestID, "rtc_unknown_event", "unsupported signaling event type", false)
	}
}

func (c *wsClient) handleOffer(envelope Envelope, direction SignalDirection, responseType string) {
	var payload SessionDescriptionPayload
	if len(envelope.Payload) > 0 {
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			c.sendError(envelope.RequestID, "rtc_invalid_offer", "invalid offer payload", false)
			return
		}
	}
	c.service.logger.Info(
		"rtc offer received",
		"participant_id", c.participant.ParticipantID,
		"channel_id", c.participant.ChannelID,
		"direction", direction,
		"request_id", envelope.RequestID,
		"sdp_len", len(strings.TrimSpace(payload.SDP)),
	)

	answer, err := c.service.webrtc.HandleOffer(c.participant.ParticipantID, direction, payload)
	if err != nil {
		c.service.logger.Warn(
			"rtc offer handling failed",
			"participant_id", c.participant.ParticipantID,
			"channel_id", c.participant.ChannelID,
			"direction", direction,
			"request_id", envelope.RequestID,
			"error", err,
		)
		c.sendError(envelope.RequestID, "rtc_negotiation_failed", err.Error(), true)
		return
	}
	c.service.logger.Info(
		"rtc answer generated",
		"participant_id", c.participant.ParticipantID,
		"channel_id", c.participant.ChannelID,
		"direction", direction,
		"request_id", envelope.RequestID,
		"sdp_len", len(strings.TrimSpace(answer.SDP)),
	)

	c.enqueue(NewEnvelope(responseType, c.participant.ChannelID, envelope.RequestID, map[string]any{
		"type":      answer.Type,
		"sdp":       answer.SDP,
		"direction": string(direction),
	}))
}

func (c *wsClient) handleICECandidate(envelope Envelope) {
	var payload struct {
		Candidate     string  `json:"candidate"`
		SDPMid        *string `json:"sdp_mid"`
		SDPMLineIndex *uint16 `json:"sdp_mline_index"`
		Direction     string  `json:"direction"`
	}
	if len(envelope.Payload) > 0 {
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			c.sendError(envelope.RequestID, "rtc_invalid_ice_candidate", "invalid ice candidate payload", false)
			return
		}
	}

	direction := SignalDirectionPublish
	if trimmed := strings.TrimSpace(payload.Direction); trimmed != "" {
		parsed, err := parseSignalDirection(trimmed)
		if err != nil {
			c.sendError(envelope.RequestID, "rtc_candidate_direction_invalid", err.Error(), false)
			return
		}
		direction = parsed
	}

	err := c.service.webrtc.AddICECandidate(c.participant.ParticipantID, direction, ICECandidatePayload{
		Candidate:     payload.Candidate,
		SDPMid:        payload.SDPMid,
		SDPMLineIndex: payload.SDPMLineIndex,
	})
	if err != nil {
		c.sendError(envelope.RequestID, "rtc_candidate_rejected", err.Error(), true)
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

	streamKind := strings.TrimSpace(stringFromAny(payload["stream_kind"]))
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

	if parsedStreamKind, ok := parseTrackStreamKind(streamKind); ok {
		trackID := strings.TrimSpace(stringFromAny(payload["track_id"]))
		streamID := strings.TrimSpace(stringFromAny(payload["stream_id"]))
		action := strings.ToLower(strings.TrimSpace(stringFromAny(payload["action"])))
		if action != "stop" {
			action = "start"
		}
		c.service.mediaHints.apply(c.participant.ChannelID, c.participant.ParticipantID, trackID, streamID, parsedStreamKind, action)
	}

	// rtc.media.state remains control/presence only; do not relay payload-sized media chunks.
	delete(payload, "chunk_b64")
	delete(payload, "chunk_seq")
	delete(payload, "sample_rate_hz")
	delete(payload, "channels")
	delete(payload, "encoding")

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
		c.service.webrtc.RemoveParticipant(c.participant.ParticipantID)
		c.service.mediaHints.clearParticipant(c.participant.ChannelID, c.participant.ParticipantID)
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

func (s *SignalingService) emitLocalICECandidate(participant Participant, direction SignalDirection, candidate ICECandidatePayload) {
	payload := map[string]any{
		"candidate": candidate.Candidate,
		"direction": string(direction),
	}
	if candidate.SDPMid != nil {
		payload["sdp_mid"] = *candidate.SDPMid
	}
	if candidate.SDPMLineIndex != nil {
		payload["sdp_mline_index"] = *candidate.SDPMLineIndex
	}
	envelope := NewEnvelope("rtc.ice.candidate", participant.ChannelID, "", payload)
	if ok := s.rooms.sendToParticipant(participant.ChannelID, participant.ParticipantID, envelope); !ok {
		s.logger.Debug(
			"unable to deliver local rtc ice candidate",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
		)
	}
}

func (s *SignalingService) emitSubscribeRefresh(participant Participant, reason string) {
	payload := map[string]any{
		"reason": strings.TrimSpace(reason),
	}
	envelope := NewEnvelope("rtc.subscribe.refresh", participant.ChannelID, "", payload)
	if ok := s.rooms.sendToParticipant(participant.ChannelID, participant.ParticipantID, envelope); !ok {
		s.logger.Debug(
			"unable to deliver rtc subscribe refresh",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"reason", reason,
		)
		return
	}
	s.logger.Debug(
		"rtc subscribe refresh delivered",
		"participant_id", participant.ParticipantID,
		"channel_id", participant.ChannelID,
		"reason", reason,
	)
}

func (s *SignalingService) emitTrackPublished(participant Participant, lifecycle TrackLifecycle) {
	lifecycle.StreamKind = s.mediaHints.resolveStreamKind(
		participant.ChannelID,
		participant.ParticipantID,
		lifecycle.TrackID,
		lifecycle.StreamID,
		lifecycle.MediaKind,
	)
	envelope := NewEnvelope("rtc.track.published", participant.ChannelID, "", lifecycle)
	s.rooms.broadcast(participant.ChannelID, envelope, "")
}

func (s *SignalingService) emitTrackUnpublished(participant Participant, lifecycle TrackLifecycle) {
	lifecycle.StreamKind = s.mediaHints.resolveStreamKind(
		participant.ChannelID,
		participant.ParticipantID,
		lifecycle.TrackID,
		lifecycle.StreamID,
		lifecycle.MediaKind,
	)
	envelope := NewEnvelope("rtc.track.unpublished", participant.ChannelID, "", lifecycle)
	s.rooms.broadcast(participant.ChannelID, envelope, "")
	s.mediaHints.apply(participant.ChannelID, participant.ParticipantID, lifecycle.TrackID, lifecycle.StreamID, lifecycle.StreamKind, "stop")
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func parseTrackStreamKind(value string) (TrackStreamKind, bool) {
	switch strings.TrimSpace(value) {
	case string(TrackStreamKindAudioMicrophone):
		return TrackStreamKindAudioMicrophone, true
	case string(TrackStreamKindVideoCamera):
		return TrackStreamKindVideoCamera, true
	case string(TrackStreamKindVideoScreen):
		return TrackStreamKindVideoScreen, true
	default:
		return "", false
	}
}

func (m *mediaHintRegistry) ensureParticipantHintsLocked(channelID string, participantID string) *participantMediaHints {
	channel := m.byChannel[channelID]
	if channel == nil {
		channel = make(map[string]*participantMediaHints)
		m.byChannel[channelID] = channel
	}
	hints := channel[participantID]
	if hints == nil {
		hints = &participantMediaHints{
			trackKindByID:  make(map[string]TrackStreamKind),
			streamKindByID: make(map[string]TrackStreamKind),
		}
		channel[participantID] = hints
	}
	return hints
}

func (m *mediaHintRegistry) apply(
	channelID string,
	participantID string,
	trackID string,
	streamID string,
	streamKind TrackStreamKind,
	action string,
) {
	channelID = strings.TrimSpace(channelID)
	participantID = strings.TrimSpace(participantID)
	if channelID == "" || participantID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	channel := m.byChannel[channelID]
	if channel == nil {
		if action == "stop" {
			return
		}
		channel = make(map[string]*participantMediaHints)
		m.byChannel[channelID] = channel
	}
	hints := channel[participantID]
	if hints == nil {
		if action == "stop" {
			return
		}
		hints = &participantMediaHints{
			trackKindByID:  make(map[string]TrackStreamKind),
			streamKindByID: make(map[string]TrackStreamKind),
		}
		channel[participantID] = hints
	}
	if action == "stop" {
		if trackID != "" {
			delete(hints.trackKindByID, trackID)
		}
		if streamID != "" {
			delete(hints.streamKindByID, streamID)
		}
		if len(hints.trackKindByID) == 0 && len(hints.streamKindByID) == 0 {
			delete(channel, participantID)
		}
		if len(channel) == 0 {
			delete(m.byChannel, channelID)
		}
		return
	}
	if trackID != "" {
		hints.trackKindByID[trackID] = streamKind
	}
	if streamID != "" {
		hints.streamKindByID[streamID] = streamKind
	}
}

func (m *mediaHintRegistry) resolveStreamKind(
	channelID string,
	participantID string,
	trackID string,
	streamID string,
	mediaKind TrackMediaKind,
) TrackStreamKind {
	channelID = strings.TrimSpace(channelID)
	participantID = strings.TrimSpace(participantID)
	trackID = strings.TrimSpace(trackID)
	streamID = strings.TrimSpace(streamID)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel := m.byChannel[channelID]; channel != nil {
		if hints := channel[participantID]; hints != nil {
			if trackID != "" {
				if streamKind, ok := hints.trackKindByID[trackID]; ok {
					return streamKind
				}
			}
			if streamID != "" {
				if streamKind, ok := hints.streamKindByID[streamID]; ok {
					return streamKind
				}
			}
		}
	}
	if mediaKind == TrackMediaKindVideo {
		return TrackStreamKindVideoCamera
	}
	return TrackStreamKindAudioMicrophone
}

func (m *mediaHintRegistry) clearParticipant(channelID string, participantID string) {
	channelID = strings.TrimSpace(channelID)
	participantID = strings.TrimSpace(participantID)
	if channelID == "" || participantID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	channel := m.byChannel[channelID]
	if channel == nil {
		return
	}
	delete(channel, participantID)
	if len(channel) == 0 {
		delete(m.byChannel, channelID)
	}
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
