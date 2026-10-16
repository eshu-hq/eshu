// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linkstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// loadGenerationRetentionConfig reads the generation retention env settings.
// An unset, unparsable, or non-positive hard ceiling resolves to
// postgres.DefaultGenerationRetentionHardMaxAge of the effective soft window
// (#7611); an explicit value is kept as set for validation to judge.
func loadGenerationRetentionConfig(getenv func(string) string) generationRetentionConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	defaults := postgres.DefaultGenerationRetentionPolicy()
	maxAge := loadDurationOrDefault(getenv, generationRetentionMaxSupersededAgeEnv, defaults.MaxSupersededAge)
	hardMaxAge := loadDurationOrDefault(getenv, generationRetentionHardMaxSupersededAgeEnv, 0)
	lifted := false
	if hardMaxAge == 0 {
		hardMaxAge = postgres.DefaultGenerationRetentionHardMaxAge(maxAge)
		lifted = hardMaxAge > defaults.HardMaxSupersededAge
	}
	return generationRetentionConfig{
		Enabled: loadBoolOrDefault(getenv, generationRetentionEnabledEnv, true),
		Runner: retention.Config{
			PollInterval: loadDurationOrDefault(getenv, generationRetentionPollIntervalEnv, defaultGenerationRetentionPollInterval),
			Policy: retention.Policy{
				MinSupersededGenerations: loadPositiveIntOrDefault(getenv, generationRetentionMinSupersededGenerationsEnv, defaults.MinSupersededGenerations),
				MaxSupersededAge:         maxAge,
				HardMaxSupersededAge:     hardMaxAge,
				BatchGenerationLimit:     loadPositiveIntOrDefault(getenv, generationRetentionBatchGenerationLimitEnv, defaults.BatchGenerationLimit),
				BatchRowLimit:            loadPositiveIntOrDefault(getenv, generationRetentionBatchRowLimitEnv, defaults.BatchRowLimit),
				PolicyScope:              loadStringOrDefault(getenv, generationRetentionPolicyScopeEnv, defaults.PolicyScope),
				PolicyRevision:           loadStringOrDefault(getenv, generationRetentionPolicyRevisionEnv, defaults.PolicyRevision),
			},
		},
		HardMaxSupersededAgeLifted: lifted,
	}
}

func validateGenerationRetentionConfig(
	getenv func(string) string,
	cfg generationRetentionConfig,
) error {
	// The hard history ceiling (#7585) caps the soft keep window: a hard
	// ceiling below the soft age promises retention the ceiling denies, so
	// that combination fails closed instead of silently deleting what the
	// soft window says to keep.
	if cfg.Runner.Policy.HardMaxSupersededAge < cfg.Runner.Policy.MaxSupersededAge {
		return fmt.Errorf("%s (%v) must not be below %s (%v): the hard history ceiling caps the soft keep window",
			generationRetentionHardMaxSupersededAgeEnv, cfg.Runner.Policy.HardMaxSupersededAge,
			generationRetentionMaxSupersededAgeEnv, cfg.Runner.Policy.MaxSupersededAge)
	}
	if cfg.Enabled {
		return nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	profile, err := query.ParseQueryProfile(getenv(queryProfileEnv))
	if err != nil {
		return err
	}
	switch profile {
	case query.ProfileLocalLightweight, query.ProfileLocalAuthoritative, query.ProfileLocalFullStack:
		return nil
	default:
		return fmt.Errorf("%s=false requires an explicit local %s profile; production reducers must run generation retention", generationRetentionEnabledEnv, queryProfileEnv)
	}
}

// logGenerationRetentionHardCeilingLift logs once, at reducer startup, when
// the unset hard ceiling was raised above 2160h to match a longer soft window.
func logGenerationRetentionHardCeilingLift(ctx context.Context, logger *slog.Logger, cfg generationRetentionConfig) {
	if logger == nil || !cfg.HardMaxSupersededAgeLifted {
		return
	}
	logger.InfoContext(
		ctx,
		"generation retention hard ceiling raised to the soft keep window",
		slog.String("hard_env", generationRetentionHardMaxSupersededAgeEnv),
		slog.String("soft_env", generationRetentionMaxSupersededAgeEnv),
		slog.String("effective_hard_max_superseded_age", cfg.Runner.Policy.HardMaxSupersededAge.String()),
		slog.String("max_superseded_age", cfg.Runner.Policy.MaxSupersededAge.String()),
		slog.String("default_hard_max_superseded_age", postgres.DefaultGenerationRetentionPolicy().HardMaxSupersededAge.String()),
	)
}

type postgresGenerationRetentionPruner struct {
	store postgres.GenerationRetentionStore
}

// generationRetentionRunnerFor builds the retention runner, or nil when
// retention is disabled. logger receives the store's per-generation search
// index pre-delete lines (#7427); the caller sets the runner's own logger.
func generationRetentionRunnerFor(
	database db.ExecQueryer,
	cfg generationRetentionConfig,
	logger *slog.Logger,
) *retention.Runner {
	if !cfg.Enabled {
		return nil
	}
	runner := &retention.Runner{
		Pruner: postgresGenerationRetentionPruner{
			store: postgres.NewGenerationRetentionStore(database).WithLogger(logger),
		},
		Config: cfg.Runner,
	}
	// The search index orphan sweep (#7427 PR2) runs its batches in their own
	// transactions, so it needs a database that can begin them.
	if sweepDatabase, ok := database.(indexretention.Database); ok {
		runner.OrphanSweeper = searchIndexOrphanSweeper{database: sweepDatabase, logger: logger}
	}
	return runner
}

// searchIndexOrphanSweeper adapts the indexretention orphan census and sweep
// to the retention runner's contract.
type searchIndexOrphanSweeper struct {
	database indexretention.Database
	logger   *slog.Logger
}

// SweepSearchIndexOrphans runs one census and sweep, its batches bounded by
// deadline from now (none when deadline is not positive) and its rows by
// rowCap, the policy's search index row cap (the sweep's 2,000,000-row
// default when not positive).
func (s searchIndexOrphanSweeper) SweepSearchIndexOrphans(
	ctx context.Context,
	deadline time.Duration,
	rowCap int64,
) (retention.OrphanSweepResult, error) {
	var until time.Time
	if deadline > 0 {
		until = time.Now().Add(deadline)
	}
	sweep, err := indexretention.SweepOrphans(ctx, s.database, until, rowCap, s.logger)
	return retention.OrphanSweepResult{
		PairsScanned:      sweep.Census.PairsScanned,
		OrphanGenerations: sweep.Census.OrphanGenerations,
		RowsSwept:         sweep.RowsSwept,
		Batches:           sweep.Batches,
		Unswept:           sweep.Unswept,
		CensusDuration:    sweep.CensusDuration,
		SweepDuration:     sweep.SweepDuration,
	}, err
}

func (p postgresGenerationRetentionPruner) PruneSupersededGenerations(
	ctx context.Context,
	policy retention.Policy,
) (retention.Result, error) {
	result, err := p.store.PruneSupersededGenerations(ctx, postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: policy.MinSupersededGenerations,
		MaxSupersededAge:         policy.MaxSupersededAge,
		HardMaxSupersededAge:     policy.HardMaxSupersededAge,
		BatchGenerationLimit:     policy.BatchGenerationLimit,
		BatchRowLimit:            policy.BatchRowLimit,
		PolicyScope:              policy.PolicyScope,
		PolicyRevision:           policy.PolicyRevision,
		// One row cap bounds the pre-delete and the orphan sweep (#8281
		// review); unset keeps the store default. The deadline follows the
		// runner's poll interval (#7427).
		SearchIndexPredeleteRowCap:   policy.SearchIndexPredeleteRowCap,
		SearchIndexPredeleteDeadline: policy.SearchIndexPredeleteDeadline,
	})
	if errors.Is(err, postgres.ErrGenerationRetentionKeyIndexUnavailable) {
		// Carry the storage refusal into the runner's contract so it reports
		// failure reason key_index_unavailable, not a generic store error.
		return retention.Result{}, fmt.Errorf("%w: %w",
			retention.ErrKeyIndexUnavailable, err)
	}
	if err != nil {
		return retention.Result{}, err
	}
	return retention.Result{
		GenerationsPruned:  result.GenerationsPruned,
		RowsPruned:         result.RowsPruned,
		Skipped:            result.Skipped,
		OldestEligibleAge:  result.OldestEligibleAge,
		Duration:           result.Duration,
		PhaseDurations:     result.PhaseDurations,
		ScopeLockHold:      result.ScopeLockHold,
		LedgerRowsPruned:   result.LedgerRowsPruned,
		LedgerRowsCounted:  result.LedgerRowsCounted,
		RowsOverLimit:      result.RowsOverLimit,
		LockedScopeRows:    result.LockedScopeRows,
		SearchIndexBatches: result.SearchIndexBatches,
	}, nil
}

// Changed-since link domain knobs (#7127 PR-3a). The domain is dark: it is
// off by default, and with the switch off it issues no SQL.
const (
	changedSinceLinkEnabledEnv          = linkstore.EnabledEnv
	changedSinceLinkSlotsEnv            = "ESHU_CHANGED_SINCE_LINK_SLOTS"
	changedSinceLinkStatementTimeoutEnv = "ESHU_CHANGED_SINCE_LINK_STATEMENT_TIMEOUT"
	changedSinceLinkPollIntervalEnv     = "ESHU_CHANGED_SINCE_LINK_POLL_INTERVAL"
	changedSinceLinkWorkersEnv          = "ESHU_CHANGED_SINCE_LINK_WORKERS"
	changedSinceLinkBackfillScopesEnv   = "ESHU_CHANGED_SINCE_LINK_BACKFILL_SCOPES_PER_CYCLE"
	changedSinceLinkMaxAttemptsEnv      = "ESHU_CHANGED_SINCE_LINK_MAX_ATTEMPTS"
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
		// One parse for every binary: the ingester and bootstrap-index read
		// the same switch for the Ack journal (#7127 PR-3b).
		Enabled:          linkstore.Enabled(getenv),
		Slots:            loadPositiveIntOrDefault(getenv, changedSinceLinkSlotsEnv, linkstore.DefaultSlots),
		StatementTimeout: loadDurationOrDefault(getenv, changedSinceLinkStatementTimeoutEnv, linkstore.DefaultStatementTimeout),
		Runner: links.Config{
			PollInterval:           loadDurationOrDefault(getenv, changedSinceLinkPollIntervalEnv, links.DefaultPollInterval),
			Workers:                loadPositiveIntOrDefault(getenv, changedSinceLinkWorkersEnv, links.DefaultWorkers),
			BackfillScopesPerCycle: loadPositiveIntOrDefault(getenv, changedSinceLinkBackfillScopesEnv, links.DefaultBackfillScopesPerCycle),
			MaxAttempts:            loadPositiveIntOrDefault(getenv, changedSinceLinkMaxAttemptsEnv, linkstore.DefaultMaxAttempts),
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
