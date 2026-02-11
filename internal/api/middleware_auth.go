package api

import (
	"context"
	"net/http"
	"strings"
)

type requesterContextKey struct{}

func withRequesterContext(next http.Handler, strict bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimSpace(r.Header.Get("X-OpenChat-User-UID"))
		deviceID := strings.TrimSpace(r.Header.Get("X-OpenChat-Device-ID"))

		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if uid == "" && strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			uid = strings.TrimSpace(authHeader[len("Bearer "):])
		}

		if uid == "" && strict {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing user identity headers", false)
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
	})
}

func requesterFromContext(ctx context.Context) requester {
	value, ok := ctx.Value(requesterContextKey{}).(requester)
	if !ok {
		return requester{UserUID: "uid_dev_local", DeviceID: "dev_local"}
	}
	return value
}
