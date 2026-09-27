// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linkstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type postgresGenerationRetentionPruner struct {
	store postgres.GenerationRetentionStore
}

func generationRetentionRunnerFor(
	database db.ExecQueryer,
	cfg generationRetentionConfig,
) *maintenance.GenerationRetentionRunner {
	if !cfg.Enabled {
		return nil
	}
	return &maintenance.GenerationRetentionRunner{
		Pruner: postgresGenerationRetentionPruner{
			store: postgres.NewGenerationRetentionStore(database),
		},
		Config: cfg.Runner,
	}
}

func (p postgresGenerationRetentionPruner) PruneSupersededGenerations(
	ctx context.Context,
	policy maintenance.GenerationRetentionPolicy,
) (maintenance.GenerationRetentionResult, error) {
	result, err := p.store.PruneSupersededGenerations(ctx, postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: policy.MinSupersededGenerations,
		MaxSupersededAge:         policy.MaxSupersededAge,
		BatchGenerationLimit:     policy.BatchGenerationLimit,
		BatchRowLimit:            policy.BatchRowLimit,
		PolicyScope:              policy.PolicyScope,
		PolicyRevision:           policy.PolicyRevision,
	})
	if err != nil {
		return maintenance.GenerationRetentionResult{}, err
	}
	return maintenance.GenerationRetentionResult{
		GenerationsPruned: result.GenerationsPruned,
		RowsPruned:        result.RowsPruned,
		Skipped:           result.Skipped,
		OldestEligibleAge: result.OldestEligibleAge,
		Duration:          result.Duration,
	}, nil
}

// Changed-since link domain knobs (#7127 PR-3a). The domain is dark: it is
// off by default, and with the switch off it issues no SQL.
const (
	changedSinceLinkEnabledEnv          = "ESHU_CHANGED_SINCE_LINK_ENABLED"
	changedSinceLinkSlotsEnv            = "ESHU_CHANGED_SINCE_LINK_SLOTS"
	changedSinceLinkStatementTimeoutEnv = "ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT"
	changedSinceLinkPollIntervalEnv     = "ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL"
	changedSinceLinkWorkersEnv          = "ESHU_CHANGED_SINCE_LINK_WORKERS"
	changedSinceLinkBackfillScopesEnv   = "ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE"
)

type changedSinceLinkConfig struct {
	Enabled          bool
	Slots            int
	StatementTimeout time.Duration
	Runner           links.Config
}

// loadChangedSinceLinkConfig reads the knobs. An unparsable or non-positive
// value uses the default, as the other reducer maintenance knobs do.
func loadChangedSinceLinkConfig(getenv func(string) string) changedSinceLinkConfig {
	return changedSinceLinkConfig{
		Enabled:          loadBoolOrDefault(getenv, changedSinceLinkEnabledEnv, false),
		Slots:            loadPositiveIntOrDefault(getenv, changedSinceLinkSlotsEnv, linkstore.DefaultSlots),
		StatementTimeout: loadDurationOrDefault(getenv, changedSinceLinkStatementTimeoutEnv, linkstore.DefaultStatementTimeout),
		Runner: links.Config{
			PollInterval:           loadDurationOrDefault(getenv, changedSinceLinkPollIntervalEnv, links.DefaultPollInterval),
			Workers:                loadPositiveIntOrDefault(getenv, changedSinceLinkWorkersEnv, links.DefaultWorkers),
			BackfillScopesPerCycle: loadPositiveIntOrDefault(getenv, changedSinceLinkBackfillScopesEnv, links.DefaultBackfillScopesPerCycle),
		},
	}
}

// changedSinceLinkRunnerFor builds the changed_since_link domain runner, or
// nil when ESHU_CHANGED_SINCE_LINK_ENABLED is not true. It touches the
// database only when the runner runs.
func changedSinceLinkRunnerFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) *links.Runner {
	cfg := loadChangedSinceLinkConfig(getenv)
	if !cfg.Enabled {
		return nil
	}
	writer := linkstore.NewLinkWriter(database)
	writer.Slots = cfg.Slots
	writer.StatementTimeout = cfg.StatementTimeout
	return &links.Runner{
		Linker:      writer,
		Journal:     linkstore.NewJournalStore(database),
		Config:      cfg.Runner,
		Tracer:      tracer,
		Instruments: instruments,
		Logger:      logger,
	}
}
