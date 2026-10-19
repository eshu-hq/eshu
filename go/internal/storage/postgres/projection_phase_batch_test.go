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

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestGraphProjectionReadinessPrefetchSingleQueryPerRound is the #7724 1A
// RED test for readiness: N distinct phase keys must cost at most
// ceil(N/1000) queries, not one Lookup per key.
func TestGraphProjectionReadinessPrefetchSingleQueryPerRound(t *testing.T) {
	t.Parallel()

	const distinct = 500
	database := newBatchReadinessTestDB()
	var keys []gpphase.PhaseKey
	for i := 0; i < distinct; i++ {
		key := readinessTestKey(i)
		database.rows[graphProjectionPhaseStateCompositeKey(graphProjectionPhaseStateRow{
			scopeID: key.ScopeID, acceptanceUnitID: key.AcceptanceUnitID, sourceRunID: key.SourceRunID,
			generationID: key.GenerationID, keyspace: string(key.Keyspace),
			phase: string(gpphase.PhaseCanonicalNodesCommitted),
		})] = graphProjectionPhaseStateRow{scopeID: key.ScopeID}
		keys = append(keys, key, key)
	}

	prefetch := NewGraphProjectionReadinessPrefetch(database)
	lookup, err := prefetch(context.Background(), keys, gpphase.PhaseCanonicalNodesCommitted)
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if database.queryCount != 1 {
		t.Fatalf("prefetch() issued %d queries for %d distinct keys, want 1", database.queryCount, distinct)
	}
	for i := 0; i < distinct; i++ {
		ready, found := lookup(readinessTestKey(i), gpphase.PhaseCanonicalNodesCommitted)
		if !found || !ready {
			t.Fatalf("lookup(key %d) = (%v, %v), want (true, true)", i, ready, found)
		}
	}
}

// TestGraphProjectionReadinessPrefetchChunksAtOneThousand proves the batch
// bound: 2500 distinct keys cost exactly 3 queries of at most 1000 keys.
func TestGraphProjectionReadinessPrefetchChunksAtOneThousand(t *testing.T) {
	t.Parallel()

	const distinct = 2500
	database := newBatchReadinessTestDB()
	var keys []gpphase.PhaseKey
	for i := 0; i < distinct; i++ {
		keys = append(keys, readinessTestKey(i))
	}

	prefetch := NewGraphProjectionReadinessPrefetch(database)
	if _, err := prefetch(context.Background(), keys, gpphase.PhaseCanonicalNodesCommitted); err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if database.queryCount != 3 {
		t.Fatalf("prefetch() issued %d queries for %d keys, want 3", database.queryCount, distinct)
	}
	if database.maxBatchKeys > 1000 {
		t.Fatalf("largest batch carried %d keys, want at most 1000", database.maxBatchKeys)
	}
}

// TestGraphProjectionReadinessPrefetchNormalizesWhitespace proves the
// #7724 1A TrimSpace contract for readiness: keys are normalized before
// the query and the closure compares on normalized keys.
func TestGraphProjectionReadinessPrefetchNormalizesWhitespace(t *testing.T) {
	t.Parallel()

	database := newBatchReadinessTestDB()
	database.rows[graphProjectionPhaseStateCompositeKey(graphProjectionPhaseStateRow{
		scopeID: "scope-a", acceptanceUnitID: "unit-a", sourceRunID: "run-1",
		generationID: "gen-1", keyspace: string(gpphase.KeyspaceCodeEntitiesUID),
		phase: string(gpphase.PhaseCanonicalNodesCommitted),
	})] = graphProjectionPhaseStateRow{scopeID: "scope-a"}

	prefetch := NewGraphProjectionReadinessPrefetch(database)
	lookup, err := prefetch(context.Background(), []gpphase.PhaseKey{{
		ScopeID: "  scope-a ", AcceptanceUnitID: "\tunit-a", SourceRunID: "run-1 ",
		GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCodeEntitiesUID,
	}}, gpphase.PhaseCanonicalNodesCommitted)
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
	ready, found := lookup(gpphase.PhaseKey{
		ScopeID: "scope-a ", AcceptanceUnitID: " unit-a", SourceRunID: "run-1",
		GenerationID: " gen-1 ", Keyspace: gpphase.KeyspaceCodeEntitiesUID,
	}, gpphase.PhaseCanonicalNodesCommitted)
	if !found || !ready {
		t.Fatalf("lookup(padded key) = (%v, %v), want (true, true)", ready, found)
	}
}

// TestGraphProjectionReadinessPrefetchMatchesPerKeyLookup is the #7724 1A
// differential (idempotency proof) for readiness: over found, not-found,
// wrong-phase, duplicate, and invalid fixtures the batch prefetch closure
// must agree exactly with the per-key store Lookup the old loop called.
func TestGraphProjectionReadinessPrefetchMatchesPerKeyLookup(t *testing.T) {
	t.Parallel()

	database := newBatchReadinessTestDB()
	seedReadinessRow(database, "scope-a", "unit-a", "run-1", "gen-1", gpphase.PhaseCanonicalNodesCommitted)
	seedReadinessRow(database, "scope-a", "unit-a", "run-1", "gen-1", gpphase.PhaseSemanticNodesCommitted)
	keys := []gpphase.PhaseKey{
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
		{ScopeID: "scope-missing", AcceptanceUnitID: "unit-missing", SourceRunID: "run-1", GenerationID: "gen-x", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
		{ScopeID: "", AcceptanceUnitID: "", SourceRunID: "", GenerationID: "", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
	}

	ctx := context.Background()
	prefetch := NewGraphProjectionReadinessPrefetch(database)
	lookup, err := prefetch(ctx, keys, gpphase.PhaseCanonicalNodesCommitted)
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	store := NewGraphProjectionPhaseStateStore(database)
	probes := []struct {
		key   gpphase.PhaseKey
		phase gpphase.Phase
	}{
		{keys[0], gpphase.PhaseCanonicalNodesCommitted},
		{keys[0], gpphase.PhaseSemanticNodesCommitted},
		{keys[2], gpphase.PhaseCanonicalNodesCommitted},
		{keys[3], gpphase.PhaseCanonicalNodesCommitted},
		{
			key:   gpphase.PhaseKey{ScopeID: "scope-never-queried", AcceptanceUnitID: "unit-z", SourceRunID: "run-9", GenerationID: "gen-z", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
			phase: gpphase.PhaseCanonicalNodesCommitted,
		},
	}
	for _, probe := range probes {
		wantReady, wantFound, err := store.Lookup(ctx, probe.key, probe.phase)
		if err != nil {
			t.Fatalf("Lookup(%v) error = %v", probe.key, err)
		}
		// The prefetch only resolves the keys and phase it was called
		// with; anything outside that set answers not-found, mirroring
		// the old loop exactly.
		prefetched := probe.phase == gpphase.PhaseCanonicalNodesCommitted && probe.key.Validate() == nil
		if prefetched {
			seen := false
			for _, key := range keys {
				if graphProjectionReadinessCompositeKey(key, probe.phase) == graphProjectionReadinessCompositeKey(probe.key, probe.phase) {
					seen = true
					break
				}
			}
			prefetched = seen
		}
		if !prefetched {
			wantFound = false
			wantReady = false
		}
		gotReady, gotFound := lookup(probe.key, probe.phase)
		if gotReady != wantReady || gotFound != wantFound {
			t.Fatalf("lookup(%v, %q) = (%v, %v), want (%v, %v)", probe.key, probe.phase, gotReady, gotFound, wantReady, wantFound)
		}
	}
}

// TestGraphProjectionReadinessPrefetchErrorFailsSelection proves a batch
// error fails the prefetch instead of silently dropping or keeping rows.
func TestGraphProjectionReadinessPrefetchErrorFailsSelection(t *testing.T) {
	t.Parallel()

	prefetch := NewGraphProjectionReadinessPrefetch(&acceptanceStoreErrorDB{})
	lookup, err := prefetch(context.Background(), []gpphase.PhaseKey{readinessTestKey(0)}, gpphase.PhaseCanonicalNodesCommitted)
	if err == nil {
		t.Fatal("prefetch() error = nil on store failure, want error")
	}
	if lookup != nil {
		t.Fatal("prefetch() returned a lookup alongside the error, want nil")
	}
}

// TestGraphProjectionReadinessPrefetchRecordsStats proves the prefetch
// reports keys/queries/rows/duration into the context stats.
func TestGraphProjectionReadinessPrefetchRecordsStats(t *testing.T) {
	t.Parallel()

	database := newBatchReadinessTestDB()
	seedReadinessRow(database, "scope-a", "unit-a", "run-1", "gen-1", gpphase.PhaseCanonicalNodesCommitted)
	keys := []gpphase.PhaseKey{
		{ScopeID: "scope-a", AcceptanceUnitID: "unit-a", SourceRunID: "run-1", GenerationID: "gen-1", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
		{ScopeID: "scope-missing", AcceptanceUnitID: "unit-missing", SourceRunID: "run-1", GenerationID: "gen-x", Keyspace: gpphase.KeyspaceCodeEntitiesUID},
	}

	var stats sharedintent.PrefetchStats
	ctx := sharedintent.ContextWithPrefetchStats(context.Background(), &stats)
	prefetch := NewGraphProjectionReadinessPrefetch(database)
	if _, err := prefetch(ctx, keys, gpphase.PhaseCanonicalNodesCommitted); err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}

	if stats.Readiness.Keys != 2 || stats.Readiness.Queries != 1 || stats.Readiness.Rows != 1 {
		t.Fatalf("readiness stats = %+v, want keys=2 queries=1 rows=1", stats.Readiness)
	}
	if stats.Acceptance != (sharedintent.PrefetchKindStats{}) {
		t.Fatalf("acceptance stats = %+v, want zero", stats.Acceptance)
	}
}

// TestGraphProjectionReadinessPrefetchEmptyIssuesNoQuery proves an empty
// (or invalid) key set costs no round trip.
func TestGraphProjectionReadinessPrefetchEmptyIssuesNoQuery(t *testing.T) {
	t.Parallel()

	database := newBatchReadinessTestDB()
	prefetch := NewGraphProjectionReadinessPrefetch(database)
	lookup, err := prefetch(context.Background(), []gpphase.PhaseKey{
		{ScopeID: "", AcceptanceUnitID: "", SourceRunID: "", GenerationID: ""},
	}, gpphase.PhaseCanonicalNodesCommitted)
	if err != nil {
		t.Fatalf("prefetch() error = %v", err)
	}
	if database.queryCount != 0 {
		t.Fatalf("prefetch() issued %d queries for invalid keys, want 0", database.queryCount)
	}
	if _, found := lookup(readinessTestKey(0), gpphase.PhaseCanonicalNodesCommitted); found {
		t.Fatal("lookup() found a key with no rows, want not-found")
	}
}

func readinessTestKey(i int) gpphase.PhaseKey {
	return gpphase.PhaseKey{
		ScopeID:          fmt.Sprintf("scope-%d", i),
		AcceptanceUnitID: fmt.Sprintf("unit-%d", i),
		SourceRunID:      "run-1",
		GenerationID:     fmt.Sprintf("gen-%d", i),
		Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
	}
}

func seedReadinessRow(database *batchReadinessTestDB, scope, unit, run, gen string, phase gpphase.Phase) {
	database.rows[graphProjectionPhaseStateCompositeKey(graphProjectionPhaseStateRow{
		scopeID: scope, acceptanceUnitID: unit, sourceRunID: run, generationID: gen,
		keyspace: string(gpphase.KeyspaceCodeEntitiesUID), phase: string(phase),
	})] = graphProjectionPhaseStateRow{scopeID: scope}
}

// batchReadinessTestDB is an in-memory ExecQueryer serving both the legacy
// per-key phase lookup and the #7724 batched UNNEST lookup, so the
// query-count tests fail on the N+1 loop and pass on the batch.
type batchReadinessTestDB struct {
	rows         map[string]graphProjectionPhaseStateRow
	queryCount   int
	maxBatchKeys int
	batches      [][]string
}

func newBatchReadinessTestDB() *batchReadinessTestDB {
	return &batchReadinessTestDB{rows: make(map[string]graphProjectionPhaseStateRow)}
}

func (database *batchReadinessTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (database *batchReadinessTestDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	database.queryCount++
	if strings.Contains(query, "UNNEST(") {
		return database.queryBatch(args...)
	}
	if !strings.Contains(query, "FROM graph_projection_phase_state") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if len(args) != 6 {
		return nil, fmt.Errorf("expected 6 args, got %d", len(args))
	}
	key := graphProjectionPhaseStateCompositeKey(graphProjectionPhaseStateRow{
		scopeID:          args[0].(string),
		acceptanceUnitID: args[1].(string),
		sourceRunID:      args[2].(string),
		generationID:     args[3].(string),
		keyspace:         args[4].(string),
		phase:            args[5].(string),
	})
	if _, ok := database.rows[key]; !ok {
		return &graphProjectionBoolRows{idx: -1}, nil
	}
	return &graphProjectionBoolRows{data: []bool{true}, idx: -1}, nil
}

func (database *batchReadinessTestDB) queryBatch(args ...any) (db.Rows, error) {
	if len(args) != 6 {
		return nil, fmt.Errorf("batch readiness query wants 6 args, got %d", len(args))
	}
	arrays := make([][]string, 5)
	for i := 0; i < 5; i++ {
		array, ok := args[i].([]string)
		if !ok {
			return nil, fmt.Errorf("batch readiness arg %d is %T, want []string", i, args[i])
		}
		arrays[i] = array
	}
	phase, ok := args[5].(string)
	if !ok {
		return nil, fmt.Errorf("batch readiness arg 5 is %T, want string", args[5])
	}
	for i := 1; i < 5; i++ {
		if len(arrays[i]) != len(arrays[0]) {
			return nil, fmt.Errorf("batch readiness arrays diverge: %d vs %d", len(arrays[0]), len(arrays[i]))
		}
	}
	if len(arrays[0]) > database.maxBatchKeys {
		database.maxBatchKeys = len(arrays[0])
	}
	var carried []string
	var matches [][]any
	for i := range arrays[0] {
		row := graphProjectionPhaseStateRow{
			scopeID: arrays[0][i], acceptanceUnitID: arrays[1][i], sourceRunID: arrays[2][i],
			generationID: arrays[3][i], keyspace: arrays[4][i], phase: phase,
		}
		carried = append(carried, arrays[0][i], arrays[1][i], arrays[2][i], arrays[3][i], arrays[4][i])
		if _, ok := database.rows[graphProjectionPhaseStateCompositeKey(row)]; ok {
			matches = append(matches, []any{row.scopeID, row.acceptanceUnitID, row.sourceRunID, row.generationID, row.keyspace})
		}
	}
	database.batches = append(database.batches, carried)
	return &readinessBatchRows{data: matches, idx: -1}, nil
}

// readinessBatchRows scans the 5-column batch result (scope, unit, run,
// generation, keyspace); presence of a row means ready.
type readinessBatchRows struct {
	data [][]any
	idx  int
}

func (r *readinessBatchRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *readinessBatchRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	if len(dest) != 5 {
		return fmt.Errorf("scan: got %d dest, want 5", len(dest))
	}
	for i, value := range r.data[r.idx] {
		typed, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
		*typed = value.(string)
	}
	return nil
}

func (r *readinessBatchRows) Err() error   { return nil }
func (r *readinessBatchRows) Close() error { return nil }
