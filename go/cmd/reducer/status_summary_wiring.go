// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	snapshots "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	statestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	statusSummaryWriterEnabledEnv  = "ESHU_STATUS_SUMMARY_WRITER_ENABLED"
	statusSummaryWriterIntervalEnv = "ESHU_STATUS_SUMMARY_WRITER_INTERVAL"
)

// statusSummaryWriterConfig is the reducer's status summary writer setting.
type statusSummaryWriterConfig struct {
	Enabled  bool
	Interval time.Duration
}

// loadStatusSummaryWriterConfig reads the ESHU_STATUS_SUMMARY_WRITER_*
// settings (#7009). The writer is off unless enabled. An unset interval is
// the 10 s default; an unparsable interval, or one below the 5 s floor, is a
// startup error rather than a silent default, because a faster cadence
// measurably slows reducer claims and a typo should not run one.
func loadStatusSummaryWriterConfig(getenv func(string) string) (statusSummaryWriterConfig, error) {
	cfg := statusSummaryWriterConfig{
		Enabled:  loadBoolOrDefault(getenv, statusSummaryWriterEnabledEnv, false),
		Interval: statussummary.DefaultInterval,
	}
	raw := strings.TrimSpace(getenv(statusSummaryWriterIntervalEnv))
	if raw == "" {
		return cfg, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return statusSummaryWriterConfig{}, fmt.Errorf("%s=%q: %w", statusSummaryWriterIntervalEnv, raw, err)
	}
	if interval < statussummary.MinInterval {
		return statusSummaryWriterConfig{}, fmt.Errorf("%s=%s is below the %s minimum",
			statusSummaryWriterIntervalEnv, interval, statussummary.MinInterval)
	}
	cfg.Interval = interval
	return cfg, nil
}

// withStatusSummaryWriter returns service with its StatusSummaryWriter set
// from the environment (nil when the writer is disabled), or the startup
// error for an invalid interval. buildObservedReducerService calls it after
// buildReducerService, which is at the 500-line file budget.
func withStatusSummaryWriter(
	service reducer.Service,
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (reducer.Service, error) {
	writer, err := statusSummaryWriterFor(getenv, database, tracer, instruments, logger)
	if err != nil {
		return reducer.Service{}, err
	}
	service.StatusSummaryWriter = writer
	return service, nil
}

// statusSummaryWriterFor builds the status summary writer (the active-work
// summary and, as a companion row of the same pass, the Terraform-state admin
// evidence), or returns
// nil when ESHU_STATUS_SUMMARY_WRITER_ENABLED is not true, so the default
// reducer starts no writer goroutine and issues no writer SQL. The statement
// and its digest come from the storage package; the database must open
// transactions because a pass locks, computes, and upserts in one.
func statusSummaryWriterFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*statussummary.Runner, error) {
	cfg, err := loadStatusSummaryWriterConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	beginner, ok := database.(db.Beginner)
	if !ok {
		return nil, fmt.Errorf("status summary writer: database %T cannot open transactions", database)
	}
	return &statussummary.Runner{
		DB: beginner,
		Statement: statussummary.Statement{
			ModelKey:     snapshots.ModelActiveWorkSummary,
			SourceSHA256: postgres.ActiveWorkSummarySourceSHA256(),
			Compute:      postgres.ReadActiveWorkSummaryEntries,
		},
		// The Terraform-state admin evidence is a second row in the same pass
		// (#7009): its own digest, as_of guard and compute time.
		Companions: []statussummary.Statement{{
			ModelKey:     snapshots.ModelTerraformState,
			SourceSHA256: statestore.SummarySourceSHA256(),
			Compute: func(ctx context.Context, queryer db.Queryer, _ time.Time) ([]snapshots.Entry, error) {
				return statestore.SummaryEntries(ctx, queryer)
			},
		}},
		Interval:    cfg.Interval,
		Tracer:      tracer,
		Instruments: instruments,
		Logger:      logger,
	}, nil
}
