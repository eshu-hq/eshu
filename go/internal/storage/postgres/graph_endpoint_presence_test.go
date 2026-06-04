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
	keyspace    string
	uid         string
	scopeID     string
	committedAt time.Time
	updatedAt   time.Time
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
		const columnsPerRow = 4
		batchRows := len(args) / columnsPerRow
		if batchRows > db.maxBatchRows {
			db.maxBatchRows = batchRows
		}
		for i := 0; i < len(args); i += columnsPerRow {
			row := graphEndpointPresenceRow{
				keyspace:    args[i+0].(string),
				uid:         args[i+1].(string),
				scopeID:     args[i+2].(string),
				committedAt: args[i+3].(time.Time),
				updatedAt:   args[i+3].(time.Time),
			}
			db.rows[graphEndpointPresenceKey(row.keyspace, row.uid)] = row
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
