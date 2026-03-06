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
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

func TestSignalingService_TrackPublishedLifecycleSmoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := NewTokenService("smoke-test-secret", 2*time.Minute)
	service := NewSignalingService(logger, tokens, SignalingConfig{})

	server := httptest.NewServer(http.HandlerFunc(service.ServeWS))
	defer server.Close()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)

	connA := dialWebSocket(t, wsURL)
	defer connA.Close()
	connB := dialWebSocket(t, wsURL)
	defer connB.Close()

	joinWithTicket(t, connA, tokens, IssueTicketInput{
		ServerID:  "srv_smoke",
		ChannelID: "vc_smoke",
		UserUID:   "uid_a",
		DeviceID:  "dev_a",
		Permissions: Permissions{
			Speak:       true,
			Video:       true,
			Screenshare: true,
		},
	})
	joinWithTicket(t, connB, tokens, IssueTicketInput{
		ServerID:  "srv_smoke",
		ChannelID: "vc_smoke",
		UserUID:   "uid_b",
		DeviceID:  "dev_b",
		Permissions: Permissions{
			Speak:       true,
			Video:       true,
			Screenshare: true,
		},
	})
	_ = connA.SetReadDeadline(time.Time{})
	_ = connB.SetReadDeadline(time.Time{})

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("create peer connection: %v", err)
	}
	defer pc.Close()

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"track_video_smoke",
		"stream_video_smoke",
	)
	if err != nil {
		t.Fatalf("create local video track: %v", err)
	}
	if _, err := pc.AddTrack(videoTrack); err != nil {
		t.Fatalf("add local video track: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}
	select {
	case <-gatherComplete:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for local ICE gathering")
	}
	localOffer := pc.LocalDescription()
	if localOffer == nil || strings.TrimSpace(localOffer.SDP) == "" {
		t.Fatalf("expected gathered local offer sdp")
	}

	sendEnvelope(t, connA, "rtc.media.state", map[string]any{
		"stream_kind": "video_screen",
		"action":      "start",
		"track_id":    "track_video_smoke",
		"stream_id":   "stream_video_smoke",
	})
	sendEnvelope(t, connA, "rtc.offer.publish", map[string]any{
		"type":      "offer",
		"sdp":       localOffer.SDP,
		"direction": "publish",
	})

	_ = waitForTrackPublishedOnConnection(t, connA, pc, videoTrack, 12*time.Second)

	trackPublished := waitForEnvelopeType(t, connB, "rtc.track.published", 8*time.Second)
	var lifecycle TrackLifecycle
	if err := json.Unmarshal(trackPublished.Payload, &lifecycle); err != nil {
		t.Fatalf("decode track lifecycle payload: %v", err)
	}
	if lifecycle.TrackID != "track_video_smoke" {
		t.Fatalf("unexpected track id: %s", lifecycle.TrackID)
	}
	if lifecycle.StreamID != "stream_video_smoke" {
		t.Fatalf("unexpected stream id: %s", lifecycle.StreamID)
	}
	if lifecycle.MediaKind != TrackMediaKindVideo {
		t.Fatalf("unexpected media kind: %s", lifecycle.MediaKind)
	}
	if lifecycle.StreamKind != TrackStreamKindVideoScreen {
		t.Fatalf("expected screen stream kind, got: %s", lifecycle.StreamKind)
	}
}

func waitForTrackPublishedOnConnection(
	t *testing.T,
	conn *websocket.Conn,
	pc *webrtc.PeerConnection,
	track *webrtc.TrackLocalStaticSample,
	timeout time.Duration,
) Envelope {
	t.Helper()
	answerApplied := false
	pendingCandidates := make([]webrtc.ICECandidateInit, 0, 4)
	sampleCounter := byte(0)
	envelopeCh := make(chan Envelope, 32)
	readErrCh := make(chan error, 1)
	go func() {
		for {
			var envelope Envelope
			if err := conn.ReadJSON(&envelope); err != nil {
				readErrCh <- err
				return
			}
			envelopeCh <- envelope
		}
	}()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	sampleTicker := time.NewTicker(time.Second / 30)
	defer sampleTicker.Stop()

	for {
		select {
		case <-timeoutTimer.C:
			t.Fatalf("timed out waiting for rtc.track.published")
		case err := <-readErrCh:
			t.Fatalf("read signaling envelope: %v", err)
		case <-sampleTicker.C:
			if !answerApplied {
				continue
			}
			if err := track.WriteSample(media.Sample{
				Data:     []byte{0x00, 0x00, 0x01, sampleCounter},
				Duration: time.Second / 30,
			}); err != nil {
				t.Fatalf("write sample while waiting for lifecycle event: %v", err)
			}
			sampleCounter++
		case envelope := <-envelopeCh:
			switch envelope.Type {
			case "rtc.answer.publish":
				if answerApplied {
					continue
				}
				var answerPayload SessionDescriptionPayload
				if err := json.Unmarshal(envelope.Payload, &answerPayload); err != nil {
					t.Fatalf("decode answer payload: %v", err)
				}
				if strings.TrimSpace(answerPayload.SDP) == "" {
					t.Fatalf("expected non-empty answer sdp")
				}
				if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerPayload.SDP}); err != nil {
					t.Fatalf("set remote answer: %v", err)
				}
				for _, candidate := range pendingCandidates {
					if err := pc.AddICECandidate(candidate); err != nil {
						t.Fatalf("apply queued remote candidate: %v", err)
					}
				}
				pendingCandidates = pendingCandidates[:0]
				answerApplied = true
			case "rtc.ice.candidate":
				var candidatePayload struct {
					Candidate     string  `json:"candidate"`
					SDPMid        *string `json:"sdp_mid"`
					SDPMLineIndex *uint16 `json:"sdp_mline_index"`
					Direction     string  `json:"direction"`
				}
				if err := json.Unmarshal(envelope.Payload, &candidatePayload); err != nil {
					t.Fatalf("decode candidate payload: %v", err)
				}
				candidate := strings.TrimSpace(candidatePayload.Candidate)
				if candidate == "" {
					continue
				}
				direction := strings.TrimSpace(candidatePayload.Direction)
				if direction != "" && direction != string(SignalDirectionPublish) {
					continue
				}
				init := webrtc.ICECandidateInit{
					Candidate:     candidate,
					SDPMid:        candidatePayload.SDPMid,
					SDPMLineIndex: candidatePayload.SDPMLineIndex,
				}
				if !answerApplied {
					pendingCandidates = append(pendingCandidates, init)
					continue
				}
				if err := pc.AddICECandidate(init); err != nil {
					t.Fatalf("apply remote candidate: %v", err)
				}
			case "rtc.track.published":
				return envelope
			case "rtc.error":
				t.Fatalf("received rtc.error while waiting for lifecycle events: %s", strings.TrimSpace(string(envelope.Payload)))
			}
		}
	}
}

func dialWebSocket(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func joinWithTicket(t *testing.T, conn *websocket.Conn, tokens *TokenService, input IssueTicketInput) {
	t.Helper()
	ticket, _, err := tokens.Issue(input)
	if err != nil {
		t.Fatalf("issue ticket: %v", err)
	}
	sendEnvelope(t, conn, "rtc.join", map[string]any{
		"ticket": ticket,
	})
	_ = waitForEnvelopeType(t, conn, "rtc.joined", 5*time.Second)
}

func sendEnvelope(t *testing.T, conn *websocket.Conn, eventType string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := conn.WriteJSON(Envelope{
		Type:    eventType,
		Payload: raw,
	}); err != nil {
		t.Fatalf("write envelope %s: %v", eventType, err)
	}
}

func waitForEnvelopeType(t *testing.T, conn *websocket.Conn, envelopeType string, timeout time.Duration) Envelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		var envelope Envelope
		if err := conn.ReadJSON(&envelope); err != nil {
			t.Fatalf("read envelope %s: %v", envelopeType, err)
		}
		if envelope.Type == envelopeType {
			return envelope
		}
	}
}
