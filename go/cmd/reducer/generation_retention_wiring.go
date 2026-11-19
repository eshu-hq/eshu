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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linkstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Relationship retention knobs (#10823). The sweep deletes the evidence,
// candidate and generation rows of relationship generations nothing keeps
// alive. It is off by default: the read-only census and its orphan gauge run
// whenever the retention runner runs, and the default flips only after a
// recorded drain in a shared environment.
const (
	// generationRetentionRelationshipSweepEnabledEnv opts into the deletes. An
	// unset or unparsable value leaves them off.
	generationRetentionRelationshipSweepEnabledEnv = "ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ENABLED"
	// generationRetentionRelationshipSweepRowCapEnv bounds the rows one sweep
	// pass deletes. An unset, unparsable or non-positive value keeps the
	// 100,000-row default (about 170 MB of WAL per pass).
	generationRetentionRelationshipSweepRowCapEnv = "ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ROW_CAP"
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
				// Off by default (#8231): an unset or unparsable value leaves
				// the superseded sweep off and issuing no SQL.
				SearchIndexSupersededSweepEnabled: loadBoolOrDefault(getenv, generationRetentionSearchIndexSupersededSweepEnabledEnv, false),
				// Off by default (#10823): an unset or unparsable value leaves
				// the relationship sweep census-only, issuing no DELETE.
				RelationshipRetentionSweepEnabled: loadBoolOrDefault(getenv, generationRetentionRelationshipSweepEnabledEnv, false),
				RelationshipRetentionRowCap:       int64(loadPositiveIntOrDefault(getenv, generationRetentionRelationshipSweepRowCapEnv, int(relationshipretention.DefaultRowCap))),
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
	profile, err := querycontract.ParseQueryProfile(getenv(queryProfileEnv))
	if err != nil {
		return err
	}
	switch profile {
	case querycontract.ProfileLocalLightweight, querycontract.ProfileLocalAuthoritative, querycontract.ProfileLocalFullStack:
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
// index pre-delete lines (#7427) and the relationship sweep's batch lines
// (#10823); tracer wraps each relationship sweep pass in one span (nil is
// allowed). The caller sets the runner's own logger.
func generationRetentionRunnerFor(
	database db.ExecQueryer,
	cfg generationRetentionConfig,
	logger *slog.Logger,
	tracer trace.Tracer,
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
	// The search index orphan sweep (#7427 PR2) and the superseded sweep
	// (#8231) run their batches in their own transactions, so they need a
	// database that can begin them.
	if sweepDatabase, ok := database.(indexretention.Database); ok {
		runner.OrphanSweeper = searchIndexOrphanSweeper{database: sweepDatabase, logger: logger}
		runner.SupersededSweeper = searchIndexSupersededSweeper{database: sweepDatabase, logger: logger}
		runner.PartitionSizes = searchIndexPartitionSizesCensuser{database: sweepDatabase}
	}
	// The relationship sweep (#10823) likewise opens its own batch
	// transactions, after the prune transaction has ended.
	if sweepDatabase, ok := database.(db.Beginner); ok {
		runner.RelationshipSweeper = relationshipRetentionSweeper{database: sweepDatabase, logger: logger, tracer: tracer}
	}
	return runner
}

// relationshipRetentionSweeper adapts the relationship retention census and
// sweep to the retention runner's contract (#10823).
type relationshipRetentionSweeper struct {
	database db.Beginner
	logger   *slog.Logger
	tracer   trace.Tracer
}

// SweepRelationships runs one census and, when the request enables deletes,
// one bounded sweep: batches stop when the pass has deleted RowCap rows (the
// leaf's 100,000-row default when not positive) or Deadline has passed from
// now (no deadline when not positive). With Deletes false it takes the
// read-only census only and sends no DELETE. A census that succeeded before a
// batch failed still returns its tables with the error.
func (s relationshipRetentionSweeper) SweepRelationships(
	ctx context.Context,
	request retention.RelationshipSweepRequest,
) (retention.RelationshipSweepResult, error) {
	opts := relationshipretention.Options{
		Deletes: request.Deletes,
		RowCap:  request.RowCap,
		Logger:  s.logger,
		Tracer:  s.tracer,
	}
	if request.Deadline > 0 {
		opts.Deadline = time.Now().Add(request.Deadline)
	}
	sweep, err := relationshipretention.Sweep(ctx, s.database, opts)
	return retention.RelationshipSweepResult{
		OrphanGenerations: sweep.Census.ByTable,
		CensusSaturated:   sweep.Census.Saturated,
		RowsSwept:         sweep.RowsSwept,
		GenerationsSwept:  sweep.GenerationsSwept,
		Skipped:           sweep.Skipped,
		Batches:           sweep.Batches,
		Capped:            sweep.Capped,
		CensusDuration:    sweep.Census.Duration,
		SweepDuration:     sweep.SweepDuration,
	}, err
}

// searchIndexPartitionSizesCensuser adapts the indexretention
// per-partition size census to the retention runner's contract (#10874).
type searchIndexPartitionSizesCensuser struct {
	database indexretention.Database
}

// CensusSearchIndexPartitionSizes takes one catalog-only size pass over the
// terms leaves and classifies each leaf. The partition label is the closed
// p00-p63 set; scope and generation identifiers never reach metric labels.
func (s searchIndexPartitionSizesCensuser) CensusSearchIndexPartitionSizes(
	ctx context.Context,
) (retention.PartitionSizesResult, error) {
	started := time.Now()
	census, err := indexretention.TakePartitionSizeCensus(ctx, s.database)
	if err != nil {
		return retention.PartitionSizesResult{}, err
	}
	result := retention.PartitionSizesResult{CensusDuration: time.Since(started)}
	for _, leaf := range census {
		var suspect string
		switch indexretention.ClassifyPartitionSize(leaf) {
		case indexretention.PartitionSizeWarn:
			suspect = "warn"
		case indexretention.PartitionSizePage:
			suspect = "page"
		}
		result.Leaves = append(result.Leaves, retention.PartitionLeafSize{
			Partition:     searchIndexPartitionLabel(leaf.Leaf),
			Leaf:          leaf.Leaf,
			TotalBytes:    leaf.TotalBytes,
			HeapBytes:     leaf.HeapBytes,
			IndexBytes:    leaf.IndexBytes,
			EstimatedRows: leaf.EstimatedRows,
			BytesPerRow:   leaf.BytesPerRow(),
			Suspect:       suspect,
		})
	}
	return result, nil
}

// searchIndexPartitionLabel shortens a terms leaf regclass to its closed
// pNN label; anything unexpected keeps its full name rather than colliding
// with a real partition.
func searchIndexPartitionLabel(leaf string) string {
	const prefix = "eshu_search_index_terms_p"
	suffix := ""
	if len(leaf) == len(prefix)+2 && len(leaf) > len(prefix) && leaf[:len(prefix)] == prefix {
		suffix = leaf[len(prefix):]
	}
	if len(suffix) == 2 && suffix[0] >= '0' && suffix[0] <= '9' && suffix[1] >= '0' && suffix[1] <= '9' {
		return "p" + suffix
	}
	return leaf
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

// searchIndexSupersededSweeper adapts the indexretention superseded sweep to
// the retention runner's contract.
type searchIndexSupersededSweeper struct {
	database indexretention.Database
	logger   *slog.Logger
}

// SweepSearchIndexSuperseded runs one superseded listing and sweep, its
// batches bounded by deadline from now (none when deadline is not positive)
// and its rows by rowCap, the policy's search index row cap (the sweep's
// 2,000,000-row default when not positive).
func (s searchIndexSupersededSweeper) SweepSearchIndexSuperseded(
	ctx context.Context,
	deadline time.Duration,
	rowCap int64,
) (retention.SupersededSweepResult, error) {
	var until time.Time
	if deadline > 0 {
		until = time.Now().Add(deadline)
	}
	sweep, err := indexretention.SweepSuperseded(ctx, s.database, until, rowCap, s.logger)
	return retention.SupersededSweepResult{
		Candidates:    sweep.Candidates,
		RowsSwept:     sweep.RowsSwept,
		Batches:       sweep.Batches,
		Unswept:       sweep.Unswept,
		ListDuration:  sweep.ListDuration,
		SweepDuration: sweep.SweepDuration,
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
		ChunkPredeleteRowCap:         policy.ChunkPredeleteRowCap,
		ChunkPredeleteDeadline:       policy.ChunkPredeleteDeadline,
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
		GenerationsPruned:              result.GenerationsPruned,
		RowsPruned:                     result.RowsPruned,
		Skipped:                        result.Skipped,
		OldestEligibleAge:              result.OldestEligibleAge,
		Duration:                       result.Duration,
		PhaseDurations:                 result.PhaseDurations,
		ScopeLockHold:                  result.ScopeLockHold,
		LedgerRowsPruned:               result.LedgerRowsPruned,
		LedgerRowsCounted:              result.LedgerRowsCounted,
		FactKeyRowsPruned:              result.FactKeyRowsPruned,
		FactKeyRowsCounted:             result.FactKeyRowsCounted,
		RowsOverLimit:                  result.RowsOverLimit,
		LockedScopeRows:                result.LockedScopeRows,
		SearchIndexBatches:             result.SearchIndexBatches,
		ChunkPredeleteBatches:          result.ChunkPredeleteBatches,
		PredrainedOverLimitGenerations: result.PredrainedOverLimitGenerations,
		PredrainedSkippedRows:          result.PredrainedSkippedRows,
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
