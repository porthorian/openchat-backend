package rtc

import (
	"encoding/json"
	"log/slog"
	"testing"
)

func TestEmitTrackPublishedUsesHintedStreamKind(t *testing.T) {
	service := &SignalingService{
		logger:     slog.Default(),
		rooms:      newRoomHub(),
		mediaHints: newMediaHintRegistry(),
	}
	receiver := &wsClient{
		participant: Participant{
			ParticipantID: "recv",
			ChannelID:     "ch",
		},
		send: make(chan Envelope, 2),
	}
	service.rooms.rooms["ch"] = map[string]*wsClient{
		"recv": receiver,
	}
	service.mediaHints.apply("ch", "src", "video-track", "video-stream", TrackStreamKindVideoScreen, "start")

	service.emitTrackPublished(
		Participant{
			ParticipantID: "src",
			ChannelID:     "ch",
			UserUID:       "uid_src",
			DeviceID:      "dev_src",
		},
		TrackLifecycle{
			ParticipantID: "src",
			UserUID:       "uid_src",
			DeviceID:      "dev_src",
			TrackID:       "video-track",
			StreamID:      "video-stream",
			MediaKind:     TrackMediaKindVideo,
			StreamKind:    TrackStreamKindVideoCamera,
		},
	)

	select {
	case envelope := <-receiver.send:
		if envelope.Type != "rtc.track.published" {
			t.Fatalf("unexpected envelope type: %s", envelope.Type)
		}
		var payload TrackLifecycle
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.StreamKind != TrackStreamKindVideoScreen {
			t.Fatalf("expected hinted stream kind %s, got %s", TrackStreamKindVideoScreen, payload.StreamKind)
		}
		if payload.MediaKind != TrackMediaKindVideo {
			t.Fatalf("expected media kind video, got %s", payload.MediaKind)
		}
	default:
		t.Fatalf("expected track published envelope")
	}
}

func TestEmitTrackUnpublishedClearsHint(t *testing.T) {
	service := &SignalingService{
		logger:     slog.Default(),
		rooms:      newRoomHub(),
		mediaHints: newMediaHintRegistry(),
	}
	receiver := &wsClient{
		participant: Participant{
			ParticipantID: "recv",
			ChannelID:     "ch",
		},
		send: make(chan Envelope, 2),
	}
	service.rooms.rooms["ch"] = map[string]*wsClient{
		"recv": receiver,
	}
	service.mediaHints.apply("ch", "src", "audio-track", "audio-stream", TrackStreamKindAudioMicrophone, "start")

	service.emitTrackUnpublished(
		Participant{
			ParticipantID: "src",
			ChannelID:     "ch",
			UserUID:       "uid_src",
			DeviceID:      "dev_src",
		},
		TrackLifecycle{
			ParticipantID: "src",
			UserUID:       "uid_src",
			DeviceID:      "dev_src",
			TrackID:       "audio-track",
			StreamID:      "audio-stream",
			MediaKind:     TrackMediaKindAudio,
			StreamKind:    TrackStreamKindAudioMicrophone,
		},
	)

	select {
	case envelope := <-receiver.send:
		if envelope.Type != "rtc.track.unpublished" {
			t.Fatalf("unexpected envelope type: %s", envelope.Type)
		}
	default:
		t.Fatalf("expected track unpublished envelope")
	}

	if got := service.mediaHints.resolveStreamKind("ch", "src", "audio-track", "audio-stream", TrackMediaKindAudio); got != TrackStreamKindAudioMicrophone {
		t.Fatalf("unexpected fallback stream kind after clear: %s", got)
	}
}
