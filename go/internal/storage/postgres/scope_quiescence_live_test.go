// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

// producerScopeQuiescenceLiveDB opens the live database for this proof, skipping
// with a message that names this proof.
//
// The shared connect-and-bootstrap helper it delegates to skips under the
// aws_cloud_runtime_drift #5848 issue number, because that is where it was
// written. Every CI run skips these live tests, so borrowing that helper
// directly would print the wrong issue number for a #5709 readiness probe on
// every one of them, and send anyone reading the log to an unrelated issue.
func producerScopeQuiescenceLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	if os.Getenv("ESHU_POSTGRES_DSN") == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres #5709 producer-scope quiescence proof")
	}
	return awsCloudRuntimeDriftAdmissionLiveDB(t)
}

// TestProducerScopeQuiescenceLive runs the probe against a real Postgres, which
// nothing did before: the query had only text-shape, empty-kinds, and
// nil-querier coverage, so a wrong column, a broken array binding, or a join
// that silently dropped rows would have shipped green. The #5709 cross-scope
// readiness floor now decides whether a consumer commits or defers on this
// query's answer, so it needs to be executed, not just spelled correctly.
//
// The three states it has to keep apart:
//
//   - no scope of the collector kind at all -> registered empty. The floor reads
//     this as "nothing to wait for" and commits.
//   - a scope with live projector work -> registered, not quiescent. The floor
//     defers.
//   - the same scope once its projector work has drained -> quiescent. The floor
//     commits.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ProducerScopeQuiescenceLive -count=1 -v
func TestProducerScopeQuiescenceLive(t *testing.T) {
	sqlDB, ctx := producerScopeQuiescenceLiveDB(t)
	db := SQLDB{DB: sqlDB}
	now := time.Now().UTC()

	// The collector kind is unique per run so a shared database (this suite's
	// other live proofs seed scopes too) cannot leak rows into the answer.
	suffix := fmt.Sprintf("5709-quiescence-%d", time.Now().UnixNano())
	collectorKind := "oci_registry_" + suffix
	scopeID := "oci_registry:ghcr.io/acme/" + suffix
	generationID := "gen-" + suffix

	t.Run("a collector kind with no scope at all reports nothing registered", func(t *testing.T) {
		report, err := ProducerScopeQuiescence(ctx, db, []string{collectorKind})
		if err != nil {
			t.Fatalf("ProducerScopeQuiescence() error = %v", err)
		}
		if len(report.Registered) != 0 {
			t.Fatalf("registered = %v, want none before any scope of the kind exists", report.Registered)
		}
		if len(report.Quiescent) != 0 {
			t.Fatalf("quiescent = %v, want none", report.Quiescent)
		}
	})

	t.Run("an active scope with live projector work is registered but not quiescent", func(t *testing.T) {
		seedProducerScopeQuiescenceScope(t, ctx, sqlDB, scopeID, collectorKind, generationID, now)
		seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)
		seedProducerScopeQuiescenceWorkItem(
			t, ctx, sqlDB, "wi-pending-"+suffix, scopeID, generationID, "projector", "pending", now,
		)

		report, err := ProducerScopeQuiescence(ctx, db, []string{collectorKind})
		if err != nil {
			t.Fatalf("ProducerScopeQuiescence() error = %v", err)
		}
		if _, ok := report.Registered[scopeID]; !ok {
			t.Fatalf("registered = %v, want it to contain %s", report.Registered, scopeID)
		}
		if _, ok := report.Quiescent[scopeID]; ok {
			t.Fatalf("quiescent = %v, want %s excluded while its projector work is still pending", report.Quiescent, scopeID)
		}
	})

	t.Run("a reducer-stage work item does not hold the scope back", func(t *testing.T) {
		// The fence is projector-stage only. A reducer row left pending must
		// not read as live projector work, or every scope with any queued
		// reducer intent would look permanently busy.
		seedProducerScopeQuiescenceWorkItem(
			t, ctx, sqlDB, "wi-reducer-"+suffix, scopeID, generationID, "reducer", "pending", now,
		)
		if _, err := sqlDB.ExecContext(
			ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, "wi-pending-"+suffix,
		); err != nil {
			t.Fatalf("drain projector work item: %v", err)
		}

		report, err := ProducerScopeQuiescence(ctx, db, []string{collectorKind})
		if err != nil {
			t.Fatalf("ProducerScopeQuiescence() error = %v", err)
		}
		if _, ok := report.Quiescent[scopeID]; !ok {
			t.Fatalf("quiescent = %v, want %s once only a reducer-stage item remains", report.Quiescent, scopeID)
		}
	})

	t.Run("a scope with no active generation is registered but not quiescent", func(t *testing.T) {
		if _, err := sqlDB.ExecContext(
			ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, scopeID,
		); err != nil {
			t.Fatalf("clear active_generation_id: %v", err)
		}

		report, err := ProducerScopeQuiescence(ctx, db, []string{collectorKind})
		if err != nil {
			t.Fatalf("ProducerScopeQuiescence() error = %v", err)
		}
		if _, ok := report.Registered[scopeID]; !ok {
			t.Fatalf("registered = %v, want it to still contain %s", report.Registered, scopeID)
		}
		if _, ok := report.Quiescent[scopeID]; ok {
			t.Fatalf("quiescent = %v, want %s excluded with no active generation", report.Quiescent, scopeID)
		}
	})
}

// seedProducerScopeQuiescenceScope inserts one ingestion_scopes row under a
// caller-chosen collector_kind, which is what the probe filters on.
func seedProducerScopeQuiescenceScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	collectorKind string,
	activeGenerationID string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1, 'container_registry', 'oci', $1, $2, $1, $3, $3, 'active', $4, '{}'::jsonb)
		ON CONFLICT (scope_id) DO UPDATE SET
		  collector_kind = EXCLUDED.collector_kind,
		  active_generation_id = EXCLUDED.active_generation_id`,
		scopeID, collectorKind, now, activeGenerationID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
}

// seedProducerScopeQuiescenceWorkItem inserts one fact_work_items row at the
// given stage and status.
func seedProducerScopeQuiescenceWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	scopeID string,
	generationID string,
	stage string,
	status string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO fact_work_items
		  (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'container_image_identity', $5, $6, $6)
		ON CONFLICT (work_item_id) DO UPDATE SET status = EXCLUDED.status`,
		workItemID, scopeID, generationID, stage, status, now,
	); err != nil {
		t.Fatalf("seed fact_work_items %s: %v", workItemID, err)
	}
}
