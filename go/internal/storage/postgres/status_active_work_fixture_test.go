// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

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
		{id: "w-old-superseded", stage: "reducer", domain: "done", status: "superseded", scope: "s-stale", gen: "g-old", conflictDomain: "c1"},
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
