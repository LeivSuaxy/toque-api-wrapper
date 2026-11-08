package collector

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/eltoque"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

type Client interface {
	Fetch(ctx context.Context) (eltoque.Rates, error)
}

type RateStore interface {
	UpsertRates(ctx context.Context, records []store.RateRecord) error
	PruneRates(ctx context.Context, cutoffTS int64) (int64, error)
	InsertSyncError(ctx context.Context, message, detail string) error
}

type Collector struct {
	client    Client
	store     RateStore
	interval  time.Duration
	retention time.Duration
	logger    *slog.Logger
}

func New(client Client, store RateStore, interval, retention time.Duration, logger *slog.Logger) *Collector {
	return &Collector{
		client:    client,
		store:     store,
		interval:  interval,
		retention: retention,
		logger:    logger,
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.runOnce(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

func (c *Collector) RunOnce(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	rates, err := c.client.Fetch(fetchCtx)
	if err != nil {
		c.recordError(ctx, "fetch eltoque rates", err)
		return err
	}

	records := make([]store.RateRecord, 0, len(rates.Tasas))
	for code, rate := range rates.Tasas {
		records = append(records, store.RateRecord{
			CurrencyCode: code,
			Rate:         rate,
			SourceDate:   rates.SourceDate,
			SourceTS:     rates.SourceTS,
		})
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CurrencyCode < records[j].CurrencyCode
	})

	writeCtx, cancelWrite := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWrite()

	if err := c.store.UpsertRates(writeCtx, records); err != nil {
		c.recordError(ctx, "persist eltoque rates", err)
		return err
	}

	cutoff := time.Now().Add(-c.retention).Unix()
	if _, err := c.store.PruneRates(writeCtx, cutoff); err != nil {
		c.logger.Error("prune rates", "err", err)
	}

	c.logger.Info("eltoque rates collected",
		"source_date", rates.SourceDate,
		"currencies", len(records),
	)
	return nil
}

func (c *Collector) runOnce(ctx context.Context) {
	if err := c.RunOnce(ctx); err != nil {
		c.logger.Error("collect eltoque rates", "err", err)
	}
}

func (c *Collector) recordError(ctx context.Context, message string, cause error) {
	errorCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := c.store.InsertSyncError(errorCtx, message, cause.Error()); err != nil {
		c.logger.Error("record sync error", "err", err)
	}
}
