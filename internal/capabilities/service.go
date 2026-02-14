package capabilities

import (
	"time"

	"github.com/openchat/openchat-backend/internal/app"
)

type Service struct {
	cfg app.Config
}

func NewService(cfg app.Config) *Service {
	return &Service{cfg: cfg}
}

type CapabilitiesResponse struct {
	ServerName             string                        `json:"server_name"`
	ServerID               string                        `json:"server_id"`
	APIVersion             string                        `json:"api_version"`
	IdentityHandshakeModes []string                      `json:"identity_handshake_modes"`
	UserUIDPolicy          string                        `json:"user_uid_policy"`
	ProfileDataPolicy      string                        `json:"profile_data_policy"`
	Transport              TransportCapabilitiesResponse `json:"transport"`
	Features               CoreFeatureFlagsResponse      `json:"features"`
	Limits                 CapabilityLimitsResponse      `json:"limits"`
	Security               SecurityCapabilitiesResponse  `json:"security"`
	RTC                    *RTCCapabilitiesResponse      `json:"rtc,omitempty"`
	Moderation             *ModerationCapabilities       `json:"moderation,omitempty"`
	Profile                *ProfileCapabilitiesResponse  `json:"profile,omitempty"`
}

type TransportCapabilitiesResponse struct {
	WebSocket bool `json:"websocket"`
	SSE       bool `json:"sse"`
	Polling   bool `json:"polling"`
}

type CoreFeatureFlagsResponse struct {
	Messaging     bool `json:"messaging"`
	Presence      bool `json:"presence"`
	Attachments   bool `json:"attachments"`
	Notifications bool `json:"notifications"`
}

type CapabilityLimitsResponse struct {
	MaxMessageBytes     int `json:"max_message_bytes"`
	MaxUploadBytes      int `json:"max_upload_bytes"`
	RateLimitPerMinute  int `json:"rate_limit_per_minute"`
	MaxCallParticipants int `json:"max_call_participants"`
}

type SecurityCapabilitiesResponse struct {
	HTTPSRequired      bool   `json:"https_required"`
	CertificatePinning string `json:"certificate_pinning"`
}

type RTCFeatureFlagsResponse struct {
	Voice       bool `json:"voice"`
	Video       bool `json:"video"`
	Screenshare bool `json:"screenshare"`
	Simulcast   bool `json:"simulcast"`
}

type RTCIceServerResponse struct {
	URLs           []string `json:"urls"`
	Username       string   `json:"username,omitempty"`
	Credential     string   `json:"credential,omitempty"`
	CredentialType string   `json:"credential_type,omitempty"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
}

type RTCConnectionPolicyResponse struct {
	JoinTimeoutMs      int   `json:"join_timeout_ms"`
	AnswerTimeoutMs    int   `json:"answer_timeout_ms"`
	ICERestartEnabled  bool  `json:"ice_restart_enabled"`
	ReconnectBackoffMs []int `json:"reconnect_backoff_ms"`
}

type RTCCapabilitiesResponse struct {
	ProtocolVersion    string                      `json:"protocol_version"`
	SignalingURL       string                      `json:"signaling_url"`
	SignalingTransport string                      `json:"signaling_transport"`
	Topologies         []string                    `json:"topologies"`
	Features           RTCFeatureFlagsResponse     `json:"features"`
	IceServers         []RTCIceServerResponse      `json:"ice_servers"`
	ConnectionPolicy   RTCConnectionPolicyResponse `json:"connection_policy"`
}

type ModerationCapabilities struct {
	Enabled        bool                     `json:"enabled"`
	Actions        ModerationActionSets     `json:"actions"`
	VotePolicy     ModerationVotePolicy     `json:"vote_policy"`
	EvidencePolicy ModerationEvidencePolicy `json:"evidence_policy"`
}

type ModerationActionSets struct {
	Immediate    []string `json:"immediate"`
	VoteRequired []string `json:"vote_required"`
}

type ModerationVotePolicy struct {
	Threshold     int `json:"threshold"`
	Quorum        int `json:"quorum"`
	WindowSeconds int `json:"window_seconds"`
}

type ModerationEvidencePolicy struct {
	ReportBundleRequired        bool `json:"report_bundle_required"`
	PlaintextDisclosureOptional bool `json:"plaintext_disclosure_optional"`
}

type ProfileCapabilitiesResponse struct {
	Enabled                  bool                              `json:"enabled"`
	Scope                    string                            `json:"scope"`
	Fields                   []string                          `json:"fields"`
	AvatarModes              []string                          `json:"avatar_modes"`
	DisplayName              ProfileDisplayNameRulesResponse   `json:"display_name"`
	AvatarUpload             *ProfileAvatarUploadRulesResponse `json:"avatar_upload,omitempty"`
	RealtimeEvent            string                            `json:"realtime_event"`
	MessageAuthorProfileMode string                            `json:"message_author_profile_mode"`
}

type ProfileDisplayNameRulesResponse struct {
	MinLength int    `json:"min_length"`
	MaxLength int    `json:"max_length"`
	Pattern   string `json:"pattern,omitempty"`
}

type ProfileAvatarUploadRulesResponse struct {
	MaxBytes  int      `json:"max_bytes"`
	MimeTypes []string `json:"mime_types"`
	MaxWidth  int      `json:"max_width"`
	MaxHeight int      `json:"max_height"`
}

func (s *Service) Build() CapabilitiesResponse {
	turnExpiry := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	return CapabilitiesResponse{
		ServerName:             "OpenChat Harbor",
		ServerID:               "srv_harbor",
		APIVersion:             "2026-02-14",
		IdentityHandshakeModes: []string{"challenge_signature", "token_proof"},
		UserUIDPolicy:          "server_scoped",
		ProfileDataPolicy:      "uid_only",
		Transport: TransportCapabilitiesResponse{
			WebSocket: true,
			SSE:       false,
			Polling:   false,
		},
		Features: CoreFeatureFlagsResponse{
			Messaging:     true,
			Presence:      true,
			Attachments:   true,
			Notifications: true,
		},
		Limits: CapabilityLimitsResponse{
			MaxMessageBytes:     65536,
			MaxUploadBytes:      10485760,
			RateLimitPerMinute:  180,
			MaxCallParticipants: 200,
		},
		Security: SecurityCapabilitiesResponse{
			HTTPSRequired:      s.cfg.IsProduction(),
			CertificatePinning: "optional",
		},
		RTC: &RTCCapabilitiesResponse{
			ProtocolVersion:    "1.0",
			SignalingURL:       s.cfg.SignalingURL(),
			SignalingTransport: "websocket",
			Topologies:         []string{"sfu"},
			Features: RTCFeatureFlagsResponse{
				Voice:       true,
				Video:       true,
				Screenshare: true,
				Simulcast:   true,
			},
			IceServers: []RTCIceServerResponse{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
				{
					URLs:           []string{"turns:turn.example.invalid:5349"},
					Username:       "dev-user",
					Credential:     "dev-secret",
					CredentialType: "ephemeral",
					ExpiresAt:      turnExpiry,
				},
			},
			ConnectionPolicy: RTCConnectionPolicyResponse{
				JoinTimeoutMs:      12000,
				AnswerTimeoutMs:    10000,
				ICERestartEnabled:  true,
				ReconnectBackoffMs: []int{250, 500, 1000, 2000, 5000},
			},
		},
		Moderation: &ModerationCapabilities{
			Enabled: true,
			Actions: ModerationActionSets{
				Immediate:    []string{"kick", "timeout_short", "channel_lock"},
				VoteRequired: []string{"ban", "timeout_long", "role_remove"},
			},
			VotePolicy: ModerationVotePolicy{
				Threshold:     2,
				Quorum:        3,
				WindowSeconds: 86400,
			},
			EvidencePolicy: ModerationEvidencePolicy{
				ReportBundleRequired:        true,
				PlaintextDisclosureOptional: true,
			},
		},
		Profile: &ProfileCapabilitiesResponse{
			Enabled:     true,
			Scope:       "global",
			Fields:      []string{"display_name", "avatar"},
			AvatarModes: []string{"generated", "uploaded"},
			DisplayName: ProfileDisplayNameRulesResponse{
				MinLength: 2,
				MaxLength: 32,
			},
			AvatarUpload: &ProfileAvatarUploadRulesResponse{
				MaxBytes:  2 * 1024 * 1024,
				MimeTypes: []string{"image/png", "image/jpeg"},
				MaxWidth:  1024,
				MaxHeight: 1024,
			},
			RealtimeEvent:            "profile_updated",
			MessageAuthorProfileMode: "snapshot",
		},
	}
}
