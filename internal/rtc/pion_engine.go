package rtc

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

type SignalingConfig struct {
	ICEServers []ICEServerConfig
}

type ICEServerConfig struct {
	URLs           []string
	Username       string
	Credential     string
	CredentialType string
}

type SignalDirection string

const (
	SignalDirectionPublish   SignalDirection = "publish"
	SignalDirectionSubscribe SignalDirection = "subscribe"
)

type SessionDescriptionPayload struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type ICECandidatePayload struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdp_mline_index,omitempty"`
}

func parseSignalDirection(raw string) (SignalDirection, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(SignalDirectionPublish):
		return SignalDirectionPublish, nil
	case string(SignalDirectionSubscribe):
		return SignalDirectionSubscribe, nil
	default:
		return "", fmt.Errorf("unsupported signal direction: %q", raw)
	}
}

type localICECandidateEmitter func(participant Participant, direction SignalDirection, candidate ICECandidatePayload)
type subscribeRefreshEmitter func(participant Participant, reason string)
type trackLifecycleEmitter func(participant Participant, lifecycle TrackLifecycle)

type PionEngine struct {
	logger               *slog.Logger
	api                  *webrtc.API
	config               webrtc.Configuration
	emitICE              localICECandidateEmitter
	emitSubscribeRefresh subscribeRefreshEmitter
	emitTrackPublished   trackLifecycleEmitter
	emitTrackUnpublished trackLifecycleEmitter

	mu                   sync.Mutex
	sessions             map[string]*pionSession
	relayTracksByChannel map[string]map[string]*relayTrack
}

type pionSession struct {
	participant Participant

	publishPC    *webrtc.PeerConnection
	subscribePC  *webrtc.PeerConnection
	relaySenders map[string]*webrtc.RTPSender

	pendingPublishCandidates   []webrtc.ICECandidateInit
	pendingSubscribeCandidates []webrtc.ICECandidateInit
}

type relayTrack struct {
	channelID           string
	sourceParticipantID string
	sourceTrackID       string
	sourceStreamID      string
	sourceSSRC          uint32
	kind                webrtc.RTPCodecType
	localTrack          *webrtc.TrackLocalStaticRTP
}

func toTrackMediaKind(kind webrtc.RTPCodecType) TrackMediaKind {
	if kind == webrtc.RTPCodecTypeVideo {
		return TrackMediaKindVideo
	}
	return TrackMediaKindAudio
}

func defaultStreamKindForMediaKind(mediaKind TrackMediaKind) TrackStreamKind {
	if mediaKind == TrackMediaKindVideo {
		return TrackStreamKindVideoCamera
	}
	return TrackStreamKindAudioMicrophone
}

func NewPionEngine(
	logger *slog.Logger,
	cfg SignalingConfig,
	emit localICECandidateEmitter,
	emitRefresh subscribeRefreshEmitter,
	emitTrackPublished trackLifecycleEmitter,
	emitTrackUnpublished trackLifecycleEmitter,
) *PionEngine {
	if logger == nil {
		logger = slog.Default()
	}
	configuration := webrtc.Configuration{
		ICEServers: make([]webrtc.ICEServer, 0, len(cfg.ICEServers)),
	}
	for _, ice := range cfg.ICEServers {
		server := webrtc.ICEServer{
			URLs:     append([]string(nil), ice.URLs...),
			Username: strings.TrimSpace(ice.Username),
		}
		if credential := strings.TrimSpace(ice.Credential); credential != "" {
			server.Credential = credential
		}
		switch strings.ToLower(strings.TrimSpace(ice.CredentialType)) {
		case "", "password":
			server.CredentialType = webrtc.ICECredentialTypePassword
		case "oauth":
			server.CredentialType = webrtc.ICECredentialTypeOauth
		default:
			server.CredentialType = webrtc.ICECredentialTypePassword
		}
		configuration.ICEServers = append(configuration.ICEServers, server)
	}

	return &PionEngine{
		logger:               logger,
		api:                  webrtc.NewAPI(),
		config:               configuration,
		emitICE:              emit,
		emitSubscribeRefresh: emitRefresh,
		emitTrackPublished:   emitTrackPublished,
		emitTrackUnpublished: emitTrackUnpublished,
		sessions:             make(map[string]*pionSession),
		relayTracksByChannel: make(map[string]map[string]*relayTrack),
	}
}

func (e *PionEngine) RegisterParticipant(participant Participant) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.sessions[participant.ParticipantID]; ok {
		existing.participant = participant
		return
	}
	e.sessions[participant.ParticipantID] = &pionSession{
		participant: participant,
	}
}

func (e *PionEngine) RemoveParticipant(participantID string) {
	e.mu.Lock()
	session, ok := e.sessions[participantID]
	if ok {
		e.removeSourceRelayTracksLocked(session.participant.ChannelID, session.participant.ParticipantID)
		delete(e.sessions, participantID)
	}
	e.mu.Unlock()
	if !ok {
		return
	}

	if session.publishPC != nil {
		_ = session.publishPC.Close()
	}
	if session.subscribePC != nil {
		_ = session.subscribePC.Close()
	}
}

func (e *PionEngine) HandleOffer(participantID string, direction SignalDirection, offer SessionDescriptionPayload) (SessionDescriptionPayload, error) {
	normalizedSDP, err := normalizeSDP(offer.SDP)
	if err != nil {
		return SessionDescriptionPayload{}, err
	}
	if offer.Type != "" && !strings.EqualFold(strings.TrimSpace(offer.Type), "offer") {
		return SessionDescriptionPayload{}, errors.New("offer type must be 'offer'")
	}

	e.mu.Lock()
	session, ok := e.sessions[participantID]
	if !ok {
		e.mu.Unlock()
		return SessionDescriptionPayload{}, errors.New("participant not registered")
	}
	participant := session.participant
	pc, err := e.getOrCreatePeerConnectionLocked(session, direction)
	if err == nil && direction == SignalDirectionSubscribe {
		e.attachRelayTracksToSubscriberLocked(session)
	}
	e.mu.Unlock()
	if err != nil {
		return SessionDescriptionPayload{}, err
	}
	e.logPeerMediaSnapshot(participant, direction, pc, "before_set_remote")

	remoteDescription := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  normalizedSDP,
	}
	if err := pc.SetRemoteDescription(remoteDescription); err != nil {
		if isConnectionClosedErr(err) {
			e.mu.Lock()
			session, ok := e.sessions[participantID]
			if !ok {
				e.mu.Unlock()
				return SessionDescriptionPayload{}, errors.New("participant not registered")
			}
			e.resetPeerConnectionLocked(session, direction, "closed-during-set-remote")
			pc, err = e.getOrCreatePeerConnectionLocked(session, direction)
			if err == nil && direction == SignalDirectionSubscribe {
				e.attachRelayTracksToSubscriberLocked(session)
			}
			e.mu.Unlock()
			if err != nil {
				return SessionDescriptionPayload{}, err
			}
			e.logPeerMediaSnapshot(participant, direction, pc, "retry_before_set_remote")
			if retryErr := pc.SetRemoteDescription(remoteDescription); retryErr == nil {
				e.logPeerMediaSnapshot(participant, direction, pc, "retry_after_set_remote")
				goto remoteApplied
			} else {
				err = retryErr
			}
		}
		e.logger.Warn(
			"failed to apply remote offer sdp",
			"participant_id", participantID,
			"direction", string(direction),
			"sdp_len", len(normalizedSDP),
			"sdp_prefix", sdpLogPrefix(normalizedSDP, 64),
			"error", err,
		)
		return SessionDescriptionPayload{}, fmt.Errorf("set remote description: %w", err)
	}
	e.logPeerMediaSnapshot(participant, direction, pc, "after_set_remote")
remoteApplied:

	// Apply any queued remote candidates that arrived before the offer.
	pending := e.takePendingCandidates(participantID, direction)
	for _, candidate := range pending {
		if err := pc.AddICECandidate(candidate); err != nil {
			return SessionDescriptionPayload{}, fmt.Errorf("apply pending remote candidate: %w", err)
		}
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return SessionDescriptionPayload{}, fmt.Errorf("create answer: %w", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return SessionDescriptionPayload{}, fmt.Errorf("set local description: %w", err)
	}
	e.logPeerMediaSnapshot(participant, direction, pc, "after_set_local")

	// Return SDP with currently available candidates while still allowing trickle.
	select {
	case <-gatherComplete:
	default:
	}
	local := pc.LocalDescription()
	if local == nil {
		return SessionDescriptionPayload{}, errors.New("local description is not available")
	}

	return SessionDescriptionPayload{
		Type: local.Type.String(),
		SDP:  local.SDP,
	}, nil
}

func normalizeSDP(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("offer sdp is required")
	}
	if !strings.HasPrefix(trimmed, "v=0") {
		return "", errors.New("offer sdp is malformed")
	}
	if !strings.Contains(trimmed, "\nm=") && !strings.Contains(trimmed, "\r\nm=") {
		return "", errors.New("offer sdp is malformed")
	}

	normalized := strings.ReplaceAll(trimmed, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return "", errors.New("offer sdp is required")
	}
	return strings.Join(filtered, "\r\n") + "\r\n", nil
}

func sdpLogPrefix(sdp string, max int) string {
	if max <= 0 {
		max = 64
	}
	clean := strings.ReplaceAll(sdp, "\r", "\\r")
	clean = strings.ReplaceAll(clean, "\n", "\\n")
	if len(clean) <= max {
		return clean
	}
	return clean[:max]
}

func (e *PionEngine) AddICECandidate(participantID string, direction SignalDirection, candidate ICECandidatePayload) error {
	candidate.Candidate = strings.TrimSpace(candidate.Candidate)
	if candidate.Candidate == "" {
		return errors.New("candidate is required")
	}

	init := webrtc.ICECandidateInit{
		Candidate:     candidate.Candidate,
		SDPMid:        candidate.SDPMid,
		SDPMLineIndex: candidate.SDPMLineIndex,
	}

	e.mu.Lock()
	session, ok := e.sessions[participantID]
	if !ok {
		e.mu.Unlock()
		return errors.New("participant not registered")
	}

	pc, pendingQueue, err := sessionPeerState(session, direction)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	if pc != nil && isPeerConnectionClosed(pc) {
		e.resetPeerConnectionLocked(session, direction, "closed-before-add-ice")
		pc, pendingQueue, err = sessionPeerState(session, direction)
		if err != nil {
			e.mu.Unlock()
			return err
		}
	}
	if pc == nil || pc.RemoteDescription() == nil {
		*pendingQueue = append(*pendingQueue, init)
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	if err := pc.AddICECandidate(init); err != nil {
		return fmt.Errorf("add candidate: %w", err)
	}
	return nil
}

func (e *PionEngine) takePendingCandidates(participantID string, direction SignalDirection) []webrtc.ICECandidateInit {
	e.mu.Lock()
	defer e.mu.Unlock()

	session, ok := e.sessions[participantID]
	if !ok {
		return nil
	}
	_, pendingQueue, err := sessionPeerState(session, direction)
	if err != nil || len(*pendingQueue) == 0 {
		return nil
	}
	pending := append([]webrtc.ICECandidateInit(nil), (*pendingQueue)...)
	*pendingQueue = (*pendingQueue)[:0]
	return pending
}

func (e *PionEngine) getOrCreatePeerConnectionLocked(session *pionSession, direction SignalDirection) (*webrtc.PeerConnection, error) {
	pc, _, err := sessionPeerState(session, direction)
	if err != nil {
		return nil, err
	}
	if pc != nil && isPeerConnectionClosed(pc) {
		e.resetPeerConnectionLocked(session, direction, "closed-before-reuse")
		pc = nil
	}
	if pc != nil {
		return pc, nil
	}

	pc, err = e.api.NewPeerConnection(e.config)
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}
	participant := session.participant
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil || e.emitICE == nil {
			return
		}
		raw := c.ToJSON()
		e.emitICE(participant, direction, ICECandidatePayload{
			Candidate:     raw.Candidate,
			SDPMid:        raw.SDPMid,
			SDPMLineIndex: raw.SDPMLineIndex,
		})
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		e.logger.Debug(
			"pion peer connection state changed",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
			"state", state.String(),
		)
	})
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		e.logger.Debug(
			"pion ice connection state changed",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
			"state", state.String(),
		)
	})
	pc.OnSignalingStateChange(func(state webrtc.SignalingState) {
		e.logger.Debug(
			"pion signaling state changed",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
			"state", state.String(),
		)
	})
	if direction == SignalDirectionPublish {
		pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			go e.relayPublishTrackRTP(participant, direction, track)
		})
	}

	switch direction {
	case SignalDirectionPublish:
		session.publishPC = pc
	case SignalDirectionSubscribe:
		session.subscribePC = pc
	default:
		_ = pc.Close()
		return nil, fmt.Errorf("unsupported signal direction: %s", direction)
	}
	return pc, nil
}

func isPeerConnectionClosed(pc *webrtc.PeerConnection) bool {
	if pc == nil {
		return true
	}
	return pc.ConnectionState() == webrtc.PeerConnectionStateClosed || pc.SignalingState() == webrtc.SignalingStateClosed
}

func isConnectionClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "connection closed")
}

func (e *PionEngine) resetPeerConnectionLocked(session *pionSession, direction SignalDirection, reason string) {
	if session == nil {
		return
	}
	var pc *webrtc.PeerConnection
	switch direction {
	case SignalDirectionPublish:
		pc = session.publishPC
		session.publishPC = nil
		session.pendingPublishCandidates = nil
	case SignalDirectionSubscribe:
		pc = session.subscribePC
		session.subscribePC = nil
		session.pendingSubscribeCandidates = nil
		session.relaySenders = nil
	default:
		return
	}
	if pc == nil {
		return
	}
	e.logger.Warn(
		"pion resetting closed peer connection",
		"participant_id", session.participant.ParticipantID,
		"channel_id", session.participant.ChannelID,
		"direction", string(direction),
		"reason", reason,
		"connection_state", pc.ConnectionState().String(),
		"signaling_state", pc.SignalingState().String(),
	)
	_ = pc.Close()
}

func relayTrackKey(sourceParticipantID string, sourceTrackID string) string {
	return sourceParticipantID + ":" + sourceTrackID
}

func (e *PionEngine) attachRelayTracksToSubscriberLocked(session *pionSession) {
	if session == nil || session.subscribePC == nil {
		return
	}
	channelTracks := e.relayTracksByChannel[session.participant.ChannelID]
	for relayKey, relay := range channelTracks {
		if relay.sourceParticipantID == session.participant.ParticipantID {
			continue
		}
		e.ensureSubscriberRelaySenderLocked(session, relayKey, relay)
	}
}

func (e *PionEngine) ensureSubscriberRelaySenderLocked(session *pionSession, relayKey string, relay *relayTrack) {
	if session == nil || session.subscribePC == nil || relay == nil {
		return
	}
	if session.relaySenders == nil {
		session.relaySenders = make(map[string]*webrtc.RTPSender)
	}
	if _, exists := session.relaySenders[relayKey]; exists {
		return
	}
	sender, err := session.subscribePC.AddTrack(relay.localTrack)
	if err != nil {
		e.logger.Warn(
			"pion failed to attach relay sender",
			"subscriber_participant_id", session.participant.ParticipantID,
			"channel_id", session.participant.ChannelID,
			"source_participant_id", relay.sourceParticipantID,
			"source_track_id", relay.sourceTrackID,
			"error", err,
		)
		return
	}
	session.relaySenders[relayKey] = sender
	e.logger.Info(
		"pion relay sender attached",
		"subscriber_participant_id", session.participant.ParticipantID,
		"channel_id", session.participant.ChannelID,
		"source_participant_id", relay.sourceParticipantID,
		"source_track_id", relay.sourceTrackID,
		"source_stream_id", relay.sourceStreamID,
		"kind", relay.kind.String(),
	)
	go e.drainRelaySenderRTCP(session.participant, sender, relay)
	if e.emitSubscribeRefresh != nil {
		go e.emitSubscribeRefresh(session.participant, "relay_attached")
	}
	go e.requestKeyFramesForRelay(relay, 3, 600*time.Millisecond)
}

func (e *PionEngine) drainRelaySenderRTCP(subscriber Participant, sender *webrtc.RTPSender, relay *relayTrack) {
	if sender == nil || relay == nil {
		return
	}
	rtcpBuf := make([]byte, 1500)
	for {
		n, _, err := sender.Read(rtcpBuf)
		if err != nil {
			e.logger.Debug(
				"pion relay sender rtcp reader ended",
				"subscriber_participant_id", subscriber.ParticipantID,
				"channel_id", subscriber.ChannelID,
				"source_participant_id", relay.sourceParticipantID,
				"source_track_id", relay.sourceTrackID,
				"error", err,
			)
			return
		}

		rtcpPackets, unmarshalErr := rtcp.Unmarshal(rtcpBuf[:n])
		if unmarshalErr != nil {
			e.logger.Debug(
				"pion relay sender rtcp decode failed",
				"subscriber_participant_id", subscriber.ParticipantID,
				"channel_id", subscriber.ChannelID,
				"source_participant_id", relay.sourceParticipantID,
				"source_track_id", relay.sourceTrackID,
				"error", unmarshalErr,
			)
			continue
		}

		feedbackPackets := make([]rtcp.Packet, 0, len(rtcpPackets))
		for _, packet := range rtcpPackets {
			switch typed := packet.(type) {
			case *rtcp.PictureLossIndication:
				feedbackPackets = append(feedbackPackets, &rtcp.PictureLossIndication{
					SenderSSRC: typed.SenderSSRC,
					MediaSSRC:  relay.sourceSSRC,
				})
			case *rtcp.TransportLayerNack:
				feedbackPackets = append(feedbackPackets, &rtcp.TransportLayerNack{
					SenderSSRC: typed.SenderSSRC,
					MediaSSRC:  relay.sourceSSRC,
					Nacks:      append([]rtcp.NackPair(nil), typed.Nacks...),
				})
			}
		}
		if len(feedbackPackets) == 0 {
			continue
		}
		if err := e.forwardRelayRTCPFeedback(relay, feedbackPackets); err != nil {
			e.logger.Debug(
				"pion relay sender rtcp forward failed",
				"subscriber_participant_id", subscriber.ParticipantID,
				"channel_id", subscriber.ChannelID,
				"source_participant_id", relay.sourceParticipantID,
				"source_track_id", relay.sourceTrackID,
				"source_ssrc", relay.sourceSSRC,
				"error", err,
			)
		}
	}
}

func (e *PionEngine) forwardRelayRTCPFeedback(relay *relayTrack, packets []rtcp.Packet) error {
	if relay == nil || len(packets) == 0 {
		return nil
	}

	e.mu.Lock()
	sourceSession := e.sessions[relay.sourceParticipantID]
	var publishPC *webrtc.PeerConnection
	if sourceSession != nil {
		publishPC = sourceSession.publishPC
	}
	e.mu.Unlock()

	if publishPC == nil {
		return errors.New("missing publish peer connection for relay source")
	}
	return publishPC.WriteRTCP(packets)
}

func (e *PionEngine) requestKeyFramesForRelay(relay *relayTrack, attempts int, interval time.Duration) {
	if relay == nil || relay.sourceSSRC == 0 || attempts <= 0 {
		return
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		e.requestKeyFrameForRelay(relay, attempt)
		if attempt < attempts {
			time.Sleep(interval)
		}
	}
}

func (e *PionEngine) requestKeyFrameForRelay(relay *relayTrack, attempt int) {
	e.mu.Lock()
	sourceSession := e.sessions[relay.sourceParticipantID]
	var publishPC *webrtc.PeerConnection
	if sourceSession != nil {
		publishPC = sourceSession.publishPC
	}
	e.mu.Unlock()
	if publishPC == nil {
		return
	}
	err := publishPC.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{
			MediaSSRC: relay.sourceSSRC,
		},
	})
	if err != nil {
		e.logger.Debug(
			"pion relay keyframe request failed",
			"channel_id", relay.channelID,
			"source_participant_id", relay.sourceParticipantID,
			"source_track_id", relay.sourceTrackID,
			"attempt", attempt,
			"error", err,
		)
		return
	}
	e.logger.Debug(
		"pion relay keyframe requested",
		"channel_id", relay.channelID,
		"source_participant_id", relay.sourceParticipantID,
		"source_track_id", relay.sourceTrackID,
		"attempt", attempt,
	)
}

func (e *PionEngine) removeSourceRelayTracksLocked(channelID string, sourceParticipantID string) {
	channelTracks := e.relayTracksByChannel[channelID]
	if len(channelTracks) == 0 {
		return
	}
	keysToRemove := make([]string, 0, len(channelTracks))
	prefix := sourceParticipantID + ":"
	for relayKey := range channelTracks {
		if strings.HasPrefix(relayKey, prefix) {
			keysToRemove = append(keysToRemove, relayKey)
		}
	}
	for _, relayKey := range keysToRemove {
		e.removeRelayTrackLocked(channelID, relayKey)
	}
}

func (e *PionEngine) removeRelayTrackLocked(channelID string, relayKey string) {
	channelTracks := e.relayTracksByChannel[channelID]
	if len(channelTracks) == 0 {
		return
	}
	relay, ok := channelTracks[relayKey]
	if !ok {
		return
	}
	delete(channelTracks, relayKey)
	if len(channelTracks) == 0 {
		delete(e.relayTracksByChannel, channelID)
	}

	affectedSubscribers := make([]Participant, 0, len(e.sessions))
	for _, session := range e.sessions {
		if session.participant.ChannelID != channelID || session.subscribePC == nil || session.relaySenders == nil {
			continue
		}
		sender, exists := session.relaySenders[relayKey]
		if !exists || sender == nil {
			continue
		}
		if err := session.subscribePC.RemoveTrack(sender); err != nil {
			e.logger.Warn(
				"pion failed to remove relay sender",
				"subscriber_participant_id", session.participant.ParticipantID,
				"channel_id", channelID,
				"source_participant_id", relay.sourceParticipantID,
				"source_track_id", relay.sourceTrackID,
				"error", err,
			)
		}
		delete(session.relaySenders, relayKey)
		affectedSubscribers = append(affectedSubscribers, session.participant)
	}

	sourceSession := e.sessions[relay.sourceParticipantID]
	if sourceSession != nil && e.emitTrackUnpublished != nil {
		lifecycle := TrackLifecycle{
			ParticipantID: relay.sourceParticipantID,
			UserUID:       sourceSession.participant.UserUID,
			DeviceID:      sourceSession.participant.DeviceID,
			TrackID:       relay.sourceTrackID,
			StreamID:      relay.sourceStreamID,
			MediaKind:     toTrackMediaKind(relay.kind),
			StreamKind:    defaultStreamKindForMediaKind(toTrackMediaKind(relay.kind)),
		}
		go e.emitTrackUnpublished(sourceSession.participant, lifecycle)
	}

	if e.emitSubscribeRefresh != nil {
		for _, subscriber := range affectedSubscribers {
			subscriber := subscriber
			go e.emitSubscribeRefresh(subscriber, "relay_removed")
		}
	}
}

func (e *PionEngine) logPeerMediaSnapshot(participant Participant, direction SignalDirection, pc *webrtc.PeerConnection, stage string) {
	if pc == nil {
		return
	}
	senders := pc.GetSenders()
	receivers := pc.GetReceivers()
	transceivers := pc.GetTransceivers()

	senderTracks := make([]string, 0, len(senders))
	for _, sender := range senders {
		track := sender.Track()
		if track == nil {
			senderTracks = append(senderTracks, "nil")
			continue
		}
		senderTracks = append(senderTracks, fmt.Sprintf("%s:%s", track.Kind().String(), track.ID()))
	}

	receiverTracks := make([]string, 0, len(receivers))
	for _, receiver := range receivers {
		track := receiver.Track()
		if track == nil {
			receiverTracks = append(receiverTracks, "nil")
			continue
		}
		receiverTracks = append(receiverTracks, fmt.Sprintf("%s:%s", track.Kind().String(), track.ID()))
	}

	e.logger.Info(
		"pion peer media snapshot",
		"participant_id", participant.ParticipantID,
		"channel_id", participant.ChannelID,
		"direction", direction,
		"stage", stage,
		"senders", len(senders),
		"receivers", len(receivers),
		"transceivers", len(transceivers),
		"sender_tracks", strings.Join(senderTracks, ","),
		"receiver_tracks", strings.Join(receiverTracks, ","),
	)
	if direction == SignalDirectionSubscribe && len(senders) == 0 {
		e.logger.Warn(
			"pion subscribe peer has no outbound sender tracks",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"stage", stage,
			"transceivers", len(transceivers),
		)
	}
}

func (e *PionEngine) relayPublishTrackRTP(participant Participant, direction SignalDirection, track *webrtc.TrackRemote) {
	localTrack, err := webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, track.ID(), track.StreamID())
	if err != nil {
		e.logger.Warn(
			"pion failed to create relay track",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
			"track_id", track.ID(),
			"stream_id", track.StreamID(),
			"kind", track.Kind().String(),
			"error", err,
		)
		return
	}
	relay := &relayTrack{
		channelID:           participant.ChannelID,
		sourceParticipantID: participant.ParticipantID,
		sourceTrackID:       track.ID(),
		sourceStreamID:      track.StreamID(),
		sourceSSRC:          uint32(track.SSRC()),
		kind:                track.Kind(),
		localTrack:          localTrack,
	}
	relayKey := relayTrackKey(relay.sourceParticipantID, relay.sourceTrackID)

	e.mu.Lock()
	channelTracks := e.relayTracksByChannel[participant.ChannelID]
	if channelTracks == nil {
		channelTracks = make(map[string]*relayTrack)
		e.relayTracksByChannel[participant.ChannelID] = channelTracks
	}
	if _, exists := channelTracks[relayKey]; exists {
		e.removeRelayTrackLocked(participant.ChannelID, relayKey)
		channelTracks = e.relayTracksByChannel[participant.ChannelID]
		if channelTracks == nil {
			channelTracks = make(map[string]*relayTrack)
			e.relayTracksByChannel[participant.ChannelID] = channelTracks
		}
	}
	channelTracks[relayKey] = relay
	for _, session := range e.sessions {
		if session.participant.ChannelID != participant.ChannelID || session.participant.ParticipantID == participant.ParticipantID {
			continue
		}
		e.ensureSubscriberRelaySenderLocked(session, relayKey, relay)
	}
	e.mu.Unlock()
	if e.emitTrackPublished != nil {
		mediaKind := toTrackMediaKind(track.Kind())
		lifecycle := TrackLifecycle{
			ParticipantID: participant.ParticipantID,
			UserUID:       participant.UserUID,
			DeviceID:      participant.DeviceID,
			TrackID:       track.ID(),
			StreamID:      track.StreamID(),
			MediaKind:     mediaKind,
			StreamKind:    defaultStreamKindForMediaKind(mediaKind),
		}
		go e.emitTrackPublished(participant, lifecycle)
	}

	e.logger.Info(
		"pion publish track received",
		"participant_id", participant.ParticipantID,
		"channel_id", participant.ChannelID,
		"direction", direction,
		"track_id", track.ID(),
		"stream_id", track.StreamID(),
		"kind", track.Kind().String(),
		"payload_type", uint8(track.PayloadType()),
		"ssrc", uint32(track.SSRC()),
	)

	const logInterval = 2 * time.Second
	var packets uint64
	var bytes uint64
	var packetsLast uint64
	var bytesLast uint64
	startedAt := time.Now()
	lastLogAt := startedAt

	for {
		packet, _, readErr := track.ReadRTP()
		if readErr != nil {
			e.logger.Info(
				"pion publish track stream ended",
				"participant_id", participant.ParticipantID,
				"channel_id", participant.ChannelID,
				"direction", direction,
				"track_id", track.ID(),
				"stream_id", track.StreamID(),
				"kind", track.Kind().String(),
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"packets_total", packets,
				"bytes_total", bytes,
				"error", readErr,
			)
			e.mu.Lock()
			e.removeRelayTrackLocked(participant.ChannelID, relayKey)
			e.mu.Unlock()
			return
		}
		if writeErr := relay.localTrack.WriteRTP(packet); writeErr != nil {
			e.logger.Debug(
				"pion relay write failed",
				"participant_id", participant.ParticipantID,
				"channel_id", participant.ChannelID,
				"track_id", track.ID(),
				"stream_id", track.StreamID(),
				"error", writeErr,
			)
		}

		packets += 1
		bytes += uint64(len(packet.Payload))
		now := time.Now()
		if now.Sub(lastLogAt) < logInterval {
			continue
		}
		e.logger.Info(
			"pion publish track rtp stats",
			"participant_id", participant.ParticipantID,
			"channel_id", participant.ChannelID,
			"direction", direction,
			"track_id", track.ID(),
			"stream_id", track.StreamID(),
			"kind", track.Kind().String(),
			"elapsed_ms", now.Sub(startedAt).Milliseconds(),
			"packets_total", packets,
			"bytes_total", bytes,
			"packets_delta", packets-packetsLast,
			"bytes_delta", bytes-bytesLast,
		)
		lastLogAt = now
		packetsLast = packets
		bytesLast = bytes
	}
}

func sessionPeerState(session *pionSession, direction SignalDirection) (*webrtc.PeerConnection, *[]webrtc.ICECandidateInit, error) {
	switch direction {
	case SignalDirectionPublish:
		return session.publishPC, &session.pendingPublishCandidates, nil
	case SignalDirectionSubscribe:
		return session.subscribePC, &session.pendingSubscribeCandidates, nil
	default:
		return nil, nil, fmt.Errorf("unsupported signal direction: %s", direction)
	}
}
