// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/semantic"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SQLQueryer adapts a *sql.DB into the status query surface.
type SQLQueryer struct {
	DB *sql.DB
}

// QueryContext implements Queryer against a sql.DB.
func (q SQLQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.DB.QueryContext(ctx, query, args...)
}

// StatusStore reads live operator status aggregates from the Wave 2 schema.
//
// Instruments is exported and left nil by NewStatusStore (see
// AWSPaginationCheckpointStore for the identical pattern) so the ~30 existing
// NewStatusStore call sites across go/cmd/* stay source-compatible; a caller
// that wants the per-read eshu_dp_status_snapshot_read_duration_seconds
// signal sets the field, or uses NewInstrumentedStatusStore. Recording is
// nil-safe.
type StatusStore struct {
	queryer     db.Queryer
	Instruments *telemetry.Instruments
	// summaryRead is the stored-summary reader setting (#7009), loaded from
	// the environment by NewStatusStore; summaryReadErr is its validation
	// error, returned by every snapshot read while the reader is on.
	summaryRead    summary.ReadConfig
	summaryReadErr error
	// liveFlight shares one live active-work statement per process among
	// concurrent reads that fell back from the stored summary.
	liveFlight *summary.Flight[activeWorkSummary]
}

// NewStatusStore constructs a read-only status store. It reads
// ESHU_STATUS_SUMMARY_READ_ENABLED and ESHU_STATUS_SUMMARY_STALE_AFTER, so
// every runtime that builds a status store honors the reader flag without a
// change at its call site; the flag is off by default.
func NewStatusStore(queryer db.Queryer) StatusStore {
	cfg, err := summary.LoadReadConfig(os.Getenv)
	return StatusStore{
		queryer: queryer, summaryRead: cfg, summaryReadErr: err,
		liveFlight: &summary.Flight[activeWorkSummary]{},
	}
}

// WithSummaryRead returns a copy of the store with an explicit stored-summary
// reader setting in place of the environment's.
func (s StatusStore) WithSummaryRead(cfg summary.ReadConfig) StatusStore {
	s.summaryRead, s.summaryReadErr = cfg, nil
	return s
}

// NewInstrumentedStatusStore constructs a read-only status store with the
// shared meter-provider Instruments already wired, so every status snapshot
// read records eshu_dp_status_snapshot_read_duration_seconds labeled by read
// (see status_read_telemetry.go) wherever the store backs operator status and
// metrics surfaces: the hosted runtimes (app.NewHostedWithStatusServer /
// runtime.NewStatusAdminServer) and the API/MCP path wired by cmd/api and
// cmd/mcp-server's newStatusStore. instruments may be nil; recording is a no-op
// in that case.
func NewInstrumentedStatusStore(queryer db.Queryer, instruments *telemetry.Instruments) StatusStore {
	store := NewStatusStore(queryer)
	store.Instruments = instruments
	return store
}

// ReadRawSnapshot returns the raw aggregate snapshot needed by the operator
// status surface.
func (s StatusStore) ReadRawSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return s.ReadStatusSnapshot(ctx, asOf)
}

// ReadStatusSnapshot returns the full raw aggregate snapshot needed by the
// shared operator status surface. It delegates to ReadStatusSnapshotFiltered
// with FullSnapshotSelection() to preserve back-compatible behavior.
func (s StatusStore) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return s.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

// ReadStatusSnapshotFiltered returns the raw aggregate snapshot, gathering only
// the optional sections requested by the selection. When the selection excludes
// collector fact evidence or registry collectors, it skips the corresponding
// fact_records aggregate queries and leaves those snapshot fields empty.
func (s StatusStore) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	selection statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	if err := selection.Validate(); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	if s.queryer == nil {
		return statuspkg.RawSnapshot{}, fmt.Errorf("queryer is required")
	}
	if selection.Mode == statuspkg.SnapshotModeSemanticOnly {
		q, done := s.read(ctx, statusReadSemanticExtraction)
		semanticExtraction, err := semanticstore.ReadSemanticExtractionObservability(ctx, q)
		if err = done(err); err != nil {
			return statuspkg.RawSnapshot{}, err
		}
		return statuspkg.RawSnapshot{AsOf: asOf.UTC(), SemanticExtraction: semanticExtraction}, nil
	}
	// Each read is labeled and timed; done records its outcome from the
	// reader's own error, so scan and decode failures count (#6794).
	var (
		q    db.Queryer
		done func(error) error
	)

	q, done = s.read(ctx, statusReadScopeCounts)
	scopeCounts, err := listNamedCounts(ctx, q, scopeCountsQuery, "list scope counts")
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadGenerationCounts)
	generationCounts, err := listNamedCounts(ctx, q, generationCountsQuery, "list generation counts")
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	scopeActivity := scopeActivityFromCounts(scopeCounts, generationCounts)
	generationHistory := generationHistoryFromCounts(generationCounts)
	q, done = s.read(ctx, statusReadGenerationTransitions)
	generationTransitions, err := listGenerationTransitions(ctx, q)
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	// Stage counts, domain backlog, queue snapshot, conflict blockages, and the
	// latest queue failure all come from one evaluation of
	// active_fact_work_items in a single round trip (#6794).
	activeWork, activeWorkSource, err := s.readActiveWork(ctx, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	recordActiveWorkSummaryMode(ctx, activeWork)
	stageCounts := activeWork.StageCounts
	q, done = s.read(ctx, statusReadProducerActivity)
	producerActivity, err := readProducerActivitySnapshot(ctx, q, asOf.UTC())
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadCollectorGenerationDeadLetters)
	collectorGenerationDeadLetters, err := readCollectorGenerationDeadLetterSnapshot(ctx, q, asOf.UTC())
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadCoordinator)
	coordinatorSnapshot, err := readCoordinatorSnapshot(ctx, q, asOf.UTC())
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	var registryCollectors []statuspkg.RegistryCollectorSnapshot
	if selection.IncludeRegistryCollectors {
		q, done = s.read(ctx, statusReadRegistryCollectors)
		registryCollectors, err = readRegistryCollectorSnapshots(ctx, q, asOf.UTC())
		if err = done(err); err != nil {
			return statuspkg.RawSnapshot{}, err
		}
	}
	q, done = s.read(ctx, statusReadAWSCloudScans)
	awsCloudScans, awsCloudScansTruncated, err := readAWSCloudScanStatuses(ctx, q)
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadAWSFreshness)
	awsFreshness, err := readAWSFreshnessSnapshot(ctx, q, asOf.UTC())
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadInfraInventory)
	infraInventory, err := readInfraInventoryStatus(ctx, q, asOf.UTC())
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	q, done = s.read(ctx, statusReadVulnerabilitySources)
	vulnerabilitySources, err := vulnerabilitystore.ReadVulnerabilitySourceStates(ctx, q)
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	var collectorFactEvidence []statuspkg.CollectorFactEvidence
	if selection.IncludeCollectorFactEvidence {
		q, done = s.read(ctx, statusReadCollectorFactEvidence)
		collectorFactEvidence, err = readCollectorFactEvidence(ctx, q)
		if err = done(err); err != nil {
			return statuspkg.RawSnapshot{}, err
		}
	}
	var terraformStateEvidence statestore.TerraformStateAdminEvidence
	if !selection.SkipTerraformStateEvidence {
		q, done = s.read(ctx, statusReadTerraformState)
		terraformStateEvidence, err = statestore.ReadTerraformStateAdminEvidence(
			ctx,
			q,
			statuspkg.MaxTerraformStateRecentWarnings,
			asOf.UTC(),
		)
		if err = done(err); err != nil {
			return statuspkg.RawSnapshot{}, err
		}
	}
	q, done = s.read(ctx, statusReadSemanticExtraction)
	semanticExtraction, err := semanticstore.ReadSemanticExtractionObservability(ctx, q)
	if err = done(err); err != nil {
		return statuspkg.RawSnapshot{}, err
	}

	return statuspkg.RawSnapshot{
		AsOf:                           asOf.UTC(),
		ScopeCounts:                    scopeCounts,
		ScopeActivity:                  scopeActivity,
		GenerationCounts:               generationCounts,
		GenerationHistory:              generationHistory,
		GenerationTransitions:          generationTransitions,
		StageCounts:                    stageCounts,
		DomainBacklogs:                 activeWork.DomainBacklogs,
		ProducerActivity:               producerActivity,
		QueueBlockages:                 activeWork.Blockages,
		Queue:                          activeWork.Queue,
		LatestQueueFailure:             activeWork.LatestFailure,
		CollectorGenerationDeadLetters: collectorGenerationDeadLetters,
		Coordinator:                    coordinatorSnapshot,
		RegistryCollectors:             registryCollectors,
		AWSCloudScans:                  awsCloudScans,
		AWSFreshness:                   awsFreshness,
		InfraInventory:                 infraInventory,
		VulnerabilitySources:           vulnerabilitySources,
		CollectorFactEvidence:          collectorFactEvidence,
		AWSCloudScansTruncated:         awsCloudScansTruncated,
		AWSCloudScanLimit:              awsCloudScanStatusLimit,
		TerraformStateLastSerials:      terraformStateEvidence.LastSerials,
		TerraformStateRecentWarnings:   terraformStateEvidence.RecentWarnings,
		SemanticExtraction:             semanticExtraction,
		ActiveWorkSource:               activeWorkSource,
	}, nil
}

func scopeActivityFromCounts(scopeCounts []statuspkg.NamedCount, generationCounts []statuspkg.NamedCount) statuspkg.ScopeActivitySnapshot {
	activeScopes := namedCount(scopeCounts, "active")
	pendingGenerations := namedCount(generationCounts, "pending")
	if pendingGenerations > activeScopes {
		pendingGenerations = activeScopes
	}

	return statuspkg.ScopeActivitySnapshot{
		Active:    activeScopes,
		Changed:   pendingGenerations,
		Unchanged: scopeUnchangedCount(activeScopes, pendingGenerations),
	}
}

func scopeUnchangedCount(activeScopes int, changedScopes int) int {
	if activeScopes <= changedScopes {
		return 0
	}
	return activeScopes - changedScopes
}

func generationHistoryFromCounts(rows []statuspkg.NamedCount) statuspkg.GenerationHistorySnapshot {
	history := statuspkg.GenerationHistorySnapshot{
		Active:     namedCount(rows, "active"),
		Pending:    namedCount(rows, "pending"),
		Completed:  namedCount(rows, "completed"),
		Superseded: namedCount(rows, "superseded"),
		Failed:     namedCount(rows, "failed"),
	}
	known := map[string]struct{}{
		"active":     {},
		"pending":    {},
		"completed":  {},
		"superseded": {},
		"failed":     {},
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		history.Other += row.Count
	}

	return history
}

func namedCount(rows []statuspkg.NamedCount, name string) int {
	total := 0
	for _, row := range rows {
		if row.Name == name {
			total += row.Count
		}
	}

	return total
}

func listNamedCounts(
	ctx context.Context,
	queryer db.Queryer,
	query string,
	op string,
) ([]statuspkg.NamedCount, error) {
	rows, err := queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	counts := []statuspkg.NamedCount{}
	for rows.Next() {
		var name string
		var count int64
		if scanErr := rows.Scan(&name, &count); scanErr != nil {
			return nil, fmt.Errorf("%s: %w", op, scanErr)
		}
		counts = append(counts, statuspkg.NamedCount{
			Name:  name,
			Count: int(count),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return counts, nil
}

func listGenerationTransitions(
	ctx context.Context,
	queryer db.Queryer,
) ([]statuspkg.GenerationTransitionSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, generationTransitionsQuery)
	if err != nil {
		return nil, fmt.Errorf("list generation transitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	transitions := []statuspkg.GenerationTransitionSnapshot{}
	for rows.Next() {
		var row statuspkg.GenerationTransitionSnapshot
		var freshnessHint string
		var observedAt time.Time
		var activatedAt sql.NullTime
		var supersededAt sql.NullTime
		if scanErr := rows.Scan(
			&row.ScopeID,
			&row.GenerationID,
			&row.Status,
			&row.TriggerKind,
			&freshnessHint,
			&observedAt,
			&activatedAt,
			&supersededAt,
			&row.CurrentActiveGenerationID,
		); scanErr != nil {
			return nil, fmt.Errorf("list generation transitions: %w", scanErr)
		}
		row.FreshnessHint = strings.TrimSpace(freshnessHint)
		row.ObservedAt = observedAt.UTC()
		if activatedAt.Valid {
			row.ActivatedAt = activatedAt.Time.UTC()
		}
		if supersededAt.Valid {
			row.SupersededAt = supersededAt.Time.UTC()
		}
		row.CurrentActiveGenerationID = strings.TrimSpace(row.CurrentActiveGenerationID)
		transitions = append(transitions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list generation transitions: %w", err)
	}

	return transitions, nil
}

// readInfraInventoryStatus reports the infra read model's state for the admin
// status surface: one query for the backfill marker and the rolling-upgrade
// fence marks (inventory.ReadFenceState).
func readInfraInventoryStatus(ctx context.Context, queryer db.Queryer, asOf time.Time) (statuspkg.InfraInventorySnapshot, error) {
	state, err := inventory.ReadFenceState(ctx, queryer, asOf)
	if err != nil {
		return statuspkg.InfraInventorySnapshot{}, err
	}
	return statuspkg.InfraInventorySnapshot{
		Reported:       true,
		State:          state.ReadModelState(),
		MarkerPresent:  state.MarkerPresent,
		DirtyRepos:     state.DirtyRepos,
		OldestDirtyAge: state.OldestDirtyAge,
	}, nil
}

// ActiveWorkSummarySourceSHA256 returns the hex SHA-256 of the exact
// active-work summary statement text this binary runs. The periodic status
// summary writer stores it with every model row and the summary reader
// compares it with its own value: a mismatch means the row came from another
// statement (a rolling upgrade), so the reader falls back to the live
// statement instead of decoding rows it did not produce (#7009).
func ActiveWorkSummarySourceSHA256() string {
	digest := sha256.Sum256([]byte(activeWorkSummaryQuery))
	return hex.EncodeToString(digest[:])
}

// ReadActiveWorkSummaryEntries runs the active-work summary statement, byte
// for byte the one the live status read runs, with asOf as $1, and returns
// its rows in statement order as status summary entries for the read model
// writer (#7009). Every row is first decoded with the live read's own
// decoder, so a row the reader could not decode is returned as an error and
// never stored. An empty result is an empty, non-nil slice.
func ReadActiveWorkSummaryEntries(ctx context.Context, queryer db.Queryer, asOf time.Time) ([]summary.Entry, error) {
	rows, err := queryer.QueryContext(ctx, activeWorkSummaryQuery, asOf.UTC())
	if err != nil {
		return nil, fmt.Errorf("read active work summary entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var decoded activeWorkSummary
	entries := []summary.Entry{}
	for rows.Next() {
		var entry summary.Entry
		if err := rows.Scan(&entry.Section, &entry.Ordinal, &entry.JSON); err != nil {
			return nil, fmt.Errorf("read active work summary entries: %w", err)
		}
		if err := decoded.add(entry.Section, entry.JSON); err != nil {
			return nil, fmt.Errorf("read active work summary entries %s row %d: %w", entry.Section, entry.Ordinal, err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active work summary entries: %w", err)
	}
	return entries, nil
}
