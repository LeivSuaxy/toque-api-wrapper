package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

var (
	ErrUnauthorized = errors.New("auth: unauthorized")
	ErrDisabled     = errors.New("auth: disabled")
)

const keyPrefix = "tk_live_"

type Key struct {
	ID                 int64
	KeyID              string
	Name               string
	Enabled            bool
	RateLimitPerMinute int
}

type cacheEntry struct {
	key        Key
	secretHash string
	revokedAt  string
	expires    time.Time
}

type Manager struct {
	store    *store.Store
	pepper   []byte
	cacheTTL time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry

	touchMu   sync.Mutex
	lastTouch map[int64]time.Time

	now func() time.Time
}

func NewManager(store *store.Store, pepper string) *Manager {
	return &Manager{
		store:     store,
		pepper:    []byte(pepper),
		cacheTTL:  30 * time.Second,
		cache:     make(map[string]cacheEntry),
		lastTouch: make(map[int64]time.Time),
		now:       time.Now,
	}
}

func GenerateKey() (keyID, secret string, err error) {
	keyBytes := make([]byte, 16)
	secretBytes := make([]byte, 32)

	if _, err := rand.Read(keyBytes); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", err
	}

	return keyPrefix + base64.RawURLEncoding.EncodeToString(keyBytes),
		base64.RawURLEncoding.EncodeToString(secretBytes),
		nil
}

func HashSecret(secret string, pepper []byte) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Manager) Authenticate(ctx context.Context, keyID, secret string) (Key, error) {
	if keyID == "" || secret == "" {
		return Key{}, ErrUnauthorized
	}

	entry, ok := m.lookup(ctx, keyID)
	if !ok {
		return Key{}, ErrUnauthorized
	}
	if !entry.key.Enabled || entry.revokedAt != "" {
		return Key{}, ErrDisabled
	}

	expected := HashSecret(secret, m.pepper)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(entry.secretHash)) != 1 {
		return Key{}, ErrUnauthorized
	}

	m.touch(ctx, entry.key.ID)
	return entry.key, nil
}

func (m *Manager) Invalidate(keyID string) {
	m.mu.Lock()
	delete(m.cache, keyID)
	m.mu.Unlock()
}

func (m *Manager) lookup(ctx context.Context, keyID string) (cacheEntry, bool) {
	m.mu.RLock()
	entry, ok := m.cache[keyID]
	m.mu.RUnlock()
	if ok && m.now().Before(entry.expires) {
		return entry, true
	}

	key, err := m.store.GetAPIKeyByKeyID(ctx, keyID)
	if err != nil {
		return cacheEntry{}, false
	}

	entry = cacheEntry{
		key: Key{
			ID:                 key.ID,
			KeyID:              key.KeyID,
			Name:               key.Name,
			Enabled:            key.Enabled,
			RateLimitPerMinute: key.RateLimitPerMinute,
		},
		secretHash: key.SecretHash,
		revokedAt:  key.RevokedAt,
		expires:    m.now().Add(m.cacheTTL),
	}

	m.mu.Lock()
	m.cache[keyID] = entry
	m.mu.Unlock()

	return entry, true
}

func (m *Manager) touch(ctx context.Context, id int64) {
	m.touchMu.Lock()
	last, ok := m.lastTouch[id]
	if ok && m.now().Sub(last) < time.Minute {
		m.touchMu.Unlock()
		return
	}
	m.lastTouch[id] = m.now()
	m.touchMu.Unlock()

	touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = m.store.TouchAPIKey(touchCtx, id)
}

type APIKeyContextKey struct{}

func KeyFromContext(ctx context.Context) (Key, bool) {
	key, ok := ctx.Value(APIKeyContextKey{}).(Key)
	return key, ok
}
