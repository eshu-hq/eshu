// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 full-bootstrap proof that the admin dead-letter replay and the admin repo_dependency intent reopen stamp reopened_at with the store clock, so a 100-hour-old row ages from the admin action; about 1.6 s locally in an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAdminReopenWritersStampReopenedAtLive is the #9320 proof for the two
// admin writers: the admin dead-letter replay moves a terminal fact work item
// back to pending, and the admin repo_dependency reopen clears a completed
// shared projection intent. Both stamp reopened_at with the store clock, so
// the #9292 backlog age counts from the admin action. Before #9320 the replay
// left reopened_at NULL and shared_projection_intents had no reopen marker,
// so both kept aging from a 100-hour-old created_at.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestAdminReopenWritersStampReopenedAtLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9320 admin reopen-stamp proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "admin_reopen_stamp", func(ctx context.Context, database *sql.DB) error {
		return pgstatus.ApplyBootstrap(ctx, pgstatus.SQLDB{DB: database})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.Add(-100 * time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('s-admin', 'repository', 'git', 's-admin', 'git', 's-admin', $1, $1, 'active', 'g-admin', '{}'::jsonb)`, old)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ('g-admin', 's-admin', 'snapshot', $1, $1, 'active', $1, '{}'::jsonb)`, old)
	exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
  failure_class, payload, created_at, updated_at, visible_at)
VALUES ('r-admin-dead', 's-admin', 'g-admin', 'reducer', 'd-admin', 'dead_letter', 3, 'retry_exhausted', '{}'::jsonb, $1, $1, $1)`, old)
	exec(`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
  acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ('i-admin', 'repo_dependency', 'p', 's-admin', 'unit-admin', 'repo-admin', 'run-admin', 'g-admin', '{}'::jsonb, $1, $1)`, old)
	exec(`INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
VALUES ('s-admin', 'unit-admin', 'run-admin', 'g-admin', $1, $1)`, old)

	adminStore := NewStore(database)
	adminStore.(*postgresStore).now = func() time.Time { return now }

	replayed, err := adminStore.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{WorkItemIDs: []string{"r-admin-dead"}, Limit: 10})
	if err != nil || len(replayed) != 1 {
		t.Fatalf("ReplayFailedWorkItems() = (%d items, %v), want 1", len(replayed), err)
	}
	reopened, err := adminStore.ReopenCompletedWork(ctx, admin.ReopenFilter{Domain: admin.ReopenDomainRepoDependency, ScopeID: "s-admin", Limit: 10})
	if err != nil || len(reopened.IntentIDs) != 1 {
		t.Fatalf("ReopenCompletedWork() = (%v, %v), want the one completed intent", reopened.IntentIDs, err)
	}

	for _, c := range []struct{ name, query string }{
		{"admin replay", `SELECT status = 'pending', reopened_at,
  EXTRACT(EPOCH FROM ($1::timestamptz - COALESCE(reopened_at, created_at)))::float8
FROM fact_work_items WHERE work_item_id = 'r-admin-dead'`},
		{"admin intent reopen", `SELECT completed_at IS NULL, reopened_at,
  EXTRACT(EPOCH FROM ($1::timestamptz - COALESCE(reopened_at, created_at)))::float8
FROM shared_projection_intents WHERE intent_id = 'i-admin'`},
	} {
		var outstanding bool
		var reopenedAt sql.NullTime
		var age float64
		if err := database.QueryRowContext(ctx, c.query, now).Scan(&outstanding, &reopenedAt, &age); err != nil {
			t.Fatalf("%s: read row: %v", c.name, err)
		}
		if !outstanding {
			t.Fatalf("%s: row is not outstanding after the admin action", c.name)
		}
		if !reopenedAt.Valid || !reopenedAt.Time.Equal(now) || age != 0 {
			t.Fatalf("%s: reopened_at = %v (valid=%v), age = %vs; want stamped at %v with age 0, not aging from created_at %v",
				c.name, reopenedAt.Time, reopenedAt.Valid, age, now, old)
		}
	}
}
