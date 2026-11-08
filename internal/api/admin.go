package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/config"
	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/ratelimit"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
	"github.com/LeivSuaxy/toque-api-provider/internal/web"
)

const (
	minRPM = 1
	maxRPM = 100000
)

type adminHandlers struct {
	cfg       *config.Config
	store     *store.Store
	auth      *auth.Manager
	sessions  *auth.SessionManager
	templates *web.Templates
	limiter   *ratelimit.Limiter
	logger    *slog.Logger
	reveals   *revealStore
}

type pageData struct {
	Title        string
	Username     string
	CSRF         string
	Error        string
	Keys         []store.APIKeyUsage
	Key          store.APIKey
	RevealSecret string
	UsageCards   []usageCard
}

type usageCard struct {
	Label string
	Usage store.Usage
}

func newAdminHandlers(deps Deps) *adminHandlers {
	return &adminHandlers{
		cfg:       deps.Config,
		store:     deps.Store,
		auth:      deps.Auth,
		sessions:  deps.Sessions,
		templates: deps.Templates,
		limiter:   deps.Limiter,
		logger:    deps.Logger,
		reveals:   newRevealStore(),
	}
}

func (h *adminHandlers) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.GetSession(r); ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	h.render(w, http.StatusOK, "login.html", pageData{Title: "Acceso"})
}

func (h *adminHandlers) login(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r, h.cfg.TrustProxyHeaders)
	result := h.limiter.Allow("login:"+ip, h.cfg.LoginRateLimitPerMinute, h.cfg.LoginRateLimitPerMinute)
	if !result.Allowed {
		retryAfter := time.Until(result.Reset)
		if retryAfter < 0 {
			retryAfter = 0
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		h.render(w, http.StatusTooManyRequests, "login.html", pageData{
			Title: "Acceso",
			Error: "Demasiados intentos. Espera un momento antes de reintentar.",
		})
		return
	}

	if err := r.ParseForm(); err != nil {
		h.render(w, http.StatusBadRequest, "login.html", pageData{
			Title: "Acceso",
			Error: "Solicitud inválida.",
		})
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	if !h.sessions.CheckCredentials(username, password) {
		h.logger.Warn("admin login failed", "ip", ip, "username", username)
		h.render(w, http.StatusUnauthorized, "login.html", pageData{
			Title: "Acceso",
			Error: "Credenciales inválidas.",
		})
		return
	}

	if _, err := h.sessions.NewSession(w); err != nil {
		h.logger.Error("create admin session", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *adminHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}
	h.sessions.Clear(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *adminHandlers) dashboard(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromRequest(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	keys, err := h.store.ListAPIKeysWithUsage(r.Context(), since(24*time.Hour))
	if err != nil {
		h.logger.Error("list api keys", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.render(w, http.StatusOK, "dashboard.html", pageData{
		Title:    "API keys",
		Username: session.Username,
		CSRF:     session.CSRF,
		Error:    r.URL.Query().Get("error"),
		Keys:     keys,
	})
}

func (h *adminHandlers) createKey(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" || len([]rune(name)) > 80 {
		h.redirectError(w, r, "El nombre es obligatorio y debe tener máximo 80 caracteres.")
		return
	}

	rateLimit, err := strconv.Atoi(r.PostFormValue("rate_limit_per_minute"))
	if err != nil || rateLimit < minRPM || rateLimit > maxRPM {
		h.redirectError(w, r, "El RPM debe estar entre 1 y 100000.")
		return
	}

	keyID, secret, err := auth.GenerateKey()
	if err != nil {
		h.logger.Error("generate api key", "err", err)
		h.redirectError(w, r, "No se pudo generar la key.")
		return
	}

	id, err := h.store.CreateAPIKey(
		r.Context(),
		keyID,
		auth.HashSecret(secret, []byte(h.cfg.APISecretPepper)),
		name,
		rateLimit,
	)
	if err != nil {
		h.logger.Error("create api key", "err", err)
		h.redirectError(w, r, "No se pudo guardar la key.")
		return
	}

	token := h.reveals.Put(id, secret)
	http.Redirect(w, r, "/admin/keys/"+strconv.FormatInt(id, 10)+"?reveal="+url.QueryEscape(token), http.StatusSeeOther)
}

func (h *adminHandlers) keyDetail(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromRequest(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	id, ok := parseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var revealSecret string
	if token := r.URL.Query().Get("reveal"); token != "" {
		if keyID, secret, ok := h.reveals.Take(token); ok && keyID == id {
			revealSecret = secret
		}
	}

	usage24, usage7, usage30 := h.usage(r, id)

	h.render(w, http.StatusOK, "key_detail.html", pageData{
		Title:        key.Name,
		Username:     session.Username,
		CSRF:         session.CSRF,
		Key:          key,
		RevealSecret: revealSecret,
		UsageCards: []usageCard{
			{Label: "24 horas", Usage: usage24},
			{Label: "7 días", Usage: usage7},
			{Label: "30 días", Usage: usage30},
		},
	})
}

func (h *adminHandlers) updateRateLimit(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	id, ok := parseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	rateLimit, err := strconv.Atoi(r.PostFormValue("rate_limit_per_minute"))
	if err != nil || rateLimit < minRPM || rateLimit > maxRPM {
		http.Error(w, "invalid rate limit", http.StatusBadRequest)
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.store.UpdateAPIKeyRateLimit(r.Context(), id, rateLimit); err != nil {
		h.logger.Error("update api key rate limit", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.auth.Invalidate(key.KeyID)
	http.Redirect(w, r, "/admin/keys/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *adminHandlers) revokeKey(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	id, ok := parseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.store.RevokeAPIKey(r.Context(), id); err != nil {
		h.logger.Error("revoke api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.auth.Invalidate(key.KeyID)
	http.Redirect(w, r, "/admin/keys/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *adminHandlers) enableKey(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	id, ok := parseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.store.EnableAPIKey(r.Context(), id); err != nil {
		h.logger.Error("enable api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.auth.Invalidate(key.KeyID)
	http.Redirect(w, r, "/admin/keys/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (h *adminHandlers) rotateKey(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	id, ok := parseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	key, err := h.store.GetAPIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.Error("get api key", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, secret, err := auth.GenerateKey()
	if err != nil {
		h.logger.Error("generate api secret", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.store.RotateAPIKeySecret(r.Context(), id, auth.HashSecret(secret, []byte(h.cfg.APISecretPepper))); err != nil {
		h.logger.Error("rotate api secret", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.auth.Invalidate(key.KeyID)

	token := h.reveals.Put(id, secret)
	http.Redirect(w, r, "/admin/keys/"+strconv.FormatInt(id, 10)+"?reveal="+url.QueryEscape(token), http.StatusSeeOther)
}

func (h *adminHandlers) usage(r *http.Request, id int64) (store.Usage, store.Usage, store.Usage) {
	periods := []time.Duration{24 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour}
	usages := make([]store.Usage, len(periods))

	for i, period := range periods {
		usage, err := h.store.Usage(r.Context(), id, since(period))
		if err != nil {
			h.logger.Error("usage", "err", err, "api_key_id", id)
			continue
		}
		usages[i] = usage
	}

	return usages[0], usages[1], usages[2]
}

func (h *adminHandlers) form(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	if !h.sessions.ValidateCSRF(r, r.PostFormValue("csrf")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *adminHandlers) render(w http.ResponseWriter, status int, name string, data pageData) {
	if err := h.templates.Render(w, status, name, data); err != nil {
		h.logger.Error("render template", "name", name, "err", err)
	}
}

func (h *adminHandlers) redirectError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin?error="+url.QueryEscape(message), http.StatusSeeOther)
}

func parseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func since(period time.Duration) string {
	return time.Now().UTC().Add(-period).Format("2006-01-02 15:04:05")
}

type revealItem struct {
	keyID   int64
	secret  string
	expires time.Time
}

type revealStore struct {
	mu    sync.Mutex
	items map[string]revealItem
}

func newRevealStore() *revealStore {
	return &revealStore{items: make(map[string]revealItem)}
}

func (s *revealStore) Put(keyID int64, secret string) string {
	token, err := newToken(18)
	if err != nil {
		return ""
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	s.items[token] = revealItem{keyID: keyID, secret: secret, expires: now.Add(2 * time.Minute)}
	return token
}

func (s *revealStore) Take(token string) (int64, string, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)

	item, ok := s.items[token]
	if !ok {
		return 0, "", false
	}
	delete(s.items, token)

	if now.After(item.expires) {
		return 0, "", false
	}
	return item.keyID, item.secret, true
}

func (s *revealStore) cleanupLocked(now time.Time) {
	for token, item := range s.items {
		if now.After(item.expires) {
			delete(s.items, token)
		}
	}
}

func newToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
