// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Run the live proofs with a throwaway Postgres whose role may CREATE DATABASE:
//
//	ESHU_POSTGRES_DSN='postgres://eshu:change-me@localhost:15432/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres/retention/work/items -run Live -count=1 -v
//
// Each proof gets a database of its own, because a retention pass walks the
// whole workflow_work_items table and must not touch another test's rows.

// liveEpoch anchors seeded timestamps far in the past so every seeded row is
// older than the policy cutoff unless a test says otherwise.
var liveEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// liveAsOf is the pass time the proofs use: a year after liveEpoch.
var liveAsOf = liveEpoch.Add(365 * 24 * time.Hour)

func isolatedLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	raw := os.Getenv("ESHU_POSTGRES_DSN")
	if raw == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the workflow work item retention live proofs")
	}
	dsn, err := url.Parse(raw)
	if err != nil || dsn.Scheme == "" {
		t.Skip("the retention live proofs need a URL-form ESHU_POSTGRES_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	base, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open base db: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	name := fmt.Sprintf("workitem_retention_%d", time.Now().UnixNano())
	if _, err := base.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create an isolated database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = base.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	dsn.Path = "/" + name
	isolated, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatalf("open isolated db: %v", err)
	}
	isolated.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = isolated.Close() })
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: isolated}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	if err := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: isolated}).EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure workflow control schema: %v", err)
	}
	return isolated, ctx
}

// seedRow is one workflow_work_items row with a claim row, so the proofs also
// see the ON DELETE CASCADE into workflow_claims.
type seedRow struct {
	id        string
	runID     string
	scopeID   string
	status    string
	ageCycles int // cycle index from liveEpoch; higher is newer
	young     bool
	kind      string    // collector_kind and source_system; empty means aws
	at        time.Time // explicit updated_at; zero uses ageCycles and young
}

func (r seedRow) collectorKind() string {
	if r.kind == "" {
		return "aws"
	}
	return r.kind
}

func (r seedRow) updatedAt() time.Time {
	if !r.at.IsZero() {
		return r.at
	}
	if r.young {
		return liveAsOf.Add(-time.Hour).Add(time.Duration(r.ageCycles) * time.Minute)
	}
	return liveEpoch.Add(time.Duration(r.ageCycles) * 15 * time.Minute)
}

func (r seedRow) generationID() string { return "gen-" + r.id }

func seedRun(t *testing.T, ctx context.Context, database *sql.DB, runID, status string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ($1, 'schedule', $2, $3, $3)`, runID, status, liveEpoch); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func seedRows(t *testing.T, ctx context.Context, database *sql.DB, rows ...seedRow) {
	t.Helper()
	for _, row := range rows {
		claimed := row.status == "claimed"
		var claimID, ownerID any
		var lease any
		if claimed {
			claimID, ownerID, lease = "claim-"+row.id, "owner-1", liveAsOf.Add(time.Hour)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (
    work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, attempt_count,
    current_claim_id, current_owner_id, lease_expires_at, created_at, updated_at
) VALUES ($1, $2, $10, 'inst-1', $10, $3, $3, $4, $4, $5, 1, $6, $7, $8, $9, $9)`,
			row.id, row.runID, row.scopeID, row.generationID(), row.status, claimID, ownerID, lease, row.updatedAt(),
			row.collectorKind(),
		); err != nil {
			t.Fatalf("seed work item %s: %v", row.id, err)
		}
		claimStatus := map[string]string{
			"claimed": "active", "completed": "completed", "failed_terminal": "failed_terminal",
			"failed_retryable": "failed_retryable", "expired": "expired", "pending": "expired",
		}[row.status]
		if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_claims (
    claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at,
    lease_expires_at, created_at, updated_at
) VALUES ($1, $2, 1, 'owner-1', $3, $4, $4, $5, $4, $4)`,
			"claim-"+row.id, row.id, claimStatus, row.updatedAt(), liveAsOf.Add(time.Hour),
		); err != nil {
			t.Fatalf("seed claim for %s: %v", row.id, err)
		}
	}
}

func seedActiveGeneration(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'aws', 'aws', $1, 'aws', 'p', $2, $2, 'active', $3)`,
		scopeID, liveEpoch, generationID,
	); err != nil {
		t.Fatalf("seed ingestion scope %s: %v", scopeID, err)
	}
}

func remainingIDs(t *testing.T, ctx context.Context, database *sql.DB) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT work_item_id FROM workflow_work_items`)
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan work item id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list work items: %v", err)
	}
	sort.Strings(ids)
	return ids
}

func orphanClaims(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workflow_claims AS claim
WHERE NOT EXISTS (SELECT 1 FROM workflow_work_items AS item WHERE item.work_item_id = claim.work_item_id)`,
	).Scan(&count); err != nil {
		t.Fatalf("count orphan claims: %v", err)
	}
	return count
}

// drainToWrap runs passes until one reaches the table end, so a proof sees a
// complete walk regardless of the batch bound.
func drainToWrap(t *testing.T, ctx context.Context, sweeper *Sweeper, policyBatch int) (deleted int) {
	t.Helper()
	policy := testPolicy(policyBatch, 1000)
	for range 1000 {
		result, err := sweeper.PruneWorkItems(ctx, liveAsOf, policy)
		if err != nil {
			t.Fatalf("PruneWorkItems() error = %v, want nil", err)
		}
		deleted += result.Deleted()
		if result.Wrapped {
			return deleted
		}
	}
	t.Fatal("retention never reached the table end")
	return deleted
}
