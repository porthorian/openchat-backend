package app

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr      string
	PublicBaseURL string
	SignalingPath string
	TicketTTL     time.Duration
	TicketSecret  string
	Environment   string
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
	return Config{
		HTTPAddr:      envOrDefault("OPENCHAT_HTTP_ADDR", ":8080"),
		PublicBaseURL: envOrDefault("OPENCHAT_PUBLIC_BASE_URL", "http://localhost:8080"),
		SignalingPath: envOrDefault("OPENCHAT_SIGNALING_PATH", "/v1/rtc/signaling"),
		TicketTTL:     time.Duration(envOrDefaultInt("OPENCHAT_JOIN_TICKET_TTL_SECONDS", 60)) * time.Second,
		TicketSecret:  envOrDefault("OPENCHAT_JOIN_TICKET_SECRET", "dev-insecure-secret-change-me"),
		Environment:   envOrDefault("OPENCHAT_ENV", "development"),
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
