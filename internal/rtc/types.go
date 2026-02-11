package rtc

import (
	"encoding/json"
	"time"
)

type Permissions struct {
	Speak       bool `json:"speak"`
	Video       bool `json:"video"`
	Screenshare bool `json:"screenshare"`
}

type TicketClaims struct {
	ServerID    string      `json:"server_id"`
	ChannelID   string      `json:"channel_id"`
	UserUID     string      `json:"user_uid"`
	DeviceID    string      `json:"device_id"`
	Permissions Permissions `json:"permissions"`
	ExpiresAt   int64       `json:"exp"`
	IssuedAt    int64       `json:"iat"`
	JTI         string      `json:"jti"`
}

type Participant struct {
	ParticipantID string      `json:"participant_id"`
	ChannelID     string      `json:"channel_id"`
	UserUID       string      `json:"user_uid"`
	DeviceID      string      `json:"device_id"`
	Permissions   Permissions `json:"permissions"`
	JoinedAt      time.Time   `json:"joined_at"`
}

type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	ChannelID string          `json:"channel_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func NewEnvelope(eventType string, channelID string, requestID string, payload any) Envelope {
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
		ChannelID: channelID,
		Payload:   rawPayload,
	}
}
