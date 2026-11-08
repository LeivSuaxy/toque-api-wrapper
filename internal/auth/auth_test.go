package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func TestGenerateKey(t *testing.T) {
	keyID, secret, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if !strings.HasPrefix(keyID, keyPrefix) {
		t.Fatalf("key id = %q", keyID)
	}

	rawKey, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(keyID, keyPrefix))
	if err != nil || len(rawKey) != 16 {
		t.Fatalf("decoded key length = %d, err = %v", len(rawKey), err)
	}
	rawSecret, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(rawSecret) != 32 {
		t.Fatalf("decoded secret length = %d, err = %v", len(rawSecret), err)
	}
}

func TestHashSecret(t *testing.T) {
	first := HashSecret("secret", []byte("pepper"))
	second := HashSecret("secret", []byte("pepper"))
	if first != second {
		t.Fatalf("hash is not deterministic")
	}
	if first == HashSecret("secret", []byte("other")) {
		t.Fatalf("hash should depend on pepper")
	}
}

func TestCheckCredentials(t *testing.T) {
	plain := NewSessionManager("admin", "password", "", []byte("secret"), time.Hour, false)
	if !plain.CheckCredentials("admin", "password") {
		t.Fatal("plain credentials should match")
	}
	if plain.CheckCredentials("admin", "wrong") {
		t.Fatal("wrong password should not match")
	}
	if plain.CheckCredentials("other", "password") {
		t.Fatal("wrong username should not match")
	}

	sum := sha256.Sum256([]byte("password"))
	hashed := NewSessionManager("admin", "", hex.EncodeToString(sum[:]), []byte("secret"), time.Hour, false)
	if !hashed.CheckCredentials("admin", "password") {
		t.Fatal("hashed credentials should match")
	}
	if hashed.CheckCredentials("admin", "wrong") {
		t.Fatal("wrong password should not match hash")
	}
}

func TestSessionFlow(t *testing.T) {
	manager := NewSessionManager("admin", "password", "", []byte("secret"), time.Hour, false)

	recorder := httptest.NewRecorder()
	session, err := manager.NewSession(recorder)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}

	request := httptest.NewRequest("GET", "/admin", nil)
	request.AddCookie(cookies[0])

	got, ok := manager.GetSession(request)
	if !ok {
		t.Fatal("session not found")
	}
	if got.Username != "admin" || got.CSRF == "" {
		t.Fatalf("session = %+v", got)
	}

	ctx := context.WithValue(request.Context(), sessionContextKey{}, got)
	request = request.WithContext(ctx)
	if !manager.ValidateCSRF(request, session.CSRF) {
		t.Fatal("valid CSRF rejected")
	}
	if manager.ValidateCSRF(request, "wrong") {
		t.Fatal("invalid CSRF accepted")
	}

	manager.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, ok := manager.GetSession(request); ok {
		t.Fatal("expired session accepted")
	}
}

func TestManagerAuthenticate(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	keyID, secret, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	id, err := database.CreateAPIKey(ctx, keyID, HashSecret(secret, []byte("pepper")), "test", 60)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	manager := NewManager(database, "pepper")

	key, err := manager.Authenticate(ctx, keyID, secret)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if key.ID != id || key.Name != "test" || key.RateLimitPerMinute != 60 {
		t.Fatalf("key = %+v", key)
	}

	if _, err := manager.Authenticate(ctx, keyID, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if _, err := manager.Authenticate(ctx, "missing", secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}

	if err := database.RevokeAPIKey(ctx, id); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	manager.Invalidate(keyID)
	if _, err := manager.Authenticate(ctx, keyID, secret); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}

	if err := database.EnableAPIKey(ctx, id); err != nil {
		t.Fatalf("EnableAPIKey: %v", err)
	}
	manager.Invalidate(keyID)
	if _, err := manager.Authenticate(ctx, keyID, secret); err != nil {
		t.Fatalf("Authenticate after enable: %v", err)
	}
}
