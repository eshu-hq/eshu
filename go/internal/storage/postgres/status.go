// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
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
}

// NewStatusStore constructs a read-only status store.
func NewStatusStore(queryer db.Queryer) StatusStore {
	return StatusStore{queryer: queryer}
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
	if s.queryer == nil {
		return statuspkg.RawSnapshot{}, fmt.Errorf("queryer is required")
	}

	scopeCounts, err := listNamedCounts(ctx, s.read(statusReadScopeCounts), scopeCountsQuery, "list scope counts")
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	generationCounts, err := listNamedCounts(ctx, s.read(statusReadGenerationCounts), generationCountsQuery, "list generation counts")
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	scopeActivity := scopeActivityFromCounts(scopeCounts, generationCounts)
	generationHistory := generationHistoryFromCounts(generationCounts)
	generationTransitions, err := listGenerationTransitions(ctx, s.read(statusReadGenerationTransitions))
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	// Stage counts, domain backlog, queue snapshot, conflict blockages, and the
	// latest queue failure all come from one evaluation of
	// active_fact_work_items in a single round trip (#6794).
	activeWork, err := readActiveWorkSummary(ctx, s.read(statusReadActiveWorkSummary), asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	stageCounts := activeWork.StageCounts
	producerActivity, err := readProducerActivitySnapshot(ctx, s.read(statusReadProducerActivity), asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	collectorGenerationDeadLetters, err := readCollectorGenerationDeadLetterSnapshot(ctx, s.read(statusReadCollectorGenerationDeadLetters), asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	coordinatorSnapshot, err := readCoordinatorSnapshot(ctx, s.read(statusReadCoordinator), asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	var registryCollectors []statuspkg.RegistryCollectorSnapshot
	if selection.IncludeRegistryCollectors {
		registryCollectors, err = readRegistryCollectorSnapshots(ctx, s.read(statusReadRegistryCollectors), asOf.UTC())
		if err != nil {
			return statuspkg.RawSnapshot{}, err
		}
	}
	awsCloudScans, awsCloudScansTruncated, err := readAWSCloudScanStatuses(ctx, s.read(statusReadAWSCloudScans))
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	awsFreshness, err := readAWSFreshnessSnapshot(ctx, s.read(statusReadAWSFreshness), asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	vulnerabilitySources, err := readVulnerabilitySourceStates(ctx, s.read(statusReadVulnerabilitySources))
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	var collectorFactEvidence []statuspkg.CollectorFactEvidence
	if selection.IncludeCollectorFactEvidence {
		collectorFactEvidence, err = readCollectorFactEvidence(ctx, s.read(statusReadCollectorFactEvidence))
		if err != nil {
			return statuspkg.RawSnapshot{}, err
		}
	}
	terraformStateEvidence, err := readTerraformStateAdminEvidence(
		ctx,
		s.read(statusReadTerraformState),
		statuspkg.MaxTerraformStateRecentWarnings,
		asOf.UTC(),
	)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	semanticExtraction, err := readSemanticExtractionObservability(ctx, s.read(statusReadSemanticExtraction))
	if err != nil {
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
		VulnerabilitySources:           vulnerabilitySources,
		CollectorFactEvidence:          collectorFactEvidence,
		AWSCloudScansTruncated:         awsCloudScansTruncated,
		AWSCloudScanLimit:              awsCloudScanStatusLimit,
		TerraformStateLastSerials:      terraformStateEvidence.LastSerials,
		TerraformStateRecentWarnings:   terraformStateEvidence.RecentWarnings,
		SemanticExtraction:             semanticExtraction,
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

func durationFromSeconds(value float64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value * float64(time.Second))
}
