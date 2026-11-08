package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/config"
	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/ratelimit"
	"github.com/LeivSuaxy/toque-api-provider/internal/rates"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
	"github.com/LeivSuaxy/toque-api-provider/internal/trace"
	"github.com/LeivSuaxy/toque-api-provider/internal/web"
)

type Deps struct {
	Config    *config.Config
	Store     *store.Store
	Rates     *rates.Service
	Auth      *auth.Manager
	Sessions  *auth.SessionManager
	Limiter   *ratelimit.Limiter
	Trace     *trace.Middleware
	Templates *web.Templates
	Logger    *slog.Logger
}

func NewRouter(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	admin := newAdminHandlers(deps)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", deps.health)
	mux.Handle("GET /v1/rates", deps.protected(http.HandlerFunc(deps.rates)))

	mux.HandleFunc("GET /admin/login", admin.loginPage)
	mux.HandleFunc("POST /admin/login", admin.login)
	mux.Handle("POST /admin/logout", deps.Sessions.Require(http.HandlerFunc(admin.logout)))
	mux.Handle("GET /admin", deps.Sessions.Require(http.HandlerFunc(admin.dashboard)))
	mux.Handle("POST /admin/keys", deps.Sessions.Require(http.HandlerFunc(admin.createKey)))
	mux.Handle("GET /admin/keys/{id}", deps.Sessions.Require(http.HandlerFunc(admin.keyDetail)))
	mux.Handle("POST /admin/keys/{id}/rate-limit", deps.Sessions.Require(http.HandlerFunc(admin.updateRateLimit)))
	mux.Handle("POST /admin/keys/{id}/revoke", deps.Sessions.Require(http.HandlerFunc(admin.revokeKey)))
	mux.Handle("POST /admin/keys/{id}/enable", deps.Sessions.Require(http.HandlerFunc(admin.enableKey)))
	mux.Handle("POST /admin/keys/{id}/rotate", deps.Sessions.Require(http.HandlerFunc(admin.rotateKey)))
	mux.Handle("GET /admin/static/", http.StripPrefix("/admin/static/", deps.Templates.Static()))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})

	return deps.recoverer(deps.securityHeaders(deps.limitBody(deps.accessLog(mux))))
}

func (deps Deps) protected(next http.Handler) http.Handler {
	return deps.Auth.Middleware(deps.Trace.Handler(deps.rateLimit(next)))
}

func (deps Deps) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := auth.KeyFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		burst := deps.Config.RateLimitBurst
		if burst <= 0 || burst > key.RateLimitPerMinute {
			burst = key.RateLimitPerMinute
		}

		result := deps.Limiter.Allow(
			strconv.FormatInt(key.ID, 10),
			key.RateLimitPerMinute,
			burst,
		)

		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.Reset.Unix(), 10))

		if !result.Allowed {
			retryAfter := time.Until(result.Reset)
			if retryAfter < 0 {
				retryAfter = 0
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (deps Deps) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if deps.Config.CookieSecure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (deps Deps) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}

func (deps Deps) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		deps.Logger.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (deps Deps) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				deps.Logger.Error("panic recovered",
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
