package app

import (
	"reflect"
	"testing"
)

func TestLoadConfigFromEnv_DefaultSTUNURLs(t *testing.T) {
	t.Setenv("OPENCHAT_RTC_STUN_URLS", "")

	cfg := LoadConfigFromEnv()
	expected := []string{
		"stun:stun.l.google.com:19302",
		"stun:stun1.l.google.com:19302",
		"stun:stun2.l.google.com:19302",
		"stun:stun3.l.google.com:19302",
		"stun:stun4.l.google.com:19302",
	}
	if !reflect.DeepEqual(cfg.RTCSTUNURLs, expected) {
		t.Fatalf("unexpected default stun urls: %#v", cfg.RTCSTUNURLs)
	}
}

func TestLoadConfigFromEnv_DefaultAllowServerCreation(t *testing.T) {
	t.Setenv("OPENCHAT_ALLOW_SERVER_CREATION", "")
	cfg := LoadConfigFromEnv()
	if !cfg.AllowServerCreation {
		t.Fatalf("expected allow server creation default true")
	}
}

func TestLoadConfigFromEnv_AllowServerCreationOverride(t *testing.T) {
	t.Setenv("OPENCHAT_ALLOW_SERVER_CREATION", "false")
	cfg := LoadConfigFromEnv()
	if cfg.AllowServerCreation {
		t.Fatalf("expected allow server creation false override")
	}
}

func TestLoadConfigFromEnv_OverrideSTUNURLs(t *testing.T) {
	t.Setenv("OPENCHAT_RTC_STUN_URLS", "stun:one.example.net:3478,stun:two.example.net:3478")

	cfg := LoadConfigFromEnv()
	expected := []string{
		"stun:one.example.net:3478",
		"stun:two.example.net:3478",
	}
	if !reflect.DeepEqual(cfg.RTCSTUNURLs, expected) {
		t.Fatalf("unexpected overridden stun urls: %#v", cfg.RTCSTUNURLs)
	}
}

func TestLoadConfigFromEnv_DisableSTUNURLs(t *testing.T) {
	t.Setenv("OPENCHAT_RTC_STUN_URLS", "none")

	cfg := LoadConfigFromEnv()
	if len(cfg.RTCSTUNURLs) != 0 {
		t.Fatalf("expected no stun urls when disabled, got: %#v", cfg.RTCSTUNURLs)
	}
}

func TestLoadConfigFromEnv_DefaultSubscribeReceiveLimits(t *testing.T) {
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS", "")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS", "")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_SERVER", "")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_SERVER", "")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_CHANNEL", "")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_CHANNEL", "")

	cfg := LoadConfigFromEnv()
	if cfg.RTCSubscribeMaxVideoTracks != 8 {
		t.Fatalf("expected default max video tracks 8, got %d", cfg.RTCSubscribeMaxVideoTracks)
	}
	if cfg.RTCSubscribeMaxAudioTracks != 16 {
		t.Fatalf("expected default max audio tracks 16, got %d", cfg.RTCSubscribeMaxAudioTracks)
	}
	if len(cfg.RTCSubscribeMaxVideoTracksByServer) != 0 {
		t.Fatalf("expected empty server video overrides, got %#v", cfg.RTCSubscribeMaxVideoTracksByServer)
	}
	if len(cfg.RTCSubscribeMaxAudioTracksByServer) != 0 {
		t.Fatalf("expected empty server audio overrides, got %#v", cfg.RTCSubscribeMaxAudioTracksByServer)
	}
	if len(cfg.RTCSubscribeMaxVideoTracksByChannel) != 0 {
		t.Fatalf("expected empty channel video overrides, got %#v", cfg.RTCSubscribeMaxVideoTracksByChannel)
	}
	if len(cfg.RTCSubscribeMaxAudioTracksByChannel) != 0 {
		t.Fatalf("expected empty channel audio overrides, got %#v", cfg.RTCSubscribeMaxAudioTracksByChannel)
	}
}

func TestLoadConfigFromEnv_SubscribeReceiveOverrideMaps(t *testing.T) {
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_SERVER", "0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8=10,6b6d8f24-a39f-4f8a-9f9d-0f70f2a2e9f1=6,srv_bad=0,srv_bad2=-1,srv_bad3=nope")
	t.Setenv("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_CHANNEL", "vc_general=20,tl_vc_huddle=12,invalid,noequal")

	cfg := LoadConfigFromEnv()
	expectedVideoByServer := map[string]int{
		"0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8": 10,
		"6b6d8f24-a39f-4f8a-9f9d-0f70f2a2e9f1": 6,
	}
	if !reflect.DeepEqual(cfg.RTCSubscribeMaxVideoTracksByServer, expectedVideoByServer) {
		t.Fatalf("unexpected video overrides by server: %#v", cfg.RTCSubscribeMaxVideoTracksByServer)
	}
	expectedAudioByChannel := map[string]int{
		"vc_general":   20,
		"tl_vc_huddle": 12,
	}
	if !reflect.DeepEqual(cfg.RTCSubscribeMaxAudioTracksByChannel, expectedAudioByChannel) {
		t.Fatalf("unexpected audio overrides by channel: %#v", cfg.RTCSubscribeMaxAudioTracksByChannel)
	}
}

func TestConfigResolveRTCSubscribeReceiveLimitsPrecedence(t *testing.T) {
	cfg := Config{
		RTCSubscribeMaxVideoTracks: 8,
		RTCSubscribeMaxAudioTracks: 16,
		RTCSubscribeMaxVideoTracksByServer: map[string]int{
			"0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8": 6,
		},
		RTCSubscribeMaxAudioTracksByServer: map[string]int{
			"0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8": 12,
		},
		RTCSubscribeMaxVideoTracksByChannel: map[string]int{
			"vc_general": 3,
		},
		RTCSubscribeMaxAudioTracksByChannel: map[string]int{
			"vc_general": 9,
		},
	}

	channelOverride := cfg.ResolveRTCSubscribeReceiveLimits("0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8", "vc_general")
	if channelOverride.MaxVideoTracks != 3 || channelOverride.MaxAudioTracks != 9 {
		t.Fatalf("expected channel overrides (3/9), got %+v", channelOverride)
	}

	serverOverride := cfg.ResolveRTCSubscribeReceiveLimits("0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8", "vc_party")
	if serverOverride.MaxVideoTracks != 6 || serverOverride.MaxAudioTracks != 12 {
		t.Fatalf("expected server overrides (6/12), got %+v", serverOverride)
	}

	instanceDefault := cfg.ResolveRTCSubscribeReceiveLimits("srv_unknown", "vc_unknown")
	if instanceDefault.MaxVideoTracks != 8 || instanceDefault.MaxAudioTracks != 16 {
		t.Fatalf("expected instance defaults (8/16), got %+v", instanceDefault)
	}
}

func TestConfigResolveRTCSubscribeReceiveLimitsUsesDefaultsWhenUnset(t *testing.T) {
	cfg := Config{
		RTCSubscribeMaxVideoTracks: 0,
		RTCSubscribeMaxAudioTracks: -4,
	}
	limits := cfg.ResolveRTCSubscribeReceiveLimits("0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8", "vc_general")
	if limits.MaxVideoTracks != 8 || limits.MaxAudioTracks != 16 {
		t.Fatalf("expected default limits (8/16), got %+v", limits)
	}
}
