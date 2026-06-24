// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestGraphEndpointPresenceStoreUpsertIsIdempotent(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	row := reducer.EndpointPresenceRow{
		Keyspace:    reducer.GraphProjectionKeyspaceKubernetesWorkloadUID,
		UID:         "workload-1",
		ScopeID:     "scope-cluster",
		CommittedAt: now,
	}

	if err := store.Upsert(ctx, []reducer.EndpointPresenceRow{row}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	// Re-upsert the same (keyspace, uid) — must converge on one row.
	if err := store.Upsert(ctx, []reducer.EndpointPresenceRow{row}); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	if got := db.rowCount("kubernetes_workload_uid", "workload-1"); got != 1 {
		t.Fatalf("row count after re-upsert = %d, want 1", got)
	}
}

func TestGraphEndpointPresenceStoreUpsertSkipsBlankIdentity(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()

	rows := []reducer.EndpointPresenceRow{
		{Keyspace: "", UID: "a", ScopeID: "s"},
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: " ", ScopeID: "s"},
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-1", ScopeID: ""},
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-1", ScopeID: "s"},
	}
	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if got := len(db.rows); got != 1 {
		t.Fatalf("stored rows = %d, want 1 (blank identities skipped)", got)
	}
	if got := db.rowCount("cloud_resource_uid", "cr-1"); got != 1 {
		t.Fatalf("cloud_resource_uid/cr-1 count = %d, want 1", got)
	}
}

func TestGraphEndpointPresenceStoreUpsertBatchBoundary(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()

	const total = graphEndpointPresenceBatchSize*2 + 7
	rows := make([]reducer.EndpointPresenceRow, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, reducer.EndpointPresenceRow{
			Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID,
			UID:      fmt.Sprintf("cr-%d", i),
			ScopeID:  "scope-aws",
		})
	}
	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if got := len(db.rows); got != total {
		t.Fatalf("stored rows = %d, want %d across batch boundary", got, total)
	}
	if db.maxBatchRows > graphEndpointPresenceBatchSize {
		t.Fatalf("a batch wrote %d rows, exceeds batch size %d", db.maxBatchRows, graphEndpointPresenceBatchSize)
	}
}

func TestGraphEndpointPresenceStoreRetractScope(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()

	rows := []reducer.EndpointPresenceRow{
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-1", ScopeID: "scope-a"},
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-2", ScopeID: "scope-a"},
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-3", ScopeID: "scope-b"},
	}
	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if err := store.RetractScope(ctx, []string{"scope-a"}); err != nil {
		t.Fatalf("RetractScope() error = %v", err)
	}
	if got := len(db.rows); got != 1 {
		t.Fatalf("rows after retract = %d, want 1 (only scope-b)", got)
	}
	if db.rowCount("cloud_resource_uid", "cr-3") != 1 {
		t.Fatal("scope-b row should survive a scope-a retract")
	}

	// Empty input is a no-op (no query).
	db.execCount = 0
	if err := store.RetractScope(ctx, nil); err != nil {
		t.Fatalf("RetractScope(nil) error = %v", err)
	}
	if db.execCount != 0 {
		t.Fatalf("RetractScope(nil) issued %d exec(s), want 0", db.execCount)
	}
}

func TestGraphEndpointPresenceStoreRetractStaleRepoGenerations(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()

	ks := reducer.GraphProjectionKeyspaceAPIEndpointRepoPath
	rows := []reducer.EndpointPresenceRow{
		// repo-1: a stale gen-1 endpoint and a current gen-2 endpoint.
		{Keyspace: ks, UID: "u-old", ScopeID: "scope-a", RepoID: "repo-1", SourceGeneration: "gen-1"},
		{Keyspace: ks, UID: "u-new", ScopeID: "scope-a", RepoID: "repo-1", SourceGeneration: "gen-2"},
		// repo-2: an unchanged repo still at gen-1 that this intent did NOT touch.
		{Keyspace: ks, UID: "u-other", ScopeID: "scope-a", RepoID: "repo-2", SourceGeneration: "gen-1"},
		// a different keyspace must never be touched.
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "cr-1", ScopeID: "scope-a", RepoID: "repo-1", SourceGeneration: "gen-1"},
	}
	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	// repo-1 re-materialized at gen-2: drop its non-gen-2 rows only.
	if err := store.RetractStaleRepoGenerations(ctx, ks, "scope-a", "gen-2", []string{"repo-1"}); err != nil {
		t.Fatalf("RetractStaleRepoGenerations() error = %v", err)
	}
	if db.rowCount(string(ks), "u-old") != 0 {
		t.Fatal("repo-1 gen-1 endpoint should be retracted as stale")
	}
	if db.rowCount(string(ks), "u-new") != 1 {
		t.Fatal("repo-1 current gen-2 endpoint must survive")
	}
	if db.rowCount(string(ks), "u-other") != 1 {
		t.Fatal("repo-2 (untouched repo) must survive even though it is at an older generation")
	}
	if db.rowCount("cloud_resource_uid", "cr-1") != 1 {
		t.Fatal("a different keyspace must never be retracted")
	}

	// Blank generation / empty repos / blank scope are no-ops (no query).
	db.execCount = 0
	for _, c := range []struct {
		scope, gen string
		repos      []string
	}{
		{"scope-a", "", []string{"repo-1"}},
		{"scope-a", "gen-2", nil},
		{"", "gen-2", []string{"repo-1"}},
	} {
		if err := store.RetractStaleRepoGenerations(ctx, ks, c.scope, c.gen, c.repos); err != nil {
			t.Fatalf("RetractStaleRepoGenerations no-op error = %v", err)
		}
	}
	if db.execCount != 0 {
		t.Fatalf("no-op retract issued %d exec(s), want 0", db.execCount)
	}
}

func TestGraphEndpointPresenceStoreMissingUIDs(t *testing.T) {
	t.Parallel()

	db := newGraphEndpointPresenceTestDB()
	store := NewGraphEndpointPresenceStore(db)
	ctx := context.Background()

	present := []reducer.EndpointPresenceRow{
		{Keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID, UID: "w-present-1", ScopeID: "s"},
		{Keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID, UID: "w-present-2", ScopeID: "s"},
		// Same uid string in a different keyspace must not satisfy a workload lookup.
		{Keyspace: reducer.GraphProjectionKeyspaceCloudResourceUID, UID: "w-missing-1", ScopeID: "s"},
	}
	if err := store.Upsert(ctx, present); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	cases := []struct {
		name     string
		keyspace reducer.GraphProjectionKeyspace
		uids     []string
		want     []string
		wantQ    int
	}{
		{
			name:     "all present",
			keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID,
			uids:     []string{"w-present-1", "w-present-2"},
			want:     nil,
			wantQ:    1,
		},
		{
			name:     "all missing",
			keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID,
			uids:     []string{"w-missing-1", "w-missing-2"},
			want:     []string{"w-missing-1", "w-missing-2"},
			wantQ:    1,
		},
		{
			name:     "mixed and deduped",
			keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID,
			uids:     []string{"w-present-1", "w-missing-1", "w-missing-1", "w-present-2", ""},
			want:     []string{"w-missing-1"},
			wantQ:    1,
		},
		{
			name:     "empty input issues no query",
			keyspace: reducer.GraphProjectionKeyspaceKubernetesWorkloadUID,
			uids:     []string{"", "  "},
			want:     nil,
			wantQ:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db.queryCount = 0
			got, err := store.MissingUIDs(ctx, tc.keyspace, tc.uids)
			if err != nil {
				t.Fatalf("MissingUIDs() error = %v", err)
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !equalStringSlices(got, want) {
				t.Fatalf("MissingUIDs() = %v, want %v", got, want)
			}
			if db.queryCount != tc.wantQ {
				t.Fatalf("MissingUIDs() issued %d query(ies), want %d (no N+1)", db.queryCount, tc.wantQ)
			}
		})
	}
}

func TestGraphEndpointPresenceStoreMissingUIDsRejectsBlankKeyspace(t *testing.T) {
	t.Parallel()

	store := NewGraphEndpointPresenceStore(newGraphEndpointPresenceTestDB())
	if _, err := store.MissingUIDs(context.Background(), "", []string{"a"}); err == nil {
		t.Fatal("MissingUIDs() with blank keyspace = nil error, want error")
	}
}

func TestGraphEndpointPresenceSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := GraphEndpointPresenceSchemaSQL()
	if !strings.Contains(sqlStr, "CREATE TABLE IF NOT EXISTS graph_endpoint_presence") {
		t.Fatal("missing graph_endpoint_presence table")
	}
	if !strings.Contains(sqlStr, "PRIMARY KEY (keyspace, uid)") {
		t.Fatal("missing (keyspace, uid) primary key")
	}
	if !strings.Contains(sqlStr, "REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE") {
		t.Fatal("missing scope FK with ON DELETE CASCADE")
	}
	if !strings.Contains(sqlStr, "repo_id TEXT NOT NULL DEFAULT ''") ||
		!strings.Contains(sqlStr, "source_generation TEXT NOT NULL DEFAULT ''") {
		t.Fatal("missing repo_id / source_generation provenance columns (#2842)")
	}
	// #2903 review (P1): the migration must NON-DESTRUCTIVELY recover pre-#2842
	// blank repo_id rows so the repo-scoped retract can match them, WITHOUT
	// deleting still-current target presence (a delete would terminalize live
	// handles_route/runs_in edges). repo_workload's uid IS the repo id, so it
	// backfills repo_id = uid; it must NOT issue a DELETE of blank-provenance rows.
	if !strings.Contains(sqlStr, "UPDATE graph_endpoint_presence") ||
		!strings.Contains(sqlStr, "SET repo_id = uid") ||
		!strings.Contains(sqlStr, "keyspace = 'repo_workload'") {
		t.Fatal("missing non-destructive repo_workload repo_id backfill")
	}
	if strings.Contains(sqlStr, "DELETE FROM graph_endpoint_presence\nWHERE keyspace IN") {
		t.Fatal("migration must not DELETE blank-provenance presence rows (drops current targets)")
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// graphEndpointPresenceRow mirrors the stored tuple for the in-memory fake DB.
type graphEndpointPresenceRow struct {
	keyspace         string
	uid              string
	scopeID          string
	repoID           string
	sourceGeneration string
	committedAt      time.Time
	updatedAt        time.Time
}

// graphEndpointPresenceTestDB is an in-memory ExecQueryer that mimics the
// (keyspace, uid) upsert, scope retract, and ANY($2) present-uid query without a
// real Postgres. It records batch sizes and call counts so tests can prove the
// idempotency, batch-boundary, and single-query (no N+1) contracts.
type graphEndpointPresenceTestDB struct {
	rows         map[string]graphEndpointPresenceRow
	execCount    int
	queryCount   int
	maxBatchRows int
}

func newGraphEndpointPresenceTestDB() *graphEndpointPresenceTestDB {
	return &graphEndpointPresenceTestDB{rows: make(map[string]graphEndpointPresenceRow)}
}

func (db *graphEndpointPresenceTestDB) rowCount(keyspace, uid string) int {
	count := 0
	for _, row := range db.rows {
		if row.keyspace == keyspace && row.uid == uid {
			count++
		}
	}
	return count
}

func (db *graphEndpointPresenceTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execCount++
	switch {
	case strings.Contains(query, "INSERT INTO graph_endpoint_presence"):
		const columnsPerRow = 6
		batchRows := len(args) / columnsPerRow
		if batchRows > db.maxBatchRows {
			db.maxBatchRows = batchRows
		}
		for i := 0; i < len(args); i += columnsPerRow {
			row := graphEndpointPresenceRow{
				keyspace:         args[i+0].(string),
				uid:              args[i+1].(string),
				scopeID:          args[i+2].(string),
				repoID:           args[i+3].(string),
				sourceGeneration: args[i+4].(string),
				committedAt:      args[i+5].(time.Time),
				updatedAt:        args[i+5].(time.Time),
			}
			db.rows[graphEndpointPresenceKey(row.keyspace, row.uid)] = row
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "DELETE FROM graph_endpoint_presence") &&
		strings.Contains(query, "source_generation"):
		// Retract-stale (#2842): DELETE WHERE keyspace=$1 AND scope_id=$2
		// AND repo_id = ANY($3) AND source_generation <> $4.
		keyspace := args[0].(string)
		scopeID := args[1].(string)
		repoIDs := args[2].([]string)
		generation := args[3].(string)
		keepRepo := make(map[string]struct{}, len(repoIDs))
		for _, r := range repoIDs {
			keepRepo[r] = struct{}{}
		}
		for key, row := range db.rows {
			if row.keyspace != keyspace || row.scopeID != scopeID {
				continue
			}
			if _, ok := keepRepo[row.repoID]; !ok {
				continue
			}
			if row.sourceGeneration != generation {
				delete(db.rows, key)
			}
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "DELETE FROM graph_endpoint_presence"):
		scopeIDs := args[0].([]string)
		drop := make(map[string]struct{}, len(scopeIDs))
		for _, s := range scopeIDs {
			drop[s] = struct{}{}
		}
		for key, row := range db.rows {
			if _, ok := drop[row.scopeID]; ok {
				delete(db.rows, key)
			}
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *graphEndpointPresenceTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.queryCount++
	if !strings.Contains(query, "FROM graph_endpoint_presence") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	keyspace := args[0].(string)
	candidates := args[1].([]string)
	want := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		want[c] = struct{}{}
	}
	var present []string
	for _, row := range db.rows {
		if row.keyspace != keyspace {
			continue
		}
		if _, ok := want[row.uid]; ok {
			present = append(present, row.uid)
		}
	}
	sort.Strings(present)
	return &graphEndpointPresenceUIDRows{data: present, idx: -1}, nil
}

func graphEndpointPresenceKey(keyspace, uid string) string {
	return keyspace + "|" + uid
}

type graphEndpointPresenceUIDRows struct {
	data []string
	idx  int
}

func (r *graphEndpointPresenceUIDRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *graphEndpointPresenceUIDRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	typed, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	*typed = r.data[r.idx]
	return nil
}

func (r *graphEndpointPresenceUIDRows) Err() error   { return nil }
func (r *graphEndpointPresenceUIDRows) Close() error { return nil }
