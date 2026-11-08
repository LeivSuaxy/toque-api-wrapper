package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/api"
	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/collector"
	"github.com/LeivSuaxy/toque-api-provider/internal/config"
	"github.com/LeivSuaxy/toque-api-provider/internal/eltoque"
	"github.com/LeivSuaxy/toque-api-provider/internal/maintenance"
	"github.com/LeivSuaxy/toque-api-provider/internal/ratelimit"
	"github.com/LeivSuaxy/toque-api-provider/internal/rates"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
	"github.com/LeivSuaxy/toque-api-provider/internal/trace"
	"github.com/LeivSuaxy/toque-api-provider/internal/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration", "err", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if cfg.SessionSecretGenerated {
		logger.Warn("SESSION_SECRET was not set; using an ephemeral secret (admin sessions will reset on restart)")
	}
	if cfg.SuperadminPasswordHash == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		logger.Warn("SUPERADMIN_PASSWORD_HASH is the SHA-256 of an empty password; change it before exposing the admin panel")
	}
	if len(cfg.APISecretPepper) < 16 {
		logger.Warn("API_SECRET_PEPPER is shorter than 16 characters; use a long random value")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		logger.Error("create data directory", "err", err)
		os.Exit(1)
	}

	database, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := database.Migrate(ctx); err != nil {
		logger.Error("run migrations", "err", err)
		_ = database.Close()
		os.Exit(1)
	}

	templates, err := web.New()
	if err != nil {
		logger.Error("load templates", "err", err)
		_ = database.Close()
		os.Exit(1)
	}

	client := eltoque.NewClient(cfg.ElToqueBaseURL, cfg.ElToqueAPIKey)
	rateService := rates.NewService(database, cfg.SampleSize)
	authManager := auth.NewManager(database, cfg.APISecretPepper)
	sessionManager := auth.NewSessionManager(
		cfg.SuperadminUsername,
		cfg.SuperadminPassword,
		cfg.SuperadminPasswordHash,
		[]byte(cfg.SessionSecret),
		cfg.AdminSessionTTL,
		cfg.CookieSecure,
	)
	limiter := ratelimit.New()
	tracer := trace.New(database, logger, cfg.TrustProxyHeaders)

	collectorRunner := collector.New(client, database, cfg.FetchInterval, cfg.Retention, logger)
	maintenanceRunner := maintenance.New(
		database,
		limiter,
		cfg.RequestLogRetention,
		cfg.AuthFailureRetention,
		logger,
	)

	router := api.NewRouter(api.Deps{
		Config:    cfg,
		Store:     database,
		Rates:     rateService,
		Auth:      authManager,
		Sessions:  sessionManager,
		Limiter:   limiter,
		Trace:     tracer,
		Templates: templates,
		Logger:    logger,
	})

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		collectorRunner.Run(ctx)
	}()
	go func() {
		defer workers.Done()
		maintenanceRunner.Run(ctx)
	}()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown http server", "err", err)
	}

	workers.Wait()

	if err := database.Close(); err != nil {
		logger.Error("close database", "err", err)
	}
}

func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		addr = ":" + port
	}

	switch {
	case strings.HasPrefix(addr, ":"):
		addr = "127.0.0.1" + addr
	case strings.HasPrefix(addr, "0.0.0.0:"):
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	case strings.HasPrefix(addr, "[::]:"):
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "[::]:")
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
