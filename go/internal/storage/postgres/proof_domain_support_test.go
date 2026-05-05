package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type proofRows struct {
	rows  [][]any
	index int
}

func newProofRows(rows [][]any) *proofRows {
	return &proofRows{rows: rows}
}

func (r *proofRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *proofRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			switch value := row[i].(type) {
			case nil:
				*target = sql.NullTime{}
			case time.Time:
				*target = sql.NullTime{Time: value.UTC(), Valid: true}
			default:
				return fmt.Errorf("row[%d] type = %T, want time.Time or nil", i, row[i])
			}
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int", i, row[i])
			}
			*target = value
		case *float64:
			value, ok := row[i].(float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want float64", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}

	r.index++
	return nil
}

func (r *proofRows) Err() error { return nil }

func (r *proofRows) Close() error { return nil }

type proofResult struct{}

func (proofResult) LastInsertId() (int64, error) { return 0, nil }
func (proofResult) RowsAffected() (int64, error) { return 1, nil }

func cloneScopes(input map[string]scope.IngestionScope) map[string]scope.IngestionScope {
	cloned := make(map[string]scope.IngestionScope, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneStrings(input map[string]string) map[string]string {
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneGenerations(input map[string]scope.ScopeGeneration) map[string]scope.ScopeGeneration {
	cloned := make(map[string]scope.ScopeGeneration, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneFacts(input map[string]facts.Envelope) map[string]facts.Envelope {
	cloned := make(map[string]facts.Envelope, len(input))
	for key, value := range input {
		cloned[key] = value.Clone()
	}
	return cloned
}

func cloneWorkItems(input map[string]proofWorkItem) map[string]proofWorkItem {
	cloned := make(map[string]proofWorkItem, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func mustMarshalProofMetadata(metadata map[string]string) []byte {
	if len(metadata) == 0 {
		return []byte("{}")
	}

	payload, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return payload
}

func proofScopeCountRows(scopeStatuses map[string]string) [][]any {
	counts := make(map[string]int64)
	for _, status := range scopeStatuses {
		counts[status]++
	}
	return namedCountRows(counts)
}

func proofGenerationCountRows(generations map[string]scope.ScopeGeneration) [][]any {
	counts := make(map[string]int64)
	for _, generation := range generations {
		counts[string(generation.Status)]++
	}
	return namedCountRows(counts)
}

func proofGenerationTransitionRows(
	generations map[string]scope.ScopeGeneration,
	activeGenerations map[string]string,
	asOf time.Time,
) [][]any {
	rows := make([][]any, 0, len(generations))
	for _, generation := range generations {
		if generation.Status != scope.GenerationStatusActive &&
			generation.Status != scope.GenerationStatusSuperseded {
			continue
		}

		var activatedAt any = generation.IngestedAt.UTC()
		var supersededAt any
		if generation.Status == scope.GenerationStatusSuperseded {
			supersededAt = asOf.UTC()
		}

		rows = append(rows, []any{
			generation.ScopeID,
			generation.GenerationID,
			string(generation.Status),
			string(generation.TriggerKind),
			generation.FreshnessHint,
			generation.ObservedAt.UTC(),
			activatedAt,
			supersededAt,
			activeGenerations[generation.ScopeID],
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		leftGenerationID, _ := rows[i][1].(string)
		rightGenerationID, _ := rows[j][1].(string)
		return leftGenerationID < rightGenerationID
	})

	return rows
}

func proofStageCountRows(workItems map[string]proofWorkItem) [][]any {
	type stageKey struct {
		stage  string
		status string
	}

	counts := make(map[stageKey]int64)
	for _, item := range workItems {
		counts[stageKey{stage: item.stage, status: item.status}]++
	}

	keys := make([]stageKey, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].stage != keys[j].stage {
			return keys[i].stage < keys[j].stage
		}
		return keys[i].status < keys[j].status
	})

	rows := make([][]any, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, []any{key.stage, key.status, counts[key]})
	}
	return rows
}

func proofDomainBacklogRows(
	workItems map[string]proofWorkItem,
	asOf time.Time,
) [][]any {
	type domainAggregate struct {
		outstanding int64
		retrying    int64
		deadLetter  int64
		failed      int64
		oldest      time.Time
	}

	aggregates := make(map[string]*domainAggregate)
	for _, item := range workItems {
		aggregate := aggregates[item.domain]
		if aggregate == nil {
			aggregate = &domainAggregate{}
			aggregates[item.domain] = aggregate
		}
		switch item.status {
		case "pending", "claimed", "running", "retrying":
			aggregate.outstanding++
			if aggregate.oldest.IsZero() || item.createdAt.Before(aggregate.oldest) {
				aggregate.oldest = item.createdAt
			}
		}
		if item.status == "retrying" {
			aggregate.retrying++
		}
		if item.status == "dead_letter" {
			aggregate.deadLetter++
		}
		if item.status == "failed" {
			aggregate.failed++
		}
	}

	domains := make([]string, 0, len(aggregates))
	for domain, aggregate := range aggregates {
		if aggregate.outstanding == 0 && aggregate.retrying == 0 &&
			aggregate.deadLetter == 0 && aggregate.failed == 0 {
			continue
		}
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	rows := make([][]any, 0, len(domains))
	for _, domain := range domains {
		aggregate := aggregates[domain]
		oldestAgeSeconds := 0.0
		if !aggregate.oldest.IsZero() {
			oldestAgeSeconds = asOf.UTC().Sub(aggregate.oldest.UTC()).Seconds()
		}
		rows = append(rows, []any{
			domain,
			aggregate.outstanding,
			aggregate.retrying,
			aggregate.deadLetter,
			aggregate.failed,
			oldestAgeSeconds,
		})
	}
	return rows
}

func proofQueueSnapshotRow(
	workItems map[string]proofWorkItem,
	asOf time.Time,
) []any {
	var totalCount int64
	var outstandingCount int64
	var pendingCount int64
	var inFlightCount int64
	var retryingCount int64
	var succeededCount int64
	var deadLetterCount int64
	var failedCount int64
	var overdueClaimCount int64
	var oldestOutstanding time.Time

	for _, item := range workItems {
		totalCount++
		switch item.status {
		case "pending":
			pendingCount++
			outstandingCount++
		case "claimed", "running":
			inFlightCount++
			outstandingCount++
			if !item.claimUntil.IsZero() && item.claimUntil.Before(asOf) {
				overdueClaimCount++
			}
		case "retrying":
			retryingCount++
			outstandingCount++
		case "succeeded":
			succeededCount++
		case "dead_letter":
			deadLetterCount++
		case "failed":
			failedCount++
		}

		switch item.status {
		case "pending", "claimed", "running", "retrying":
			if oldestOutstanding.IsZero() || item.createdAt.Before(oldestOutstanding) {
				oldestOutstanding = item.createdAt
			}
		}
	}

	oldestOutstandingAgeSeconds := 0.0
	if !oldestOutstanding.IsZero() {
		oldestOutstandingAgeSeconds = asOf.UTC().Sub(oldestOutstanding.UTC()).Seconds()
	}

	return []any{
		totalCount,
		outstandingCount,
		pendingCount,
		inFlightCount,
		retryingCount,
		succeededCount,
		deadLetterCount,
		failedCount,
		oldestOutstandingAgeSeconds,
		overdueClaimCount,
	}
}

func namedCountRows(counts map[string]int64) [][]any {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([][]any, 0, len(names))
	for _, name := range names {
		rows = append(rows, []any{name, counts[name]})
	}
	return rows
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func runProofProjectorCycle(t *testing.T, db *proofDomainDB, now time.Time) {
	t.Helper()
	runProofProjectorCycleWithInjector(t, db, now, nil)
}

func runProofProjectorCycleWithInjector(
	t *testing.T,
	db *proofDomainDB,
	now time.Time,
	retryInjector projector.RetryInjector,
) {
	t.Helper()
	runProofProjectorCycleWithWriters(
		t,
		db,
		now,
		retryInjector,
		&recordingCanonicalWriter{},
		&recordingContentWriter{},
	)
}

func runProofProjectorCycleWithWriters(
	t *testing.T,
	db *proofDomainDB,
	now time.Time,
	retryInjector projector.RetryInjector,
	canonicalWriter *recordingCanonicalWriter,
	contentWriter *recordingContentWriter,
) {
	t.Helper()

	if canonicalWriter == nil {
		canonicalWriter = &recordingCanonicalWriter{}
	}
	if contentWriter == nil {
		contentWriter = &recordingContentWriter{}
	}

	projectorQueue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
		Now:           func() time.Time { return now },
	}
	projectorService := projector.Service{
		PollInterval: time.Millisecond,
		WorkSource:   projectorQueue,
		FactStore:    NewFactStore(db),
		Runner: projector.Runtime{
			CanonicalWriter: canonicalWriter,
			ContentWriter:   contentWriter,
			IntentWriter:    ReducerQueue{db: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute, Now: func() time.Time { return now }},
			RetryInjector:   retryInjector,
		},
		WorkSink: projectorQueue,
		Wait:     func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := projectorService.Run(context.Background()); err != nil {
		t.Fatalf("projector service Run() error = %v, want nil", err)
	}
}

func proofRepositoryFacts(
	scopeID string,
	generationID string,
	factID string,
	digest string,
	observedAt time.Time,
) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:        factID,
			ScopeID:       scopeID,
			GenerationID:  generationID,
			FactKind:      "repository",
			StableFactKey: "repository:" + factID,
			ObservedAt:    observedAt,
			Payload: map[string]any{
				"graph_id":   "repo-123",
				"graph_kind": "repository",
				"name":       "eshu",
				"digest":     digest,
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      factID,
			},
		},
	}
}
