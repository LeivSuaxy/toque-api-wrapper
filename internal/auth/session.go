package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const sessionCookieName = "toque_admin_session"

type Session struct {
	Username  string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
	CSRF      string `json:"csrf"`
}

type SessionManager struct {
	username     string
	password     string
	passwordHash string
	secret       []byte
	ttl          time.Duration
	secure       bool
	now          func() time.Time
}

type sessionContextKey struct{}

func NewSessionManager(username, password, passwordHash string, secret []byte, ttl time.Duration, secure bool) *SessionManager {
	return &SessionManager{
		username:     username,
		password:     password,
		passwordHash: strings.ToLower(strings.TrimSpace(passwordHash)),
		secret:       secret,
		ttl:          ttl,
		secure:       secure,
		now:          time.Now,
	}
}

func (m *SessionManager) CheckCredentials(username, password string) bool {
	if subtle.ConstantTimeCompare([]byte(username), []byte(m.username)) != 1 {
		return false
	}

	if m.passwordHash != "" {
		sum := sha256.Sum256([]byte(password))
		got := hex.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(got), []byte(m.passwordHash)) == 1
	}

	return subtle.ConstantTimeCompare([]byte(password), []byte(m.password)) == 1
}

func (m *SessionManager) NewSession(w http.ResponseWriter) (*Session, error) {
	csrf, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	session := &Session{
		Username:  m.username,
		ExpiresAt: m.now().Add(m.ttl).Unix(),
		CSRF:      csrf,
	}

	value, err := m.sign(session)
	if err != nil {
		return nil, err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(m.ttl.Seconds()),
		Expires:  m.now().Add(m.ttl),
	})

	return session, nil
}

func (m *SessionManager) GetSession(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, false
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return nil, false
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}

	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}

	var session Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, false
	}
	if session.Username == "" || session.Username != m.username || m.now().Unix() >= session.ExpiresAt {
		return nil, false
	}

	return &session, true
}

func (m *SessionManager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (m *SessionManager) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := m.GetSession(r)
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *SessionManager) ValidateCSRF(r *http.Request, token string) bool {
	session, ok := SessionFromRequest(r)
	if !ok || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(token)) == 1
}

func SessionFromContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(*Session)
	return session, ok
}

func SessionFromRequest(r *http.Request) (*Session, bool) {
	return SessionFromContext(r.Context())
}

func (m *SessionManager) sign(session *Session) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", err
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + signature, nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
