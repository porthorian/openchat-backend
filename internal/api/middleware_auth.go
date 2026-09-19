package api

import (
	"context"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"strings"
)

type requesterContextKey struct{}

type sessionContextKey struct{}

// withRequesterContext accepts UID headers only in explicit local development
// or test mode. In other environments it ignores them and verifies a bearer
// token against the server-side session table on every request.
func (s *Server) withRequesterContext(next http.Handler, required bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		devHeaders := s.cfg.Environment == "test" || (s.cfg.Environment == "development" && s.cfg.DevHeaderAuth && isLoopbackPeer(r.RemoteAddr))
		if devHeaders {
			uid := strings.TrimSpace(r.Header.Get("X-OpenChat-User-UID"))
			deviceID := strings.TrimSpace(r.Header.Get("X-OpenChat-Device-ID"))
			if uid == "" {
				uid = strings.TrimSpace(r.URL.Query().Get("user_uid"))
			}
			if deviceID == "" {
				deviceID = strings.TrimSpace(r.URL.Query().Get("device_id"))
			}
			if uid == "" && required {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing local development UID", false)
				return
			}
			if uid == "" {
				uid = "uid_dev_local"
			}
			if deviceID == "" {
				deviceID = "dev_local"
			}
			ctx := context.WithValue(r.Context(), requesterContextKey{}, requester{UserUID: uid, DeviceID: deviceID})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		value := strings.TrimSpace(r.Header.Get("Authorization"))
		token := ""
		if len(value) >= 8 && strings.EqualFold(value[:7], "Bearer ") {
			token = strings.TrimSpace(value[7:])
		}
		if token == "" && r.URL.Path == "/v1/realtime" {
			for _, protocol := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
				if candidate := strings.TrimSpace(protocol); strings.HasPrefix(candidate, "openchat.session.") {
					token = strings.TrimPrefix(candidate, "openchat.session.")
					break
				}
			}
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "verified session required", false)
			return
		}
		session, err := s.auth.VerifyToken(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "session expired or invalid", false)
			return
		}
		serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
		if channelID := strings.TrimSpace(chi.URLParam(r, "channelID")); channelID != "" {
			if channelServerID, ok := s.chat.ServerIDForChannel(channelID); ok {
				serverID = channelServerID
			}
		}
		if r.URL.Path == "/v1/realtime" {
			serverID = session.ServerID
		}
		if serverID != "" {
			if serverID != session.ServerID {
				writeError(w, http.StatusForbidden, "server_mismatch", "session belongs to another server", false)
				return
			}
			allowed, err := s.auth.CanAccessServer(r.Context(), serverID, session.UserUID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "membership_unavailable", "membership check unavailable", true)
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "membership_required", "active membership required", false)
				return
			}
		}
		ctx := context.WithValue(r.Context(), requesterContextKey{}, requester{UserUID: session.UserUID, DeviceID: session.DeviceID})
		ctx = context.WithValue(ctx, sessionContextKey{}, session)
		// Downstream websocket handlers still read legacy headers. Replace
		// untrusted values with the verified session identity before upgrade.
		trusted := r.Clone(ctx)
		trusted.Header = r.Header.Clone()
		trusted.Header.Set("X-OpenChat-User-UID", session.UserUID)
		trusted.Header.Set("X-OpenChat-Device-ID", session.DeviceID)
		trusted.Header.Set("X-OpenChat-Server-ID", session.ServerID)
		next.ServeHTTP(w, trusted)
	})
}

func requesterFromContext(ctx context.Context) requester {
	value, ok := ctx.Value(requesterContextKey{}).(requester)
	if !ok {
		return requester{}
	}
	return value
}

func isLoopbackPeer(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
