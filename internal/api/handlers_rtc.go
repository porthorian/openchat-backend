package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/rtc"
)

type joinTicketRequest struct {
	ServerID string `json:"server_id"`
}

func (s *Server) issueJoinTicket(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}
	if !s.chat.ChannelExists(channelID) {
		writeError(w, http.StatusNotFound, "channel_not_found", "unknown voice channel", false)
		return
	}
	if !s.chat.IsVoiceChannel(channelID) {
		writeError(w, http.StatusBadRequest, "invalid_channel_type", "join ticket can only be created for voice channels", false)
		return
	}

	requester := requesterFromContext(r.Context())
	var body joinTicketRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	serverID := strings.TrimSpace(body.ServerID)
	if serverID == "" {
		serverID = s.capabilities.Build().ServerID
	}
	if !s.chat.ServerExists(serverID) {
		writeError(w, http.StatusNotFound, "server_not_found", "unknown server", false)
		return
	}

	ticket, claims, err := s.tokens.Issue(rtc.IssueTicketInput{
		ServerID:  serverID,
		ChannelID: channelID,
		UserUID:   requester.UserUID,
		DeviceID:  requester.DeviceID,
		Permissions: rtc.Permissions{
			Speak:       true,
			Video:       true,
			Screenshare: true,
		},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "rtc_ticket_issue_failed", err.Error(), false)
		return
	}

	capabilities := s.capabilities.Build()
	iceServers := []any{}
	if capabilities.RTC != nil {
		for _, ice := range capabilities.RTC.IceServers {
			server := map[string]any{"urls": ice.URLs}
			if ice.Username != "" {
				server["username"] = ice.Username
			}
			if ice.Credential != "" {
				server["credential"] = ice.Credential
			}
			if ice.CredentialType != "" {
				server["credential_type"] = ice.CredentialType
			}
			if ice.ExpiresAt != "" {
				server["expires_at"] = ice.ExpiresAt
			}
			iceServers = append(iceServers, server)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":        ticket,
		"channel_id":    claims.ChannelID,
		"server_id":     claims.ServerID,
		"user_uid":      claims.UserUID,
		"device_id":     claims.DeviceID,
		"expires_at":    time.Unix(claims.ExpiresAt, 0).UTC().Format(time.RFC3339),
		"signaling_url": s.cfg.SignalingURL(),
		"ice_servers":   iceServers,
		"permissions":   claims.Permissions,
	})
}

func (s *Server) signalingWS(w http.ResponseWriter, r *http.Request) {
	s.signaling.ServeWS(w, r)
}
