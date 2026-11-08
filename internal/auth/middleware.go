package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyID := r.Header.Get("X-API-Key")
		secret := r.Header.Get("X-API-Secret")

		key, err := m.Authenticate(r.Context(), keyID, secret)
		if err != nil {
			m.recordFailure(r, keyID, err)
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid api key or secret")
			return
		}

		ctx := context.WithValue(r.Context(), APIKeyContextKey{}, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Manager) recordFailure(r *http.Request, keyID string, cause error) {
	reason := "bad_credentials"
	switch {
	case keyID == "" || r.Header.Get("X-API-Secret") == "":
		reason = "missing_headers"
	case errors.Is(cause, ErrDisabled):
		reason = "disabled"
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()

	_ = m.store.InsertAuthFailure(ctx, store.AuthFailure{
		KeyID:     keyID,
		Reason:    reason,
		RemoteIP:  httpx.ClientIP(r, false),
		UserAgent: r.UserAgent(),
	})
}
