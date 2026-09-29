// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// wideFailure is a non-retryable cause that carries its own details, the shape
// GraphWriteTimeoutError has, at a width the writer must bound.
type wideFailure struct{ details string }

func (e wideFailure) Error() string          { return "neo4j execute group timed out" }
func (e wideFailure) FailureClass() string   { return "graph_write_timeout" }
func (e wideFailure) FailureDetails() string { return e.details }

// TestFailBoundsWideDetailsAndSupersedeFoldCopiesTheStoredString is the #7407
// end-to-end proof against real PostgreSQL (set
// ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN). A Fail with 10 kB of details stores
// at most MaxFailureDetailsBytes ending in the truncation marker, and when a
// claim later supersedes that dead-lettered row, prior_failure.failure_details
// is the stored string byte for byte: the bound sits at the writer, so the
// evidence and its copy stay equal.
func TestFailBoundsWideDetailsAndSupersedeFoldCopiesTheStoredString(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)

	const scopeID = "bound-scope"
	const oldGeneration, newGeneration = scopeID + "-gen-old", scopeID + "-gen-new"
	const oldWorkID = "old-" + scopeID
	pfSeedScope(t, database, scopeID, "pending", "pending")
	claimed := 5 * time.Minute
	pfInsertWork(t, database, oldWorkID, scopeID, oldGeneration, "projector",
		pfWithLease(pfSeed{attempts: 1}, "claimed", "claimer", claimed))
	pfInsertWork(t, database, "new-"+scopeID, scopeID, newGeneration, "projector", pfNeverFailed())

	original := strings.Repeat("wide-", 2000) // 10,000 bytes
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if err := queue.Fail(context.Background(), pfWork(scopeID, oldGeneration, 1), wideFailure{details: original}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	stored := readFailureDetails(t, database, oldWorkID)
	if len(stored) > queuestore.MaxFailureDetailsBytes {
		t.Fatalf("stored failure_details = %d bytes, limit %d", len(stored), queuestore.MaxFailureDetailsBytes)
	}
	if !strings.Contains(stored, "...[truncated: 10000 bytes, kept ") || !strings.HasSuffix(stored, "]") {
		t.Fatalf("stored failure_details carries no marker naming the 10000-byte original: ...%s", tail(stored))
	}

	// Re-arm the scope so the newer generation's pending row is claimable; a
	// dead-letter Fail marks the scope failed, which is Fail's own effect and
	// not what this proof is about.
	if _, err := database.Exec(`UPDATE ingestion_scopes SET status = 'active' WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("re-arm scope: %v", err)
	}
	if _, ok, err := queue.Claim(context.Background()); err != nil || !ok {
		t.Fatalf("Claim() ok=%v err=%v, want the newer generation claimed and the older swept", ok, err)
	}

	row := pfRead(t, database, oldWorkID)
	if row.status != "superseded" {
		t.Fatalf("old row status = %q, want superseded", row.status)
	}
	prior, _ := row.details["prior_failure"].(map[string]any)
	if got, _ := prior["failure_details"].(string); got != stored {
		t.Fatalf("prior_failure.failure_details differs from the stored string:\n got  %d bytes ...%.60s\n want %d bytes ...%.60s",
			len(got), tail(got), len(stored), tail(stored))
	}
	if got, _ := prior["failure_class"].(string); got != "graph_write_timeout" {
		t.Fatalf("prior_failure.failure_class = %q, want the class unchanged by the bound", got)
	}
}

func readFailureDetails(t *testing.T, database *sql.DB, workItemID string) string {
	t.Helper()
	var details sql.NullString
	if err := database.QueryRow(`SELECT failure_details FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&details); err != nil {
		t.Fatalf("read failure_details of %s: %v", workItemID, err)
	}
	return details.String
}

func tail(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[len(s)-60:]
}
