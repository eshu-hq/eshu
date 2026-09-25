// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// claimMaintenanceProofDSN returns the disposable database DSN or skips.
func claimMaintenanceProofDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN to a disposable Postgres database")
	}
	return dsn
}

// seedClaimMaintenanceScopes inserts one git scope per id.
func seedClaimMaintenanceScopes(t *testing.T, database *sql.DB, scopeIDs ...string) {
	t.Helper()
	for _, scopeID := range scopeIDs {
		if _, err := database.Exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active')
`, scopeID); err != nil {
			t.Fatalf("insert scope %s: %v", scopeID, err)
		}
	}
}

// seedClaimMaintenanceWork inserts one generation and its projector work row.
// age orders ingested_at and updated_at; claimUntil is relative to now.
func seedClaimMaintenanceWork(
	t *testing.T,
	database *sql.DB,
	scopeID, generationID, generationStatus, workStatus string,
	age time.Duration,
	claimUntil *time.Duration,
) {
	t.Helper()
	at := time.Now().UTC().Add(-age)
	if _, err := database.Exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ($1, $2, 'push', $3, $3, $4)
`, generationID, scopeID, at, generationStatus); err != nil {
		t.Fatalf("insert generation %s: %v", generationID, err)
	}
	var until any
	owner := any(nil)
	if claimUntil != nil {
		until = time.Now().UTC().Add(*claimUntil)
		owner = "other-worker"
	}
	if _, err := database.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at, last_attempt_at
) VALUES ($1, $2, $3, 'projector', 'source_local', $4, 1, $5, $6, $7,
          '{}'::jsonb, $7, $7, $7)
`, projectorWorkItemID(scopeID, generationID), scopeID, generationID, workStatus, owner, until, at); err != nil {
		t.Fatalf("insert work %s: %v", generationID, err)
	}
}

// workState reads status, failure_class, and lease owner for one work row.
func workState(t *testing.T, database *sql.DB, scopeID, generationID string) (string, string, string) {
	t.Helper()
	var status string
	var failureClass, owner sql.NullString
	if err := database.QueryRow(`
SELECT status, failure_class, lease_owner FROM fact_work_items WHERE work_item_id = $1
`, projectorWorkItemID(scopeID, generationID)).Scan(&status, &failureClass, &owner); err != nil {
		t.Fatalf("read work %s: %v", generationID, err)
	}
	return status, failureClass.String, owner.String
}

func generationState(t *testing.T, database *sql.DB, generationID string) string {
	t.Helper()
	var status string
	if err := database.QueryRow(`SELECT status FROM scope_generations WHERE generation_id = $1`,
		generationID).Scan(&status); err != nil {
		t.Fatalf("read generation %s: %v", generationID, err)
	}
	return status
}

// TestProjectorClaimMaintenanceSemantics enumerates what the claim statement's
// maintenance branches must still do after the lock-first rewrite (#7108):
// supersede stale generations (including dead-lettered ones), reclaim an
// expired duplicate lease beside a live one, and reclaim expired siblings of
// the claimed scope.
func TestProjectorClaimMaintenanceSemantics(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()

	t.Run("supersedes_stale_generations_and_claims_newest", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		seedClaimMaintenanceScopes(t, database, "scope-s")
		seedClaimMaintenanceWork(t, database, "scope-s", "gen-1", "pending", "pending", 3*time.Hour, nil)
		seedClaimMaintenanceWork(t, database, "scope-s", "gen-2", "failed", "dead_letter", 2*time.Hour, nil)
		seedClaimMaintenanceWork(t, database, "scope-s", "gen-3", "pending", "pending", time.Hour, nil)
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = (%v, %v), want claimed work", ok, err)
		}
		if got, want := work.Generation.GenerationID, "gen-3"; got != want {
			t.Fatalf("claimed generation = %q, want newest %q", got, want)
		}
		for _, gen := range []string{"gen-1", "gen-2"} {
			status, class, _ := workState(t, database, "scope-s", gen)
			if status != "superseded" || class != "projector_superseded_by_newer_generation" {
				t.Fatalf("%s work = (%s, %s), want superseded", gen, status, class)
			}
			if got := generationState(t, database, gen); got != "superseded" {
				t.Fatalf("%s generation status = %q, want superseded", gen, got)
			}
		}
	})

	t.Run("reclaims_expired_duplicate_beside_live_lease", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		seedClaimMaintenanceScopes(t, database, "scope-d")
		seedClaimMaintenanceWork(t, database, "scope-d", "gen-1", "pending", "claimed", 2*time.Hour, durationPtr(-time.Minute))
		seedClaimMaintenanceWork(t, database, "scope-d", "gen-2", "active", "running", time.Hour, durationPtr(time.Minute))
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

		if _, ok, err := queue.Claim(ctx); err != nil || ok {
			t.Fatalf("Claim() = (%v, %v), want no claim while the scope has a live lease", ok, err)
		}
		status, class, owner := workState(t, database, "scope-d", "gen-1")
		if status != "retrying" || class != "projector_stale_scope_reclaim" || owner != "" {
			t.Fatalf("expired duplicate = (%s, %s, %q), want retrying reclaim without owner", status, class, owner)
		}
		if status, _, owner := workState(t, database, "scope-d", "gen-2"); status != "running" || owner != "other-worker" {
			t.Fatalf("live lease = (%s, %q), want untouched running", status, owner)
		}
	})

	t.Run("reclaims_expired_siblings_of_claimed_scope", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		seedClaimMaintenanceScopes(t, database, "scope-c")
		seedClaimMaintenanceWork(t, database, "scope-c", "gen-1", "active", "running", 2*time.Hour, durationPtr(-2*time.Minute))
		seedClaimMaintenanceWork(t, database, "scope-c", "gen-2", "pending", "claimed", time.Hour, durationPtr(-time.Minute))
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)

		work, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim() = (%v, %v), want reclaimed expired lease", ok, err)
		}
		var sibling string
		switch work.Generation.GenerationID {
		case "gen-1":
			sibling = "gen-2"
		case "gen-2":
			sibling = "gen-1"
		default:
			t.Fatalf("claimed unexpected generation %q", work.Generation.GenerationID)
		}
		status, class, owner := workState(t, database, "scope-c", sibling)
		if status != "retrying" || class != "projector_stale_scope_reclaim" || owner != "" {
			t.Fatalf("expired sibling = (%s, %s, %q), want retrying reclaim", status, class, owner)
		}
	})
}
