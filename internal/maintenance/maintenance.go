package maintenance

import (
	"context"
	"log/slog"
	"time"
)

type Store interface {
	PruneAPIRequests(ctx context.Context, before string) (int64, error)
	PruneAuthFailures(ctx context.Context, before string) (int64, error)
}

type Limiter interface {
	Cleanup(maxIdle time.Duration)
}

type Runner struct {
	store            Store
	limiter          Limiter
	requestRetention time.Duration
	authRetention    time.Duration
	interval         time.Duration
	logger           *slog.Logger
}

func New(store Store, limiter Limiter, requestRetention, authRetention time.Duration, logger *slog.Logger) *Runner {
	return &Runner{
		store:            store,
		limiter:          limiter,
		requestRetention: requestRetention,
		authRetention:    authRetention,
		interval:         time.Hour,
		logger:           logger,
	}
}

func (r *Runner) Run(ctx context.Context) {
	r.runOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	now := time.Now().UTC()
	requestsBefore := now.Add(-r.requestRetention).Format("2006-01-02 15:04:05")
	authBefore := now.Add(-r.authRetention).Format("2006-01-02 15:04:05")

	if deleted, err := r.store.PruneAPIRequests(runCtx, requestsBefore); err != nil {
		r.logger.Error("prune api requests", "err", err)
	} else if deleted > 0 {
		r.logger.Info("pruned api requests", "rows", deleted)
	}

	if deleted, err := r.store.PruneAuthFailures(runCtx, authBefore); err != nil {
		r.logger.Error("prune auth failures", "err", err)
	} else if deleted > 0 {
		r.logger.Info("pruned auth failures", "rows", deleted)
	}

	if r.limiter != nil {
		r.limiter.Cleanup(10 * time.Minute)
	}
}
