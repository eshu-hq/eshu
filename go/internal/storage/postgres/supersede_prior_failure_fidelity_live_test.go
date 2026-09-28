// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"
)

// pfTextCase is one stored failure_details value the fold must reproduce.
type pfTextCase struct {
	name    string
	details *string
	null    bool
}

var pfEmptyText = ""

var pfTextCases = []pfTextCase{
	{name: "plain_text", details: pfStr("phase=semantic label=Variable rows=500")},
	{name: "json_object_text", details: pfStr(`{"fact_kind":"file","nested":{"a":[1,2,3]}}`)},
	{name: "not_json_text", details: pfStr("{oops: not json")},
	{name: "quotes_and_backslashes", details: pfStr(`say "hi" \ and \\ and \" end`)},
	{name: "newlines_and_tabs", details: pfStr("line1\nline2\r\n\tindented")},
	{name: "multibyte", details: pfStr("日本語 ☃ é 🚀 مرحبا")},
	{name: "literal_backslash_u0000", details: pfStr(`before\u0000after`)},
	{name: "empty_string", details: &pfEmptyText},
	{name: "null", null: true},
	{name: "one_megabyte", details: pfStr(strings.Repeat("ab\n\"\\é☃", 1<<17))},
}

// TestSupersedeFoldsPriorFailureTextFidelity proves the old failure_details is
// embedded as a JSON string verbatim, whatever it holds. Free text, JSON text,
// quotes, backslashes, newlines, multibyte text, the six characters \u0000, an
// empty string and NULL must all round-trip, and a value that is not valid
// JSON must not abort the claim statement.
func TestSupersedeFoldsPriorFailureTextFidelity(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	for _, tc := range pfTextCases {
		scopeID := "pf-text-" + tc.name
		seed := pfDeadLetter()
		seed.details = tc.details
		pfSeedScope(t, database, scopeID, "failed", "pending")
		pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", seed)
		pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector", pfNeverFailed())
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if _, ok, err := queue.Claim(context.Background()); err != nil || !ok {
		t.Fatalf("Claim() ok=%v err=%v, want the claim to succeed over every stored text", ok, err)
	}
	for _, tc := range pfTextCases {
		t.Run(tc.name, func(t *testing.T) {
			var typ string
			var value sql.NullString
			if err := database.QueryRow(`
SELECT jsonb_typeof(failure_details::jsonb #> '{prior_failure,failure_details}'),
       failure_details::jsonb #>> '{prior_failure,failure_details}'
FROM fact_work_items WHERE work_item_id = $1`, "old-pf-text-"+tc.name).Scan(&typ, &value); err != nil {
				t.Fatalf("read prior_failure.failure_details: %v", err)
			}
			if tc.null {
				if typ != "null" || value.Valid {
					t.Fatalf("NULL details folded as (%s, %q), want JSON null", typ, value.String)
				}
				return
			}
			if typ != "string" || !value.Valid || value.String != *tc.details {
				t.Fatalf("details folded as (%s, len %d), want a JSON string of len %d, byte-identical",
					typ, len(value.String), len(*tc.details))
			}
		})
	}
}

// TestSupersedeIsTerminalForPriorFailure proves a superseded row that already
// holds prior_failure is never rewritten: no statement that supersedes work
// selects a superseded row, so the fold cannot nest or overwrite. The row is
// made as attractive as possible to each statement (lease matching, generation
// predicates matching); only its status can protect it.
func TestSupersedeIsTerminalForPriorFailure(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()

	t.Run("projector", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		scopeID := "pf-term"
		pfSeedScope(t, database, scopeID, "failed", "pending")
		pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", pfDeadLetter())
		pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector", pfNeverFailed())
		queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
		if _, ok, err := queue.Claim(ctx); err != nil || !ok {
			t.Fatalf("first Claim() ok=%v err=%v", ok, err)
		}
		if _, err := database.Exec(`UPDATE scope_generations SET status = 'failed', superseded_at = NULL
WHERE generation_id = $1`, scopeID+"-gen-old"); err != nil {
			t.Fatalf("re-arm generation: %v", err)
		}
		if _, err := database.Exec(`UPDATE fact_work_items SET lease_owner = 'claimer',
claim_until = now() + interval '1 minute' WHERE work_item_id = $1`, "old-"+scopeID); err != nil {
			t.Fatalf("re-arm lease: %v", err)
		}
		before := pfSnapshot(t, database, "old-"+scopeID)
		if !strings.Contains(before, "prior_failure") {
			t.Fatalf("precondition: superseded row has no prior_failure: %s", before)
		}
		now := time.Now().UTC().Add(time.Minute)
		for _, step := range []struct {
			name string
			run  func() error
		}{
			{"claim_sweep", func() error { _, _, err := queue.Claim(ctx); return err }},
			{"ack_obsolete", func() error {
				_, err := database.ExecContext(ctx, supersedeProjectorObsoleteGenerationsQuery, now, scopeID, scopeID+"-gen-new")
				return err
			}},
			{"heartbeat_supersede", func() error {
				_, err := database.ExecContext(ctx, supersedeRunningProjectorWorkQuery,
					now, scopeID, scopeID+"-gen-old", "claimer", 3)
				return err
			}},
			{"ack_refusal", func() error {
				_, err := database.ExecContext(ctx, markProjectorAckSupersededQuery,
					now, scopeID, scopeID+"-gen-old", "claimer", 3)
				return err
			}},
		} {
			if err := step.run(); err != nil {
				t.Fatalf("%s: %v", step.name, err)
			}
			if after := pfSnapshot(t, database, "old-"+scopeID); after != before {
				t.Fatalf("%s rewrote a superseded row:\nbefore %s\nafter  %s", step.name, before, after)
			}
		}
	})

	t.Run("reducer", func(t *testing.T) {
		database := openClaimDeadlockProofDB(t, dsn, 2)
		scopeID := "pf-term-red"
		pfSeedScope(t, database, scopeID, "superseded", "active")
		pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "reducer", pfDeadLetter())
		queue := NewReducerQueue(SQLDB{DB: database}, "reducer-claimer", time.Minute)
		if _, _, err := queue.Claim(ctx); err != nil {
			t.Fatalf("first Claim() error = %v", err)
		}
		before := pfSnapshot(t, database, "old-"+scopeID)
		if !strings.Contains(before, "prior_failure") {
			t.Fatalf("precondition: superseded reducer row has no prior_failure: %s", before)
		}
		if _, _, err := queue.Claim(ctx); err != nil {
			t.Fatalf("second Claim() error = %v", err)
		}
		if _, err := queue.ClaimBatch(ctx, 4); err != nil {
			t.Fatalf("ClaimBatch() error = %v", err)
		}
		if after := pfSnapshot(t, database, "old-"+scopeID); after != before {
			t.Fatalf("reducer claim rewrote a superseded row:\nbefore %s\nafter  %s", before, after)
		}
	})
}

// pfSnapshot returns the whole row as text, so any column change shows.
func pfSnapshot(t *testing.T, database *sql.DB, workItemID string) string {
	t.Helper()
	var row string
	if err := database.QueryRow(`SELECT to_jsonb(w)::text FROM fact_work_items AS w WHERE work_item_id = $1`,
		workItemID).Scan(&row); err != nil {
		t.Fatalf("snapshot %s: %v", workItemID, err)
	}
	return row
}

// TestSupersedeFoldsPriorFailureSeesConcurrentCommit is the EvalPlanQual proof
// for the Ack-time supersede, the statement that blocks on a work row lock.
// Session A changes a snapshot-matching retrying row to dead_letter with new
// evidence and holds. Session B runs the supersede, snapshots the old row,
// blocks on it, and resumes after A commits. Under Read Committed B re-reads
// the committed row version, so prior_failure must carry A's dead_letter
// evidence, not the retrying values B's snapshot saw.
func TestSupersedeFoldsPriorFailureSeesConcurrentCommit(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	ctx := context.Background()
	database := openClaimDeadlockProofDB(t, dsn, 4)
	scopeID := "pf-epq"
	pfSeedScope(t, database, scopeID, "failed", "pending")
	pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", pfRetrying())

	committed := pfSeed{
		status: "dead_letter", attempts: 2,
		class: pfStr("epq_committed_class"), message: pfStr("committed while B waited"),
		details: pfStr("committed details"),
	}
	txA, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	defer func() { _ = txA.Rollback() }()
	if _, err := txA.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2, failure_class = $3, failure_message = $4, failure_details = $5
WHERE work_item_id = $1`, "old-"+scopeID, committed.status, *committed.class, *committed.message, *committed.details); err != nil {
		t.Fatalf("session A update: %v", err)
	}

	var wg sync.WaitGroup
	var errB error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errB = database.ExecContext(ctx, supersedeProjectorObsoleteGenerationsQuery,
			time.Now().UTC(), scopeID, scopeID+"-gen-new")
	}()
	deadline := time.Now().Add(20 * time.Second)
	for blocked := false; !blocked; {
		if time.Now().After(deadline) {
			t.Fatal("session B never blocked on A's row lock, so the recheck was not exercised")
		}
		if err := database.QueryRow(`
SELECT EXISTS (SELECT 1 FROM pg_stat_activity
               WHERE wait_event_type = 'Lock' AND query LIKE '%superseded_work AS%'
                 AND pid <> pg_backend_pid())`).Scan(&blocked); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := txA.Commit(); err != nil {
		t.Fatalf("commit A: %v", err)
	}
	wg.Wait()
	if errB != nil {
		t.Fatalf("session B supersede: %v", errB)
	}
	pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID), pfWant{
		class: pfProjectorClass, message: pfProjectorMessage,
		detailKeys: map[string]any{
			"scope_id": scopeID, "work_item_id": "old-" + scopeID,
			"generation_id": scopeID + "-gen-old", "current_generation_id": scopeID + "-gen-new",
		},
		prior: &committed,
	}, committed)
}
