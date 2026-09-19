// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// statusSemanticsAsOf is the fixed status clock for the #6794 semantics
// fixture; every seeded timestamp is relative to it.
var statusSemanticsAsOf = time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

// TestStatusActiveWorkQueriesPreserveSemantics is the #6794 output-parity
// proof for the status queries that embed activeFactWorkItemsCTE and for the
// shared-projection half of domainBacklogQuery. The expected rows are derived
// by hand from the fixture intent below (not from a frozen copy of any SQL),
// and apart from the lease-only phantom count below it passes unmodified on
// the pre-#6794 query shapes, so it pins the observable contract across the
// rewrite:
//
//   - unleased reducer rows on an older generation than the scope's active
//     generation are hidden (older ingested_at, or equal ingested_at with a
//     smaller generation_id); claimed, succeeded, and non-reducer rows on that
//     older generation stay visible;
//   - scopes with no active pointer, a pointer to a missing generation, or a
//     pointer to another scope's generation, and generations newer than the
//     active one keep their rows;
//   - shared projection domains appear only with pending intents or a live
//     lease; a live-lease domain with no intent rows at all reports zero
//     outstanding intents. The pre-#6794 LEFT JOIN counted its null-extended
//     row as one phantom pending intent; #6794 fixes that on purpose.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestStatusActiveWorkQueriesPreserveSemantics(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 status semantics proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)

	cases := []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{
			name:  "stage counts",
			query: stageCountsQuery,
			want: []string{
				"projector|pending|1",
				"reducer|claimed|2",
				"reducer|failed|1",
				"reducer|pending|4",
				"reducer|retrying|1",
				"reducer|succeeded|1",
			},
		},
		{
			name:  "queue snapshot",
			query: queueSnapshotQuery,
			args:  []any{statusSemanticsAsOf},
			// total counts every row, hidden stale rows included. The
			// bootstrap records the 096 provenance upgrade marker.
			want: []string{"13|8|5|2|1|1|0|1|true|0|1800|1"},
		},
		{
			name:  "domain backlog",
			query: domainBacklogQuery,
			args:  []any{statusSemanticsAsOf},
			want: []string{
				"done|3|1|0|0|0|10800",
				"shared|2|0|0|0|0|7200",
				"expired|1|0|0|0|0|14400",
				"d2|1|0|1|0|0|1800",
				"d3|1|0|0|0|1|1800",
				"d4|1|0|0|0|0|1800",
				"d5|1|1|0|0|0|1800",
				"dforeign|1|0|0|0|0|1800",
				"dproj|1|0|0|0|0|1800",
				"completedlease|0|1|0|0|0|0",
				"leaseonly|0|1|0|0|0|0",
			},
		},
		{
			name:  "latest queue failure",
			query: latestQueueFailureQuery,
			want:  []string{"w-tie-c"},
		},
		{
			name:  "conflict blockages",
			query: reducerConflictBlockageQuery,
			args:  []any{statusSemanticsAsOf},
			// w-old-pending shares the fenced conflict key but is a hidden
			// stale row, so domain "done" must not appear.
			want: []string{"reducer|d4|cd|k|1|1800"},
		},
	}
	for _, tc := range cases {
		got := statusSemanticsRows(ctx, t, conn, tc.query, tc.args...)
		if tc.name == "latest queue failure" {
			got = statusSemanticsWorkItemIDs(got)
		}
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s rows:\n got: %q\nwant: %q", tc.name, got, tc.want)
		}
	}
}

// TestActiveWorkSummaryMatchesStandaloneReads is the #6794 dedupe
// differential: on the semantics fixture, the single activeWorkSummaryQuery
// round trip must decode to exactly what the five pre-#6794 standalone reads
// (kept as a test oracle) return.
func TestActiveWorkSummaryMatchesStandaloneReads(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 active work summary differential")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)
	seedActiveWorkDifferentialExtras(ctx, t, conn)
	queryer := sqlConnQueryer{conn: conn}

	summary, err := readActiveWorkSummary(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("readActiveWorkSummary() error = %v", err)
	}
	stageCounts, err := listStageCounts(ctx, queryer)
	if err != nil {
		t.Fatalf("standalone stage counts: %v", err)
	}
	backlogs, err := listDomainBacklogs(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone domain backlog: %v", err)
	}
	queue, err := readQueueSnapshot(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone queue snapshot: %v", err)
	}
	blockages, err := listReducerConflictBlockages(ctx, queryer, statusSemanticsAsOf)
	if err != nil {
		t.Fatalf("standalone blockages: %v", err)
	}
	failure, err := readLatestQueueFailure(ctx, queryer)
	if err != nil {
		t.Fatalf("standalone latest failure: %v", err)
	}
	for name, pair := range map[string][2]any{
		"stage counts":   {summary.StageCounts, stageCounts},
		"domain backlog": {summary.DomainBacklogs, backlogs},
		"queue snapshot": {summary.Queue, queue},
		"blockages":      {summary.Blockages, blockages},
		"latest failure": {summary.LatestFailure, failure},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s:\n summary:    %#v\n standalone: %#v", name, pair[0], pair[1])
		}
	}
	if len(summary.DomainBacklogs) == 0 || summary.LatestFailure == nil {
		t.Fatalf("fixture must exercise every section: %#v", summary)
	}
	// The extras seed more fenced conflict keys than the blockage limit, with
	// age ties, and several failure candidates with an updated_at tie, so a
	// changed limit or ordering in the summary cannot pass by coincidence.
	if got, want := len(summary.Blockages), reducerConflictBlockageLimit; got != want {
		t.Fatalf("blockage rows = %d, want the %d-row limit to truncate", got, want)
	}
	if got, want := summary.LatestFailure.WorkItemID, "w-fail-tie-a"; got != want {
		t.Fatalf("latest failure = %q, want %q (updated_at tie broken by work_item_id)", got, want)
	}
}

// seedActiveWorkDifferentialExtras adds rows only the differential needs: 13
// fenced conflict keys over three domains (more than the blockage limit, with
// equal blocked counts and repeated ages so the order's later keys decide the
// cut) and three failure candidates, two tied on updated_at.
func seedActiveWorkDifferentialExtras(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed extras: %v\n%s", err, query)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('s-extra', 'repository', 'git', 's-extra', 'git', 's-extra', $1, $1, 'active', 'g-extra', '{}'::jsonb)`, at(-3*time.Hour))
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('g-extra', 's-extra', 'snapshot', $1, $1, 'active', '{}'::jsonb)`, at(-2*time.Hour))
	insertWork := func(id, domain, status, conflictDomain, conflictKey, failure string, created, updated time.Duration, claimUntil any) {
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  conflict_domain, conflict_key, status, attempt_count, claim_until, failure_class,
  payload, created_at, updated_at)
VALUES ($1, 's-extra', 'g-extra', 'reducer', $2, $3, $4, $5, 0, $6, NULLIF($7, ''), '{}'::jsonb, $8, $9)`,
			id, domain, conflictDomain, conflictKey, status, claimUntil, failure, at(created), at(updated))
	}
	for i := 0; i < 13; i++ {
		domain := []string{"dx-a", "dx-b", "dx-c"}[i%3]
		key := "key-" + string(rune('a'+i))
		// Ages repeat every four keys, so several keys tie on age.
		created := -time.Duration(10+(i%4)*5) * time.Minute
		insertWork("w-fence-"+key, domain, "claimed", "cx", key, "", created, -time.Minute, at(10*time.Minute))
		insertWork("w-block-"+key, domain, "pending", "cx", key, "", created, -time.Minute, nil)
	}
	insertWork("w-fail-tie-b", "dx-fail", "failed", "cf", "f1", "tie-b", -time.Hour, -3*time.Minute, nil)
	insertWork("w-fail-tie-a", "dx-fail", "retrying", "cf", "f2", "tie-a", -time.Hour, -3*time.Minute, nil)
	insertWork("w-fail-older", "dx-fail", "dead_letter", "cf", "f3", "older", -time.Hour, -4*time.Minute, nil)
	if _, err := conn.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("analyze extras: %v", err)
	}
}

// TestActiveFactWorkItemsFormsSelectTheSameRows keeps the materialized
// activeFactWorkItemsCTE and the per-row activeFactWorkItemsPerRowCTE (used by
// the drain EXISTS) in step: on the semantics fixture, which covers every
// stale-generation branch, both must select exactly the same work items.
func TestActiveFactWorkItemsFormsSelectTheSameRows(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 active work form parity proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	seedStatusSemanticsFixture(ctx, t, conn)
	seedActiveWorkDifferentialExtras(ctx, t, conn)

	selectIDs := func(cte string) []string {
		return statusSemanticsRows(ctx, t, conn,
			"WITH "+cte+" SELECT work_item_id FROM active_fact_work_items ORDER BY work_item_id")
	}
	materialized := selectIDs(activeFactWorkItemsCTE)
	perRow := selectIDs(activeFactWorkItemsPerRowCTE)
	if strings.Join(materialized, ",") != strings.Join(perRow, ",") {
		t.Fatalf("active work forms disagree:\n materialized: %v\n per-row:      %v", materialized, perRow)
	}
	if len(materialized) == 0 {
		t.Fatal("fixture selected no active work items")
	}
	for _, hidden := range []string{"w-old-pending", "w-old-dead", "w-tie-a"} {
		for _, id := range materialized {
			if id == hidden {
				t.Fatalf("stale unleased row %s must be hidden by both forms", hidden)
			}
		}
	}
}

// sqlConnQueryer adapts a dedicated *sql.Conn (which carries the fixture
// schema's search_path) to db.Queryer.
type sqlConnQueryer struct{ conn *sql.Conn }

func (q sqlConnQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.conn.QueryContext(ctx, query, args...)
}

func openStatusSemanticsSchema(ctx context.Context, t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	schema := fmt.Sprintf("status_semantics_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = conn.Close()
	})
	for _, stmt := range []string{"CREATE SCHEMA " + schema, "SET search_path TO " + schema + ", public"} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	for _, def := range BootstrapDefinitions() {
		if _, err := conn.ExecContext(ctx, def.SQL); err != nil {
			t.Fatalf("apply migration %s: %v", def.Name, err)
		}
	}
	return conn
}

// seedStatusSemanticsFixture seeds six scopes that cover every branch of the
// stale-generation filter plus shared projection intents and leases covering
// every branch of the shared-projection backlog.
func seedStatusSemanticsFixture(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	at := func(offset time.Duration) time.Time { return statusSemanticsAsOf.Add(offset) }
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	for _, scope := range []struct{ id, active string }{
		{"s-stale", "g-new"},
		{"s-tie", "g-tie-b"},
		{"s-noactive", ""},
		{"s-missing", "g-missing-pointer"},
		{"s-newer", "g-n1"},
		// Active pointer names another scope's generation: it must not count
		// as this scope's active generation.
		{"s-foreign", "g-na"},
	} {
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULLIF($3, ''), '{}'::jsonb)`,
			scope.id, at(-3*time.Hour), scope.active)
	}
	for _, gen := range []struct {
		id, scope string
		ingested  time.Duration
	}{
		{"g-old", "s-stale", -3 * time.Hour},
		{"g-new", "s-stale", -2 * time.Hour},
		{"g-tie-a", "s-tie", -2 * time.Hour},
		{"g-tie-b", "s-tie", -2 * time.Hour},
		{"g-tie-c", "s-tie", -2 * time.Hour},
		{"g-na", "s-noactive", -3 * time.Hour},
		{"g-mp", "s-missing", -3 * time.Hour},
		{"g-n1", "s-newer", -3 * time.Hour},
		{"g-n2", "s-newer", -2 * time.Hour},
		{"g-f-old", "s-foreign", -4 * time.Hour},
	} {
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3,
  CASE WHEN EXISTS (SELECT 1 FROM ingestion_scopes WHERE active_generation_id = $1)
       THEN 'active' ELSE 'superseded' END, '{}'::jsonb)`, gen.id, gen.scope, at(gen.ingested))
	}
	for _, work := range []struct {
		id, stage, domain, status, scope, gen, conflictDomain, failure string
		created, updated                                               time.Duration
		claimUntil                                                     *time.Duration
	}{
		{id: "w-old-pending", stage: "reducer", domain: "done", status: "pending", scope: "s-stale", gen: "g-old", conflictDomain: "cd"},
		{id: "w-old-claimed", stage: "reducer", domain: "done", status: "claimed", scope: "s-stale", gen: "g-old", conflictDomain: "c-old", claimUntil: durationPtr(-time.Minute)},
		{id: "w-old-succeeded", stage: "reducer", domain: "done", status: "succeeded", scope: "s-stale", gen: "g-old", conflictDomain: "c1"},
		{id: "w-old-dead", stage: "reducer", domain: "done", status: "dead_letter", scope: "s-stale", gen: "g-old", conflictDomain: "c1", failure: "hidden", updated: -time.Minute},
		{id: "w-new-pending", stage: "reducer", domain: "done", status: "pending", scope: "s-stale", gen: "g-new", conflictDomain: "c1", created: -10 * time.Minute},
		{id: "w-projector-old", stage: "projector", domain: "dproj", status: "pending", scope: "s-stale", gen: "g-old", conflictDomain: "c1"},
		{id: "w-tie-a", stage: "reducer", domain: "d2", status: "retrying", scope: "s-tie", gen: "g-tie-a", conflictDomain: "c2", failure: "hidden-tie", updated: -2 * time.Minute},
		{id: "w-tie-c", stage: "reducer", domain: "d2", status: "retrying", scope: "s-tie", gen: "g-tie-c", conflictDomain: "c2", failure: "boom", updated: -5 * time.Minute},
		{id: "w-na-failed", stage: "reducer", domain: "d3", status: "failed", scope: "s-noactive", gen: "g-na", conflictDomain: "c3"},
		{id: "w-mp-pending", stage: "reducer", domain: "d3", status: "pending", scope: "s-missing", gen: "g-mp", conflictDomain: "c3"},
		{id: "w-n2-pending", stage: "reducer", domain: "d4", status: "pending", scope: "s-newer", gen: "g-n2", conflictDomain: "cd"},
		{id: "w-foreign-pending", stage: "reducer", domain: "dforeign", status: "pending", scope: "s-foreign", gen: "g-f-old", conflictDomain: "c4"},
		{id: "w-n1-claimed", stage: "reducer", domain: "d5", status: "claimed", scope: "s-newer", gen: "g-n1", conflictDomain: "cd", claimUntil: durationPtr(10 * time.Minute)},
	} {
		created := work.created
		if created == 0 {
			created = -30 * time.Minute
		}
		updated := work.updated
		if updated == 0 {
			updated = -20 * time.Minute
		}
		var claimUntil any
		if work.claimUntil != nil {
			claimUntil = at(*work.claimUntil)
		}
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  conflict_domain, conflict_key, status, attempt_count, claim_until, failure_class,
  payload, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'k', $7, 0, $8, NULLIF($9, ''), '{}'::jsonb, $10, $11)`,
			work.id, work.scope, work.gen, work.stage, work.domain, work.conflictDomain,
			work.status, claimUntil, work.failure, at(created), at(updated))
	}
	for _, intent := range []struct {
		id, domain         string
		created, completed time.Duration
	}{
		{"i-done", "done", -3 * time.Hour, 0},
		{"i-shared-1", "shared", -2 * time.Hour, 0},
		{"i-shared-2", "shared", -time.Hour, 0},
		{"i-shared-3", "shared", -4 * time.Hour, -time.Hour},
		{"i-completed", "completedonly", -time.Hour, -time.Minute},
		{"i-completedlease", "completedlease", -time.Hour, -time.Minute},
		{"i-expired", "expired", -4 * time.Hour, 0},
	} {
		var completed any
		if intent.completed != 0 {
			completed = at(intent.completed)
		}
		exec(`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key,
  repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ($1, $2, 'p', 'r', 'run', 'g', '{}'::jsonb, $3, $4)`, intent.id, intent.domain, at(intent.created), completed)
	}
	for _, lease := range []struct {
		domain, owner string
		expires       time.Duration
	}{
		{"completedlease", "worker", 5 * time.Minute},
		{"leaseonly", "worker", 5 * time.Minute},
		{"expired", "worker", -5 * time.Minute},
		{"unowned", "", 5 * time.Minute},
	} {
		exec(`INSERT INTO shared_projection_partition_leases (projection_domain, partition_id,
  partition_count, lease_owner, lease_expires_at, updated_at)
VALUES ($1, 0, 1, NULLIF($2, ''), $3, $4)`, lease.domain, lease.owner, at(lease.expires), statusSemanticsAsOf)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

func durationPtr(d time.Duration) *time.Duration { return &d }

// statusSemanticsRows renders every row as pipe-joined text so the expected
// rows stay readable; numeric ages are rounded to whole seconds.
func statusSemanticsRows(ctx context.Context, t *testing.T, conn *sql.Conn, query string, args ...any) []string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	var out []string
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		fields := make([]string, len(values))
		for i, value := range values {
			switch typed := value.(type) {
			case float64:
				fields[i] = fmt.Sprintf("%.0f", typed)
			case string:
				// pgx renders numeric EXTRACT(EPOCH ...) results as text.
				fields[i] = typed
				if parsed, parseErr := strconv.ParseFloat(typed, 64); parseErr == nil && strings.Contains(typed, ".") {
					fields[i] = fmt.Sprintf("%.0f", parsed)
				}
			case time.Time:
				fields[i] = typed.UTC().Format(time.RFC3339)
			default:
				fields[i] = fmt.Sprint(typed)
			}
		}
		out = append(out, strings.Join(fields, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// statusSemanticsWorkItemIDs keeps the work_item_id field (the first
// "w-"-prefixed field) of each rendered row.
func statusSemanticsWorkItemIDs(rows []string) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, field := range strings.Split(row, "|") {
			if strings.HasPrefix(field, "w-") {
				ids = append(ids, field)
				break
			}
		}
	}
	return ids
}
