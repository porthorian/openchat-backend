package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/auth"
	"github.com/openchat/openchat-backend/internal/capabilities"
	"github.com/openchat/openchat-backend/internal/chat"
	"github.com/openchat/openchat-backend/internal/profile"
	"github.com/openchat/openchat-backend/internal/realtime"
	"github.com/openchat/openchat-backend/internal/rtc"
	"github.com/openchat/openchat-backend/internal/serveractions"
)

type Server struct {
	cfg          app.Config
	logger       *slog.Logger
	capabilities *capabilities.Service
	tokens       *rtc.TokenService
	signaling    *rtc.SignalingService
	chat         *chat.Service
	realtime     *realtime.Hub
	profiles     *profile.Service
	auth         *auth.Service
	actions      *serveractions.Service
}

func NewServer(cfg app.Config, logger *slog.Logger) *Server {
	capSvc := capabilities.NewService(cfg)
	capabilitiesSnapshot := capSvc.Build()
	tokens := rtc.NewTokenService(cfg.TicketSecret, cfg.TicketTTL)
	signaling := rtc.NewSignalingService(logger, tokens, buildRTCSignalingConfig(capabilitiesSnapshot))
	chatService := chat.NewService(cfg.PublicBaseURL)
	realtimeHub := realtime.NewHub(logger)
	chatService.SetBroadcaster(realtimeHub)
	realtimeHub.SetChannelServerResolver(chatService.ServerIDForChannel)

	profileService := profile.NewService(cfg.PublicBaseURL, capabilitiesSnapshot.ServerID)
	profileService.SetBroadcaster(realtimeHub)

	return &Server{
		cfg:          cfg,
		logger:       logger,
		capabilities: capSvc,
		tokens:       tokens,
		signaling:    signaling,
		chat:         chatService,
		realtime:     realtimeHub,
		profiles:     profileService,
	}
}

func (s *Server) SetAuthService(service *auth.Service) {
	s.auth = service
	if service != nil {
		s.actions = serveractions.New(service.DB)
		s.signaling.SetJoinAuthorizer(func(ctx context.Context, claims rtc.TicketClaims) error {
			serverID, ok := s.chat.ServerIDForChannel(claims.ChannelID)
			if !ok || serverID != claims.ServerID {
				return errors.New("RTC channel is not in the ticket server")
			}
			allowed, err := service.CanAccessServer(ctx, claims.ServerID, claims.UserUID)
			if err != nil {
				return err
			}
			if !allowed {
				return errors.New("RTC membership denied")
			}
			return nil
		})
	}
}

func buildRTCSignalingConfig(snapshot capabilities.CapabilitiesResponse) rtc.SignalingConfig {
	cfg := rtc.SignalingConfig{}
	if snapshot.RTC == nil {
		return cfg
	}
	servers := make([]rtc.ICEServerConfig, 0, len(snapshot.RTC.IceServers))
	for _, ice := range snapshot.RTC.IceServers {
		servers = append(servers, rtc.ICEServerConfig{
			URLs:           append([]string(nil), ice.URLs...),
			Username:       ice.Username,
			Credential:     ice.Credential,
			CredentialType: ice.CredentialType,
		})
	}
	cfg.ICEServers = servers
	return cfg
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(withCORS)
	if !s.cfg.IsProduction() {
		router.Use(middleware.Logger)
	}

	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	router.Route("/v1", func(v1 chi.Router) {
		v1.Get("/client/capabilities", s.getCapabilities)
		v1.Post("/servers/{serverID}/sessions/challenge", s.beginSessionChallenge)
		v1.Post("/servers/{serverID}/sessions", s.completeSessionChallenge)
		v1.Get("/rtc/signaling", s.signalingWS)
		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, true) }).Get("/realtime", s.realtimeWS)
		v1.With(func(next http.Handler) http.Handler {
			return s.withRequesterContext(next, false)
		}).Get("/servers", s.listServers)

		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, false) }).Get("/servers/{serverID}/channels", s.listChannelGroups)
		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, false) }).Get("/servers/{serverID}/members", s.listMembers)
		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, false) }).Get("/channels/{channelID}/messages", s.listMessages)
		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, false) }).Get("/channels/{channelID}/attachments/{attachmentID}", s.getMessageAttachment)
		v1.With(func(next http.Handler) http.Handler { return s.withRequesterContext(next, false) }).Get("/profile/avatar/{assetID}", s.getProfileAvatar)

		v1.Group(func(authed chi.Router) {
			authed.Use(func(next http.Handler) http.Handler {
				return s.withRequesterContext(next, true)
			})
			authed.Post("/servers", s.createServer)
			authed.Post("/servers/{serverID}/identity-bindings/{userUID}:approve", s.approveIdentityBinding)
			authed.Put("/servers/{serverID}/read-acks", s.putBulkReadAcks)
			authed.Get("/servers/{serverID}/invites", s.listInvites)
			authed.Post("/servers/{serverID}/invites", s.createInvite)
			authed.Delete("/servers/{serverID}/invites/{inviteID}", s.revokeInvite)
			authed.Post("/invites/{code}/redeem", s.redeemInvite)
			authed.Post("/servers/{serverID}/ownership:claim", s.claimServerOwnership)
			authed.Post("/rtc/channels/{channelID}/join-ticket", s.issueJoinTicket)
			authed.Post("/servers/{serverID}/channels", s.createChannel)
			authed.Post("/servers/{serverID}/categories", s.createCategory)
			authed.Put("/servers/{serverID}/categories/{groupID}", s.putCategory)
			authed.Delete("/servers/{serverID}/categories/{groupID}", s.deleteCategory)
			authed.Put("/servers/{serverID}/channel-layout", s.putChannelLayout)
			authed.Get("/servers/{serverID}/settings", s.getServerSettings)
			authed.Put("/servers/{serverID}/settings", s.putServerSettings)
			authed.Post("/channels/{channelID}/messages", s.createMessage)
			authed.Get("/channels/{channelID}/mentions:resolve", s.resolveMentions)
			authed.Get("/channels/{channelID}/read-ack", s.getReadAck)
			authed.Put("/channels/{channelID}/read-ack", s.putReadAck)
			authed.Delete("/servers/{serverID}/membership", s.leaveServerMembership)
			authed.Get("/profile/me", s.getMyProfile)
			authed.Put("/profile/me", s.updateMyProfile)
			authed.Post("/profile/avatar", s.uploadProfileAvatar)
			authed.Get("/profiles:batch", s.batchProfiles)
		})
	})

	return router
}
