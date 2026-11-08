package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ElToqueAPIKey  string
	ElToqueBaseURL string

	DatabasePath string
	HTTPAddr     string

	FetchInterval time.Duration
	SampleSize    int
	Retention     time.Duration

	RequestLogRetention  time.Duration
	AuthFailureRetention time.Duration

	SuperadminUsername     string
	SuperadminPassword     string
	SuperadminPasswordHash string

	SessionSecret          string
	SessionSecretGenerated bool
	APISecretPepper        string
	AdminSessionTTL        time.Duration
	CookieSecure           bool

	RateLimitPerMinute      int
	RateLimitBurst          int
	LoginRateLimitPerMinute int
	TrustProxyHeaders       bool

	LogLevel string
}

func Load() (*Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return nil, err
	}

	cfg := &Config{
		ElToqueAPIKey:  getenv("ELTOQUE_API_KEY", ""),
		ElToqueBaseURL: getenv("ELTOQUE_BASE_URL", "https://tasas.eltoque.com/v1/trmi"),

		DatabasePath: getenv("DATABASE_PATH", "./data/toque.db"),
		HTTPAddr:     getenv("HTTP_ADDR", ":8080"),

		FetchInterval: getenvDuration("FETCH_INTERVAL", 5*time.Minute),
		SampleSize:    getenvInt("SAMPLE_SIZE", 25),
		Retention:     getenvDuration("RETENTION", 30*24*time.Hour),

		RequestLogRetention:  getenvDuration("REQUEST_LOG_RETENTION", 90*24*time.Hour),
		AuthFailureRetention: getenvDuration("AUTH_FAILURE_RETENTION", 30*24*time.Hour),

		SuperadminUsername:     getenv("SUPERADMIN_USERNAME", ""),
		SuperadminPassword:     getenv("SUPERADMIN_PASSWORD", ""),
		SuperadminPasswordHash: normalizeHash(getenv("SUPERADMIN_PASSWORD_HASH", "")),

		SessionSecret:   getenv("SESSION_SECRET", ""),
		APISecretPepper: getenv("API_SECRET_PEPPER", ""),
		AdminSessionTTL: getenvDuration("ADMIN_SESSION_TTL", 12*time.Hour),
		CookieSecure:    getenvBool("COOKIE_SECURE", true),

		RateLimitPerMinute:      getenvInt("RATE_LIMIT_PER_MINUTE", 60),
		RateLimitBurst:          getenvInt("RATE_LIMIT_BURST", 60),
		LoginRateLimitPerMinute: getenvInt("LOGIN_RATE_LIMIT_PER_MINUTE", 5),
		TrustProxyHeaders:       getenvBool("TRUST_PROXY_HEADERS", false),

		LogLevel: getenv("LOG_LEVEL", "info"),
	}

	if cfg.SessionSecret == "" {
		secret, err := randomSecret()
		if err != nil {
			return nil, err
		}
		cfg.SessionSecret = secret
		cfg.SessionSecretGenerated = true
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	var errs []error

	if c.ElToqueAPIKey == "" {
		errs = append(errs, errors.New("ELTOQUE_API_KEY is required"))
	}
	if c.DatabasePath == "" {
		errs = append(errs, errors.New("DATABASE_PATH is required"))
	}
	if c.HTTPAddr == "" {
		errs = append(errs, errors.New("HTTP_ADDR is required"))
	}
	if c.SampleSize <= 0 {
		errs = append(errs, errors.New("SAMPLE_SIZE must be greater than zero"))
	}
	if c.FetchInterval <= 0 {
		errs = append(errs, errors.New("FETCH_INTERVAL must be greater than zero"))
	}
	if c.SuperadminUsername == "" {
		errs = append(errs, errors.New("SUPERADMIN_USERNAME is required"))
	}
	if c.SuperadminPassword == "" && c.SuperadminPasswordHash == "" {
		errs = append(errs, errors.New("SUPERADMIN_PASSWORD or SUPERADMIN_PASSWORD_HASH is required"))
	}
	if c.APISecretPepper == "" {
		errs = append(errs, errors.New("API_SECRET_PEPPER is required"))
	}
	if c.AdminSessionTTL <= 0 {
		errs = append(errs, errors.New("ADMIN_SESSION_TTL must be greater than zero"))
	}
	if c.RateLimitPerMinute <= 0 {
		errs = append(errs, errors.New("RATE_LIMIT_PER_MINUTE must be greater than zero"))
	}
	if c.RateLimitBurst <= 0 {
		c.RateLimitBurst = c.RateLimitPerMinute
	}

	return errors.Join(errs...)
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = cleanValue(value)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}

	return scanner.Err()
}

func cleanValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return strings.TrimSpace(value)
}

func normalizeHash(hash string) string {
	hash = strings.TrimSpace(strings.ToLower(hash))
	hash = strings.TrimPrefix(hash, "sha256:")
	return hash
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := getenv(key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	raw := getenv(key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	raw := getenv(key, "")
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
