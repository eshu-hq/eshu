// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestAcceptedGenerationPrefetchSingleQueryPerRound is the #7724 1A RED
// test: N distinct acceptance keys must cost at most ceil(N/1000) queries,
// not one Lookup per key. It fails on the per-key loop (N queries) and
// passes once the prefetch batches through LookupBatch.
func TestAcceptedGenerationPrefetchSingleQueryPerRound(t *testing.T) {
	t.Parallel()

	const distinct = 500
	database := newBatchAcceptanceTestDB()
	var intents []reducer.SharedProjectionIntentRow
	for i := 0; i < distinct; i++ {
		scope := fmt.Sprintf("scope-%d", i)
		unit := fmt.Sprintf("unit-%d", i)
		database.rows[acceptanceKey(scope, unit, "run-1")] = sharedProjectionAcceptanceRow{
			scopeID: scope, acceptanceUnitID: unit, sourceRunID: "run-1",
			generationID: fmt.Sprintf("gen-%d", i),
		}
		intents = append(intents,
			reducer.SharedProjectionIntentRow{ScopeID: scope, AcceptanceUnitID: unit, RepositoryID: unit, SourceRunID: "run-1", GenerationID: fmt.Sprintf("gen-%d", i)},
			// A duplicate row per key: dedupe must keep it from costing a query.
			reducer.SharedProjectionIntentRow{ScopeID: scope, AcceptanceUnitID: unit, RepositoryID: unit, SourceRunID: "run-1", GenerationID: fmt.Sprintf("gen-%d", i)},
		)
	}

	prefetch := NewAcceptedGenerationPrefetch(database)
	lookup, err := prefetch(context.Background(), intents)
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if database.queryCount != 1 {
		t.Fatalf("prefetch() issued %d queries for %d distinct keys, want 1", database.queryCount, distinct)
	}
	for i := 0; i < distinct; i++ {
		got, found := lookup(reducer.SharedProjectionAcceptanceKey{
			ScopeID: fmt.Sprintf("scope-%d", i), AcceptanceUnitID: fmt.Sprintf("unit-%d", i), SourceRunID: "run-1",
		})
		if !found || got != fmt.Sprintf("gen-%d", i) {
			t.Fatalf("lookup(key %d) = (%q, %v), want (gen-%d, true)", i, got, found, i)
		}
	}
}

// TestAcceptedGenerationPrefetchChunksAtOneThousand proves the batch bound:
// 2500 distinct keys cost exactly 3 queries of at most 1000 keys each.
func TestAcceptedGenerationPrefetchChunksAtOneThousand(t *testing.T) {
	t.Parallel()

	const distinct = 2500
	database := newBatchAcceptanceTestDB()
	var intents []reducer.SharedProjectionIntentRow
	for i := 0; i < distinct; i++ {
		scope := fmt.Sprintf("scope-%d", i)
		unit := fmt.Sprintf("unit-%d", i)
		database.rows[acceptanceKey(scope, unit, "run-1")] = sharedProjectionAcceptanceRow{
			scopeID: scope, acceptanceUnitID: unit, sourceRunID: "run-1", generationID: "gen",
		}
		intents = append(intents, reducer.SharedProjectionIntentRow{
			ScopeID: scope, AcceptanceUnitID: unit, RepositoryID: unit, SourceRunID: "run-1", GenerationID: "gen",
		})
	}

	prefetch := NewAcceptedGenerationPrefetch(database)
	if _, err := prefetch(context.Background(), intents); err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if database.queryCount != 3 {
		t.Fatalf("prefetch() issued %d queries for %d keys, want 3", database.queryCount, distinct)
	}
	if database.maxBatchKeys > 1000 {
		t.Fatalf("largest batch carried %d keys, want at most 1000", database.maxBatchKeys)
	}
}

// TestAcceptedGenerationPrefetchNormalizesWhitespace proves the #7724 1A
// TrimSpace contract: keys are normalized before the query (the store
// never sees padding) and the closure compares on normalized keys, so a
// padded lookup resolves the trimmed row.
func TestAcceptedGenerationPrefetchNormalizesWhitespace(t *testing.T) {
	t.Parallel()

	database := newBatchAcceptanceTestDB()
	database.rows[acceptanceKey("scope-a", "unit-a", "run-1")] = sharedProjectionAcceptanceRow{
		scopeID: "scope-a", acceptanceUnitID: "unit-a", sourceRunID: "run-1", generationID: "gen-1",
	}

	prefetch := NewAcceptedGenerationPrefetch(database)
	lookup, err := prefetch(context.Background(), []reducer.SharedProjectionIntentRow{{
		ScopeID: "  scope-a ", AcceptanceUnitID: " unit-a\t", RepositoryID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1",
	}})
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	for _, batch := range database.batches {
		for _, key := range batch {
			if key != strings.TrimSpace(key) {
				t.Fatalf("store received unnormalized key %q, want trimmed", key)
			}
		}
	}
	got, found := lookup(reducer.SharedProjectionAcceptanceKey{
		ScopeID: " scope-a", AcceptanceUnitID: "unit-a ", SourceRunID: "\trun-1\n",
	})
	if !found || got != "gen-1" {
		t.Fatalf("lookup(padded key) = (%q, %v), want (gen-1, true)", got, found)
	}
}

// TestAcceptedGenerationPrefetchMatchesPerKeyLookup is the #7724 1A
// differential (idempotency proof): over found, not-found, duplicate, and
// unkeyed fixtures the batch prefetch closure must agree exactly with the
// per-key store Lookup the old loop called. Identical DB state yields
// identical selection.
func TestAcceptedGenerationPrefetchMatchesPerKeyLookup(t *testing.T) {
	t.Parallel()

	database := newBatchAcceptanceTestDB()
	database.rows[acceptanceKey("scope-a", "unit-a", "run-1")] = sharedProjectionAcceptanceRow{
		scopeID: "scope-a", acceptanceUnitID: "unit-a", sourceRunID: "run-1", generationID: "gen-1",
	}
	database.rows[acceptanceKey("scope-b", "unit-b", "run-2")] = sharedProjectionAcceptanceRow{
		scopeID: "scope-b", acceptanceUnitID: "unit-b", sourceRunID: "run-2", generationID: "gen-9",
	}
	intents := []reducer.SharedProjectionIntentRow{
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		{ScopeID: "scope-missing", AcceptanceUnitID: "unit-missing", RepositoryID: "unit-missing", SourceRunID: "run-1", GenerationID: "gen-x"},
		{ScopeID: "", AcceptanceUnitID: "", RepositoryID: "", SourceRunID: "", GenerationID: "gen-x"},
	}

	ctx := context.Background()
	prefetch := NewAcceptedGenerationPrefetch(database)
	lookup, err := prefetch(ctx, intents)
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	store := NewSharedProjectionAcceptanceStore(database)
	probeKeys := []reducer.SharedProjectionAcceptanceKey{
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1"},
		{ScopeID: "scope-b", AcceptanceUnitID: "unit-b", SourceRunID: "run-1"},
		{ScopeID: "scope-b", AcceptanceUnitID: "unit-b", SourceRunID: "run-2"},
		{ScopeID: "scope-missing", AcceptanceUnitID: "unit-missing", SourceRunID: "run-1"},
		{ScopeID: "scope-never-queried", AcceptanceUnitID: "unit-z", SourceRunID: "run-9"},
	}
	for _, key := range probeKeys {
		wantGen, wantFound, err := store.Lookup(ctx, key.ScopeID, key.AcceptanceUnitID, key.SourceRunID)
		if err != nil {
			t.Fatalf("Lookup(%v) error = %v", key, err)
		}
		// The prefetch only resolves keys derived from its intents; keys
		// outside that set are answered not-found. Restrict the expected
		// answer to the prefetched set, mirroring the old loop exactly.
		prefetched := false
		for _, intent := range intents {
			if intentKey, ok := intent.AcceptanceKey(); ok && intentKey == key {
				prefetched = true
				break
			}
		}
		if !prefetched {
			wantFound = false
			wantGen = ""
		}
		gotGen, gotFound := lookup(key)
		if gotGen != wantGen || gotFound != wantFound {
			t.Fatalf("lookup(%v) = (%q, %v), want (%q, %v)", key, gotGen, gotFound, wantGen, wantFound)
		}
	}
}

// TestAcceptedGenerationPrefetchErrorFailsSelection proves a batch error
// fails the prefetch (and therefore the selection) instead of silently
// dropping or keeping rows.
func TestAcceptedGenerationPrefetchErrorFailsSelection(t *testing.T) {
	t.Parallel()

	prefetch := NewAcceptedGenerationPrefetch(&acceptanceStoreErrorDB{})
	lookup, err := prefetch(context.Background(), []reducer.SharedProjectionIntentRow{{
		ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1",
	}})
	if err == nil {
		t.Fatal("prefetch() error = nil on store failure, want error")
	}
	if lookup != nil {
		t.Fatal("prefetch() returned a lookup alongside the error, want nil")
	}
}

// TestAcceptedGenerationPrefetchRecordsStats proves the prefetch reports
// keys/queries/rows/duration into the context stats (#7724 telemetry:
// per-visit prefetch keys/queries/rows + durations).
func TestAcceptedGenerationPrefetchRecordsStats(t *testing.T) {
	t.Parallel()

	database := newBatchAcceptanceTestDB()
	database.rows[acceptanceKey("scope-a", "unit-a", "run-1")] = sharedProjectionAcceptanceRow{
		scopeID: "scope-a", acceptanceUnitID: "unit-a", sourceRunID: "run-1", generationID: "gen-1",
	}
	intents := []reducer.SharedProjectionIntentRow{
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", RepositoryID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		{ScopeID: "scope-missing", AcceptanceUnitID: "unit-missing", RepositoryID: "unit-missing", SourceRunID: "run-1", GenerationID: "gen-x"},
	}

	var stats sharedintent.PrefetchStats
	ctx := sharedintent.ContextWithPrefetchStats(context.Background(), &stats)
	prefetch := NewAcceptedGenerationPrefetch(database)
	if _, err := prefetch(ctx, intents); err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if stats.Acceptance.Keys != 2 || stats.Acceptance.Queries != 1 || stats.Acceptance.Rows != 1 {
		t.Fatalf("acceptance stats = %+v, want keys=2 queries=1 rows=1", stats.Acceptance)
	}
	if stats.Readiness != (sharedintent.PrefetchKindStats{}) {
		t.Fatalf("readiness stats = %+v, want zero", stats.Readiness)
	}
}

// TestAcceptedGenerationPrefetchEmptyIssuesNoQuery proves an empty (or
// unkeyed) intent set costs no round trip.
func TestAcceptedGenerationPrefetchEmptyIssuesNoQuery(t *testing.T) {
	t.Parallel()

	database := newBatchAcceptanceTestDB()
	prefetch := NewAcceptedGenerationPrefetch(database)
	lookup, err := prefetch(context.Background(), []reducer.SharedProjectionIntentRow{
		{ScopeID: "", AcceptanceUnitID: "", RepositoryID: "", SourceRunID: ""},
	})
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}
	if database.queryCount != 0 {
		t.Fatalf("prefetch() issued %d queries for unkeyed intents, want 0", database.queryCount)
	}
	if _, found := lookup(reducer.SharedProjectionAcceptanceKey{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1"}); found {
		t.Fatal("lookup() found a key with no rows, want not-found")
	}
}

// batchAcceptanceTestDB is an in-memory ExecQueryer serving both the legacy
// per-key acceptance lookup and the #7724 batched UNNEST lookup, so the
// query-count tests fail on the N+1 loop and pass on the batch. It records
// query counts, per-batch key counts, and the exact keys each batch
// carried for normalization assertions.
type batchAcceptanceTestDB struct {
	rows         map[string]sharedProjectionAcceptanceRow
	queryCount   int
	maxBatchKeys int
	batches      [][]string
}

func newBatchAcceptanceTestDB() *batchAcceptanceTestDB {
	return &batchAcceptanceTestDB{rows: make(map[string]sharedProjectionAcceptanceRow)}
}

func (database *batchAcceptanceTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (database *batchAcceptanceTestDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	database.queryCount++
	if strings.Contains(query, "UNNEST(") {
		return database.queryBatch(args...)
	}
	return queryAcceptanceRows(database.testRows(), query, args...)
}

func (database *batchAcceptanceTestDB) testRows() []sharedProjectionAcceptanceRow {
	rows := make([]sharedProjectionAcceptanceRow, 0, len(database.rows))
	for _, row := range database.rows {
		rows = append(rows, row)
	}
	return rows
}

func (database *batchAcceptanceTestDB) queryBatch(args ...any) (db.Rows, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("batch acceptance query wants 3 array args, got %d", len(args))
	}
	scopes, ok := args[0].([]string)
	if !ok {
		return nil, fmt.Errorf("batch acceptance arg 0 is %T, want []string", args[0])
	}
	units, ok := args[1].([]string)
	if !ok {
		return nil, fmt.Errorf("batch acceptance arg 1 is %T, want []string", args[1])
	}
	runs, ok := args[2].([]string)
	if !ok {
		return nil, fmt.Errorf("batch acceptance arg 2 is %T, want []string", args[2])
	}
	if len(scopes) != len(units) || len(scopes) != len(runs) {
		return nil, fmt.Errorf("batch acceptance arrays diverge: %d/%d/%d", len(scopes), len(units), len(runs))
	}
	if len(scopes) > database.maxBatchKeys {
		database.maxBatchKeys = len(scopes)
	}
	var carried []string
	var matches [][]any
	for i := range scopes {
		carried = append(carried, scopes[i], units[i], runs[i])
		if row, ok := database.rows[acceptanceKey(scopes[i], units[i], runs[i])]; ok {
			matches = append(matches, []any{row.scopeID, row.acceptanceUnitID, row.sourceRunID, row.generationID})
		}
	}
	database.batches = append(database.batches, carried)
	return &acceptanceRows{data: matches, idx: -1}, nil
}
