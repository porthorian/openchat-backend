package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/capabilities"
	"github.com/openchat/openchat-backend/internal/chat"
	"github.com/openchat/openchat-backend/internal/profile"
	"github.com/openchat/openchat-backend/internal/realtime"
	"github.com/openchat/openchat-backend/internal/rtc"
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
}

func NewServer(cfg app.Config, logger *slog.Logger) *Server {
	capSvc := capabilities.NewService(cfg)
	tokens := rtc.NewTokenService(cfg.TicketSecret, cfg.TicketTTL)
	signaling := rtc.NewSignalingService(logger, tokens)
	chatService := chat.NewService(cfg.PublicBaseURL)
	realtimeHub := realtime.NewHub(logger)
	chatService.SetBroadcaster(realtimeHub)

	capabilitiesSnapshot := capSvc.Build()
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
		v1.Get("/rtc/signaling", s.signalingWS)
		v1.Get("/realtime", s.realtimeWS)
		v1.With(func(next http.Handler) http.Handler {
			return withRequesterContext(next, false)
		}).Get("/servers", s.listServers)

		v1.Get("/servers/{serverID}/channels", s.listChannelGroups)
		v1.Get("/servers/{serverID}/members", s.listMembers)
		v1.Get("/channels/{channelID}/messages", s.listMessages)
		v1.Get("/channels/{channelID}/attachments/{attachmentID}", s.getMessageAttachment)
		v1.Get("/profile/avatar/{assetID}", s.getProfileAvatar)

		v1.Group(func(authed chi.Router) {
			authed.Use(func(next http.Handler) http.Handler {
				return withRequesterContext(next, s.cfg.IsProduction())
			})
			authed.Post("/rtc/channels/{channelID}/join-ticket", s.issueJoinTicket)
			authed.Post("/channels/{channelID}/messages", s.createMessage)
			authed.Delete("/servers/{serverID}/membership", s.leaveServerMembership)
			authed.Get("/profile/me", s.getMyProfile)
			authed.Put("/profile/me", s.updateMyProfile)
			authed.Post("/profile/avatar", s.uploadProfileAvatar)
			authed.Get("/profiles:batch", s.batchProfiles)
		})
	})

	return router
}
