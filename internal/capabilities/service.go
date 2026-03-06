package capabilities

import "github.com/openchat/openchat-backend/internal/app"

type Service struct {
	cfg app.Config
}

func NewService(cfg app.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) rtcIceServers() []RTCIceServerResponse {
	servers := make([]RTCIceServerResponse, 0, 2)
	if len(s.cfg.RTCSTUNURLs) > 0 {
		servers = append(servers, RTCIceServerResponse{
			URLs: append([]string(nil), s.cfg.RTCSTUNURLs...),
		})
	}
	if len(s.cfg.RTCTURNURLs) > 0 {
		servers = append(servers, RTCIceServerResponse{
			URLs:           append([]string(nil), s.cfg.RTCTURNURLs...),
			Username:       s.cfg.RTCTURNUsername,
			Credential:     s.cfg.RTCTURNCredential,
			CredentialType: s.cfg.RTCTURNCredentialType,
		})
	}
	return servers
}

type CapabilitiesResponse struct {
	ServerName             string                        `json:"server_name"`
	ServerID               string                        `json:"server_id"`
	APIVersion             string                        `json:"api_version"`
	BuildVersion           string                        `json:"build_version"`
	BuildCommit            string                        `json:"build_commit"`
	IdentityHandshakeModes []string                      `json:"identity_handshake_modes"`
	UserUIDPolicy          string                        `json:"user_uid_policy"`
	ProfileDataPolicy      string                        `json:"profile_data_policy"`
	Transport              TransportCapabilitiesResponse `json:"transport"`
	Features               CoreFeatureFlagsResponse      `json:"features"`
	Mentions               MentionCapabilitiesResponse   `json:"mentions"`
	ReadAcks               ReadAckCapabilitiesResponse   `json:"read_acks"`
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

type MentionCapabilitiesResponse struct {
	User            bool     `json:"user"`
	Channel         bool     `json:"channel"`
	Resolve         bool     `json:"resolve"`
	Notifications   bool     `json:"notifications"`
	SupportedTokens []string `json:"supported_tokens"`
}

type ReadAckCapabilitiesResponse struct {
	Channel    bool   `json:"channel"`
	CursorType string `json:"cursor_type"`
	Monotonic  bool   `json:"monotonic"`
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

type RTCSubscribeReceivePolicyResponse struct {
	MaxVideoTracks int `json:"max_video_tracks"`
	MaxAudioTracks int `json:"max_audio_tracks"`
}

type RTCCapabilitiesResponse struct {
	ProtocolVersion    string                            `json:"protocol_version"`
	SignalingURL       string                            `json:"signaling_url"`
	SignalingTransport string                            `json:"signaling_transport"`
	Topologies         []string                          `json:"topologies"`
	Features           RTCFeatureFlagsResponse           `json:"features"`
	IceServers         []RTCIceServerResponse            `json:"ice_servers"`
	ConnectionPolicy   RTCConnectionPolicyResponse       `json:"connection_policy"`
	SubscribeReceive   RTCSubscribeReceivePolicyResponse `json:"subscribe_receive_policy"`
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
	build := app.CurrentBuildInfo()
	subscribeReceiveLimits := s.cfg.ResolveRTCSubscribeReceiveLimits("", "")
	return CapabilitiesResponse{
		ServerName:             "OpenChat Harbor",
		ServerID:               "srv_harbor",
		APIVersion:             "2026-02-14",
		BuildVersion:           build.Version,
		BuildCommit:            build.Commit,
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
		Mentions: MentionCapabilitiesResponse{
			User:            true,
			Channel:         true,
			Resolve:         true,
			Notifications:   true,
			SupportedTokens: []string{"@here", "@channel"},
		},
		ReadAcks: ReadAckCapabilitiesResponse{
			Channel:    true,
			CursorType: "message_id",
			Monotonic:  true,
		},
		Limits: CapabilityLimitsResponse{
			MaxMessageBytes:     65536,
			MaxUploadBytes:      52428800,
			RateLimitPerMinute:  180,
			MaxCallParticipants: 200,
		},
		Security: SecurityCapabilitiesResponse{
			HTTPSRequired:      s.cfg.IsProduction(),
			CertificatePinning: "optional",
		},
		RTC: &RTCCapabilitiesResponse{
			ProtocolVersion:    "2.0",
			SignalingURL:       s.cfg.SignalingURL(),
			SignalingTransport: "websocket",
			Topologies:         []string{"sfu"},
			Features: RTCFeatureFlagsResponse{
				Voice:       true,
				Video:       true,
				Screenshare: true,
				Simulcast:   true,
			},
			IceServers: s.rtcIceServers(),
			ConnectionPolicy: RTCConnectionPolicyResponse{
				JoinTimeoutMs:      12000,
				AnswerTimeoutMs:    10000,
				ICERestartEnabled:  true,
				ReconnectBackoffMs: []int{250, 500, 1000, 2000, 5000},
			},
			SubscribeReceive: RTCSubscribeReceivePolicyResponse{
				MaxVideoTracks: subscribeReceiveLimits.MaxVideoTracks,
				MaxAudioTracks: subscribeReceiveLimits.MaxAudioTracks,
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
