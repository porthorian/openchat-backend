package app

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                            string
	PublicBaseURL                       string
	SignalingPath                       string
	TicketTTL                           time.Duration
	TicketSecret                        string
	Environment                         string
	AllowServerCreation                 bool
	RTCSTUNURLs                         []string
	RTCTURNURLs                         []string
	RTCTURNUsername                     string
	RTCTURNCredential                   string
	RTCTURNCredentialType               string
	RTCSubscribeMaxVideoTracks          int
	RTCSubscribeMaxAudioTracks          int
	RTCSubscribeMaxVideoTracksByServer  map[string]int
	RTCSubscribeMaxAudioTracksByServer  map[string]int
	RTCSubscribeMaxVideoTracksByChannel map[string]int
	RTCSubscribeMaxAudioTracksByChannel map[string]int
}

type RTCSubscribeReceiveLimits struct {
	MaxVideoTracks int
	MaxAudioTracks int
}

func (c Config) IsProduction() bool {
	return strings.EqualFold(c.Environment, "production")
}

func (c Config) SignalingURL() string {
	base, err := url.Parse(c.PublicBaseURL)
	if err != nil {
		return "ws://localhost:8080" + c.SignalingPath
	}
	if base.Scheme == "https" {
		base.Scheme = "wss"
	} else {
		base.Scheme = "ws"
	}
	base.Path = c.SignalingPath
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}

func LoadConfigFromEnv() Config {
	publicBaseURL := envOrDefault("OPENCHAT_PUBLIC_BASE_URL", "http://localhost:8080")
	return Config{
		HTTPAddr:            envOrDefault("OPENCHAT_HTTP_ADDR", ":8080"),
		PublicBaseURL:       publicBaseURL,
		SignalingPath:       envOrDefault("OPENCHAT_SIGNALING_PATH", "/v1/rtc/signaling"),
		TicketTTL:           time.Duration(envOrDefaultInt("OPENCHAT_JOIN_TICKET_TTL_SECONDS", 60)) * time.Second,
		TicketSecret:        envOrDefault("OPENCHAT_JOIN_TICKET_SECRET", "dev-insecure-secret-change-me"),
		Environment:         envOrDefault("OPENCHAT_ENV", "development"),
		AllowServerCreation: envOrDefaultBool("OPENCHAT_ALLOW_SERVER_CREATION", true),
		RTCSTUNURLs: envCSVOrDefault("OPENCHAT_RTC_STUN_URLS", []string{
			"stun:stun.l.google.com:19302",
			"stun:stun1.l.google.com:19302",
			"stun:stun2.l.google.com:19302",
			"stun:stun3.l.google.com:19302",
			"stun:stun4.l.google.com:19302",
		}),
		RTCTURNURLs:                         envCSVOrDefault("OPENCHAT_RTC_TURN_URLS", nil),
		RTCTURNUsername:                     envOrDefault("OPENCHAT_RTC_TURN_USERNAME", ""),
		RTCTURNCredential:                   envOrDefault("OPENCHAT_RTC_TURN_CREDENTIAL", ""),
		RTCTURNCredentialType:               envOrDefault("OPENCHAT_RTC_TURN_CREDENTIAL_TYPE", "static"),
		RTCSubscribeMaxVideoTracks:          envOrDefaultInt("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS", 8),
		RTCSubscribeMaxAudioTracks:          envOrDefaultInt("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS", 16),
		RTCSubscribeMaxVideoTracksByServer:  envIntMapOrDefault("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_SERVER", nil),
		RTCSubscribeMaxAudioTracksByServer:  envIntMapOrDefault("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_SERVER", nil),
		RTCSubscribeMaxVideoTracksByChannel: envIntMapOrDefault("OPENCHAT_RTC_SUBSCRIBE_MAX_VIDEO_TRACKS_BY_CHANNEL", nil),
		RTCSubscribeMaxAudioTracksByChannel: envIntMapOrDefault("OPENCHAT_RTC_SUBSCRIBE_MAX_AUDIO_TRACKS_BY_CHANNEL", nil),
	}
}

func (c Config) ResolveRTCSubscribeReceiveLimits(serverID string, channelID string) RTCSubscribeReceiveLimits {
	maxVideoTracks := c.RTCSubscribeMaxVideoTracks
	maxAudioTracks := c.RTCSubscribeMaxAudioTracks
	if maxVideoTracks <= 0 {
		maxVideoTracks = 8
	}
	if maxAudioTracks <= 0 {
		maxAudioTracks = 16
	}

	serverID = strings.TrimSpace(serverID)
	channelID = strings.TrimSpace(channelID)
	if serverID != "" {
		if override := c.RTCSubscribeMaxVideoTracksByServer[serverID]; override > 0 {
			maxVideoTracks = override
		}
		if override := c.RTCSubscribeMaxAudioTracksByServer[serverID]; override > 0 {
			maxAudioTracks = override
		}
	}
	if channelID != "" {
		if override := c.RTCSubscribeMaxVideoTracksByChannel[channelID]; override > 0 {
			maxVideoTracks = override
		}
		if override := c.RTCSubscribeMaxAudioTracksByChannel[channelID]; override > 0 {
			maxAudioTracks = override
		}
	}
	if maxVideoTracks <= 0 {
		maxVideoTracks = 1
	}
	if maxAudioTracks <= 0 {
		maxAudioTracks = 1
	}
	return RTCSubscribeReceiveLimits{
		MaxVideoTracks: maxVideoTracks,
		MaxAudioTracks: maxAudioTracks,
	}
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envOrDefaultInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envOrDefaultBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envCSVOrDefault(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if len(fallback) == 0 {
			return nil
		}
		return append([]string(nil), fallback...)
	}
	if strings.EqualFold(value, "none") || value == "-" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envIntMapOrDefault(key string, fallback map[string]int) map[string]int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if len(fallback) == 0 {
			return nil
		}
		out := make(map[string]int, len(fallback))
		for id, count := range fallback {
			if strings.TrimSpace(id) == "" || count <= 0 {
				continue
			}
			out[strings.TrimSpace(id)] = count
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	if strings.EqualFold(value, "none") || value == "-" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make(map[string]int, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			continue
		}
		keyValue := strings.SplitN(segment, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		id := strings.TrimSpace(keyValue[0])
		if id == "" {
			continue
		}
		count, err := strconv.Atoi(strings.TrimSpace(keyValue[1]))
		if err != nil || count <= 0 {
			continue
		}
		out[id] = count
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
