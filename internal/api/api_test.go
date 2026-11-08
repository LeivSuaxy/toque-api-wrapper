package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/config"
	"github.com/LeivSuaxy/toque-api-provider/internal/ratelimit"
	"github.com/LeivSuaxy/toque-api-provider/internal/rates"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
	"github.com/LeivSuaxy/toque-api-provider/internal/trace"
	"github.com/LeivSuaxy/toque-api-provider/internal/web"
)

type testEnv struct {
	store    *store.Store
	auth     *auth.Manager
	sessions *auth.SessionManager
	router   http.Handler
	keyID    string
	secret   string
	keyDBID  int64
}

func setupEnv(t *testing.T, rpm, burst int) *testEnv {
	t.Helper()

	database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	ctx := context.Background()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.UpsertRates(ctx, []store.RateRecord{
		{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:35:00", SourceTS: 1},
		{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:40:00", SourceTS: 2},
		{CurrencyCode: "USD", Rate: 30, SourceDate: "2026-11-08 08:45:00", SourceTS: 3},
		{CurrencyCode: "ECU", Rate: 100, SourceDate: "2026-11-08 08:45:00", SourceTS: 3},
	}); err != nil {
		t.Fatalf("seed rates: %v", err)
	}

	keyID, secret, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyDBID, err := database.CreateAPIKey(ctx, keyID, auth.HashSecret(secret, []byte("pepper")), "test", rpm)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	cfg := &config.Config{
		SampleSize:              25,
		RateLimitPerMinute:      60,
		RateLimitBurst:          burst,
		LoginRateLimitPerMinute: 100,
		CookieSecure:            false,
		TrustProxyHeaders:       false,
		APISecretPepper:         "pepper",
		AdminSessionTTL:         time.Hour,
		SuperadminUsername:      "admin",
		SuperadminPassword:      "password",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	templates, err := web.New()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	authManager := auth.NewManager(database, "pepper")
	sessionManager := auth.NewSessionManager("admin", "password", "", []byte("session-secret"), time.Hour, false)
	limiter := ratelimit.New()

	router := NewRouter(Deps{
		Config:    cfg,
		Store:     database,
		Rates:     rates.NewService(database, 25),
		Auth:      authManager,
		Sessions:  sessionManager,
		Limiter:   limiter,
		Trace:     trace.New(database, logger, false),
		Templates: templates,
		Logger:    logger,
	})

	return &testEnv{
		store:    database,
		auth:     authManager,
		sessions: sessionManager,
		router:   router,
		keyID:    keyID,
		secret:   secret,
		keyDBID:  keyDBID,
	}
}

func (e *testEnv) headers() map[string]string {
	return map[string]string{
		"X-API-Key":    e.keyID,
		"X-API-Secret": e.secret,
	}
}

func (e *testEnv) request(method, target string, form url.Values, headers map[string]string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	request := httptest.NewRequest(method, target, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	e.router.ServeHTTP(recorder, request)
	return recorder
}

func (e *testEnv) adminSession(t *testing.T) (*auth.Session, *http.Cookie) {
	t.Helper()

	recorder := httptest.NewRecorder()
	session, err := e.sessions.NewSession(recorder)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	return session, cookies[0]
}

func TestRatesRequiresAuth(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodGet, "/v1/rates", nil, nil, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestRatesFixedMedianByDefault(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var payload ratesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Fixed || payload.SampleSize != 25 {
		t.Fatalf("payload = %+v", payload)
	}

	usd := findResult(payload.Data, "USD")
	if usd == nil {
		t.Fatal("USD missing")
	}
	if usd.Rate != 20 || usd.RecordsUsed != 3 {
		t.Fatalf("USD = %+v", usd)
	}
}

func TestRatesLatest(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodGet, "/v1/rates?fixed=false", nil, env.headers(), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}

	var payload ratesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	usd := findResult(payload.Data, "USD")
	if usd == nil || usd.Rate != 30 || usd.RecordsUsed != 1 {
		t.Fatalf("USD = %+v", usd)
	}
}

func TestRatesFilterAndErrors(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodGet, "/v1/rates?currency=USD", nil, env.headers(), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("filter status = %d", response.Code)
	}
	var payload ratesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].CurrencyCode != "USD" {
		t.Fatalf("data = %+v", payload.Data)
	}

	response = env.request(http.MethodGet, "/v1/rates?currency=XXX", nil, env.headers(), nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown currency status = %d", response.Code)
	}

	response = env.request(http.MethodGet, "/v1/rates?fixed=maybe", nil, env.headers(), nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid fixed status = %d", response.Code)
	}
}

func TestRateLimit(t *testing.T) {
	env := setupEnv(t, 1, 1)

	first := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}

	second := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	if second.Header().Get("X-RateLimit-Limit") != "1" {
		t.Fatalf("limit header = %q", second.Header().Get("X-RateLimit-Limit"))
	}
}

func TestTraceRecorded(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}

	since := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	usage, err := env.store.Usage(context.Background(), env.keyDBID, since)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.Requests != 1 {
		t.Fatalf("requests = %d, want 1", usage.Requests)
	}
	if usage.ResponseBytes == 0 {
		t.Fatal("response bytes not recorded")
	}
}

func TestAdminLoginAndDashboard(t *testing.T) {
	env := setupEnv(t, 60, 10)

	response := env.request(http.MethodPost, "/admin/login", url.Values{
		"username": {"admin"},
		"password": {"password"},
	}, nil, nil)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/admin" {
		t.Fatalf("location = %q", response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}

	dashboard := env.request(http.MethodGet, "/admin", nil, nil, cookies)
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", dashboard.Code)
	}
	if !strings.Contains(dashboard.Body.String(), "API keys") {
		t.Fatal("dashboard body missing title")
	}

	bad := env.request(http.MethodPost, "/admin/login", url.Values{
		"username": {"admin"},
		"password": {"wrong"},
	}, nil, nil)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", bad.Code)
	}
}

func TestAdminCreateKeyRevealsSecretOnce(t *testing.T) {
	env := setupEnv(t, 60, 10)
	session, cookie := env.adminSession(t)

	response := env.request(http.MethodPost, "/admin/keys", url.Values{
		"csrf":                  {session.CSRF},
		"name":                  {"nueva"},
		"rate_limit_per_minute": {"60"},
	}, nil, []*http.Cookie{cookie})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}

	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/admin/keys/") || !strings.Contains(location, "reveal=") {
		t.Fatalf("location = %q", location)
	}

	detail := env.request(http.MethodGet, location, nil, nil, []*http.Cookie{cookie})
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}
	if !strings.Contains(detail.Body.String(), "Secret generado") {
		t.Fatal("secret not revealed")
	}

	again := env.request(http.MethodGet, location, nil, nil, []*http.Cookie{cookie})
	if strings.Contains(again.Body.String(), "Secret generado") {
		t.Fatal("secret revealed twice")
	}

	keys, err := env.store.ListAPIKeysWithUsage(context.Background(), time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %d, want 2", len(keys))
	}
}

func TestAdminCSRFRejected(t *testing.T) {
	env := setupEnv(t, 60, 10)
	_, cookie := env.adminSession(t)

	response := env.request(http.MethodPost, "/admin/keys", url.Values{
		"name":                  {"nueva"},
		"rate_limit_per_minute": {"60"},
	}, nil, []*http.Cookie{cookie})
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestAdminUpdateRateLimit(t *testing.T) {
	env := setupEnv(t, 60, 60)

	// Populate the auth cache before changing the limit.
	first := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d", first.Code)
	}

	session, cookie := env.adminSession(t)
	response := env.request(
		http.MethodPost,
		"/admin/keys/"+strconv.FormatInt(env.keyDBID, 10)+"/rate-limit",
		url.Values{
			"csrf":                  {session.CSRF},
			"rate_limit_per_minute": {"2"},
		},
		nil,
		[]*http.Cookie{cookie},
	)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, body = %s", response.Code, response.Body.String())
	}

	for i := 0; i < 2; i++ {
		allowed := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
		if allowed.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i+1, allowed.Code)
		}
	}

	limited := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d, want 429", limited.Code)
	}
}

func TestAdminRevokeInvalidatesKey(t *testing.T) {
	env := setupEnv(t, 60, 10)
	session, cookie := env.adminSession(t)

	response := env.request(
		http.MethodPost,
		"/admin/keys/"+strconv.FormatInt(env.keyDBID, 10)+"/revoke",
		url.Values{"csrf": {session.CSRF}},
		nil,
		[]*http.Cookie{cookie},
	)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("revoke status = %d, body = %s", response.Code, response.Body.String())
	}

	ratesResponse := env.request(http.MethodGet, "/v1/rates", nil, env.headers(), nil)
	if ratesResponse.Code != http.StatusUnauthorized {
		t.Fatalf("api status after revoke = %d, want 401", ratesResponse.Code)
	}
}

func findResult(results []rates.Result, code string) *rates.Result {
	for i := range results {
		if results[i].CurrencyCode == code {
			return &results[i]
		}
	}
	return nil
}
