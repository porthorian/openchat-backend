package capabilities

import (
	"testing"

	"github.com/openchat/openchat-backend/internal/app"
)

func TestBuildRTCCapabilitiesV2SFU(t *testing.T) {
	svc := NewService(app.Config{})
	caps := svc.Build()
	if caps.RTC == nil {
		t.Fatalf("expected rtc capabilities")
	}
	if caps.RTC.ProtocolVersion != "2.0" {
		t.Fatalf("expected rtc protocol version 2.0, got %s", caps.RTC.ProtocolVersion)
	}
	if len(caps.RTC.Topologies) != 1 || caps.RTC.Topologies[0] != "sfu" {
		t.Fatalf("expected rtc topologies [sfu], got %#v", caps.RTC.Topologies)
	}
	if caps.RTC.SubscribeReceive.MaxVideoTracks != 8 || caps.RTC.SubscribeReceive.MaxAudioTracks != 16 {
		t.Fatalf("expected subscribe receive defaults (8/16), got %+v", caps.RTC.SubscribeReceive)
	}
}

func TestBuildRTCCapabilitiesSubscribeReceiveFromConfig(t *testing.T) {
	svc := NewService(app.Config{
		RTCSubscribeMaxVideoTracks: 12,
		RTCSubscribeMaxAudioTracks: 24,
	})
	caps := svc.Build()
	if caps.RTC == nil {
		t.Fatalf("expected rtc capabilities")
	}
	if caps.RTC.SubscribeReceive.MaxVideoTracks != 12 || caps.RTC.SubscribeReceive.MaxAudioTracks != 24 {
		t.Fatalf("expected subscribe receive limits (12/24), got %+v", caps.RTC.SubscribeReceive)
	}
}
