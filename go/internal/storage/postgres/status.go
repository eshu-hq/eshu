package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// Rows is the small read-only row cursor surface used by the status reader.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// Queryer is the small read-only SQL adapter needed by the status reader.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (Rows, error)
}

// SQLQueryer adapts a *sql.DB into the status query surface.
type SQLQueryer struct {
	DB *sql.DB
}

// QueryContext implements Queryer against a sql.DB.
func (q SQLQueryer) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return q.DB.QueryContext(ctx, query, args...)
}

// StatusStore reads live operator status aggregates from the Wave 2 schema.
type StatusStore struct {
	queryer Queryer
}

// NewStatusStore constructs a read-only status store.
func NewStatusStore(queryer Queryer) StatusStore {
	return StatusStore{queryer: queryer}
}

// ReadRawSnapshot returns the raw aggregate snapshot needed by the operator
// status surface.
func (s StatusStore) ReadRawSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return s.ReadStatusSnapshot(ctx, asOf)
}

// ReadStatusSnapshot returns the raw aggregate snapshot needed by the shared
// operator status surface.
func (s StatusStore) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	if s.queryer == nil {
		return statuspkg.RawSnapshot{}, fmt.Errorf("queryer is required")
	}

	scopeCounts, err := listNamedCounts(ctx, s.queryer, scopeCountsQuery, "list scope counts")
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	generationCounts, err := listNamedCounts(ctx, s.queryer, generationCountsQuery, "list generation counts")
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	scopeActivity := scopeActivityFromCounts(scopeCounts, generationCounts)
	generationHistory := generationHistoryFromCounts(generationCounts)
	generationTransitions, err := listGenerationTransitions(ctx, s.queryer)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	stageCounts, err := listStageCounts(ctx, s.queryer)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	domainBacklogs, err := listDomainBacklogs(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	queueSnapshot, err := readQueueSnapshot(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	queueBlockages, err := listReducerConflictBlockages(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	latestQueueFailure, err := readLatestQueueFailure(ctx, s.queryer)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	coordinatorSnapshot, err := readCoordinatorSnapshot(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	registryCollectors, err := readRegistryCollectorSnapshots(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	awsCloudScans, awsCloudScansTruncated, err := readAWSCloudScanStatuses(ctx, s.queryer)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	awsFreshness, err := readAWSFreshnessSnapshot(ctx, s.queryer, asOf.UTC())
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	terraformStateEvidence, err := readTerraformStateAdminEvidence(
		ctx,
		s.queryer,
		statuspkg.MaxTerraformStateRecentWarnings,
		asOf.UTC(),
	)
	if err != nil {
		return statuspkg.RawSnapshot{}, err
	}

	return statuspkg.RawSnapshot{
		AsOf:                         asOf.UTC(),
		ScopeCounts:                  scopeCounts,
		ScopeActivity:                scopeActivity,
		GenerationCounts:             generationCounts,
		GenerationHistory:            generationHistory,
		GenerationTransitions:        generationTransitions,
		StageCounts:                  stageCounts,
		DomainBacklogs:               domainBacklogs,
		QueueBlockages:               queueBlockages,
		Queue:                        queueSnapshot,
		LatestQueueFailure:           latestQueueFailure,
		Coordinator:                  coordinatorSnapshot,
		RegistryCollectors:           registryCollectors,
		AWSCloudScans:                awsCloudScans,
		AWSFreshness:                 awsFreshness,
		AWSCloudScansTruncated:       awsCloudScansTruncated,
		AWSCloudScanLimit:            awsCloudScanStatusLimit,
		TerraformStateLastSerials:    terraformStateEvidence.LastSerials,
		TerraformStateRecentWarnings: terraformStateEvidence.RecentWarnings,
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
	queryer Queryer,
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

func listStageCounts(ctx context.Context, queryer Queryer) ([]statuspkg.StageStatusCount, error) {
	rows, err := queryer.QueryContext(ctx, stageCountsQuery)
	if err != nil {
		return nil, fmt.Errorf("list stage counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := []statuspkg.StageStatusCount{}
	for rows.Next() {
		var stage string
		var state string
		var count int64
		if scanErr := rows.Scan(&stage, &state, &count); scanErr != nil {
			return nil, fmt.Errorf("list stage counts: %w", scanErr)
		}
		counts = append(counts, statuspkg.StageStatusCount{
			Stage:  stage,
			Status: state,
			Count:  int(count),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stage counts: %w", err)
	}

	return counts, nil
}

func listGenerationTransitions(
	ctx context.Context,
	queryer Queryer,
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

func listDomainBacklogs(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) ([]statuspkg.DomainBacklog, error) {
	rows, err := queryer.QueryContext(ctx, domainBacklogQuery, asOf)
	if err != nil {
		return nil, fmt.Errorf("list domain backlogs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	backlogs := []statuspkg.DomainBacklog{}
	for rows.Next() {
		var domain string
		var outstandingCount int64
		var inFlightCount int64
		var retryingCount int64
		var deadLetterCount int64
		var failedCount int64
		var oldestOutstandingAgeSeconds float64
		if scanErr := rows.Scan(
			&domain,
			&outstandingCount,
			&inFlightCount,
			&retryingCount,
			&deadLetterCount,
			&failedCount,
			&oldestOutstandingAgeSeconds,
		); scanErr != nil {
			return nil, fmt.Errorf("list domain backlogs: %w", scanErr)
		}
		backlogs = append(backlogs, statuspkg.DomainBacklog{
			Domain:      domain,
			Outstanding: int(outstandingCount),
			InFlight:    int(inFlightCount),
			Retrying:    int(retryingCount),
			DeadLetter:  int(deadLetterCount),
			Failed:      int(failedCount),
			OldestAge:   durationFromSeconds(oldestOutstandingAgeSeconds),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list domain backlogs: %w", err)
	}

	return backlogs, nil
}

func readQueueSnapshot(
	ctx context.Context,
	queryer Queryer,
	asOf time.Time,
) (statuspkg.QueueSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, queueSnapshotQuery, asOf)
	if err != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
		}
		return statuspkg.QueueSnapshot{}, nil
	}

	var totalCount int64
	var outstandingCount int64
	var pendingCount int64
	var inFlightCount int64
	var retryingCount int64
	var succeededCount int64
	var deadLetterCount int64
	var failedCount int64
	var oldestOutstandingAgeSeconds float64
	var overdueClaimCount int64
	if scanErr := rows.Scan(
		&totalCount,
		&outstandingCount,
		&pendingCount,
		&inFlightCount,
		&retryingCount,
		&succeededCount,
		&deadLetterCount,
		&failedCount,
		&oldestOutstandingAgeSeconds,
		&overdueClaimCount,
	); scanErr != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return statuspkg.QueueSnapshot{}, fmt.Errorf("read queue snapshot: %w", err)
	}

	return statuspkg.QueueSnapshot{
		Total:                int(totalCount),
		Outstanding:          int(outstandingCount),
		Pending:              int(pendingCount),
		InFlight:             int(inFlightCount),
		Retrying:             int(retryingCount),
		Succeeded:            int(succeededCount),
		DeadLetter:           int(deadLetterCount),
		Failed:               int(failedCount),
		OldestOutstandingAge: durationFromSeconds(oldestOutstandingAgeSeconds),
		OverdueClaims:        int(overdueClaimCount),
	}, nil
}

func durationFromSeconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}
