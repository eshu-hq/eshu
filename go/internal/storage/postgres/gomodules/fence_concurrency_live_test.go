// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10055 real PostgreSQL concurrency proof of the commit-skew fence against the real projector Ack and the retirement fence's table lock: both completion orders end on the successor, the fence queues behind an Ack holding the scope row without deadlock, a sustained race with holders leaves one consistent outcome, and a lock timeout rolls the retraction back with its holders
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const fenceProofLeaseOwner = "fence-proof"

// seedSuccessor writes a pending successor generation with a claimed projector
// work row: the state a committed claim is in when the projector is about to Ack.
func seedSuccessor(t *testing.T, database *sql.DB, scopeID, generationID, commit string) projector.ScopeGenerationWork {
	t.Helper()
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, source_commit_sha)
VALUES ($1, $2, 'snapshot', now(), now(), 'pending', $3)`, generationID, scopeID, commit); err != nil {
		t.Fatalf("seed successor generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    lease_owner, claim_until, last_attempt_at, payload, created_at, updated_at)
VALUES ('projector_' || $1 || '_' || $2, $1, $2, 'projector', 'source_local', 'claimed', 1,
    $3, now() + interval '5 minutes', now(), '{}'::jsonb, now(), now())`, scopeID, generationID, fenceProofLeaseOwner); err != nil {
		t.Fatalf("seed successor work: %v", err)
	}
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{ScopeID: scopeID, GenerationID: generationID},
		AttemptCount: 1,
	}
}

func proofQueue(database *sql.DB) postgres.ProjectorQueue {
	return postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, fenceProofLeaseOwner, 5*time.Minute)
}

// assertEndsOnSuccessor is the one acceptable outcome of any interleaving.
func assertEndsOnSuccessor(t *testing.T, database *sql.DB, scopeID, prior, successor string) {
	t.Helper()
	if state := readScopeState(t, database, scopeID, successor); state.pointer.String != successor || state.status != "active" {
		t.Fatalf("successor state = %+v, want it active and named by the pointer", state)
	}
	if state := readScopeState(t, database, scopeID, prior); state.status != "superseded" || !state.supersededAt.Valid {
		t.Fatalf("prior state = %+v, want it superseded", state)
	}
}

// TestSuccessorAckBeforeTheFenceMakesItANoOpLive is the first order of decision
// 4.3: the page read names the prior generation, the successor activates, and the
// fence then matches nothing and counts nothing.
func TestSuccessorAckBeforeTheFenceMakesItANoOpLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, prior := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	stale := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: prior}}

	work := seedSuccessor(t, database, scopeID, "successor", fmt.Sprintf("%040x", 1))
	if err := proofQueue(database).Ack(t.Context(), work, runtime.Result{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	result, err := fenceStore(database).FenceStaleGoEvidence(t.Context(), stale, time.Now().UTC())
	if err != nil || result.Fenced != 0 {
		t.Fatalf("result = %+v, err = %v, want 0 fenced", result, err)
	}
	assertEndsOnSuccessor(t, database, scopeID, prior, "successor")
}

// TestFenceBeforeTheSuccessorAckEndsOnItLive is the second order: the fence
// retracts first, the Ack still activates the successor, and nothing is revived.
func TestFenceBeforeTheSuccessorAckEndsOnItLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, prior := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	work := seedSuccessor(t, database, scopeID, "successor", fmt.Sprintf("%040x", 1))

	fence := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: prior}}
	result, err := fenceStore(database).FenceStaleGoEvidence(t.Context(), fence, time.Now().UTC())
	if err != nil || result.Fenced != 1 {
		t.Fatalf("result = %+v, err = %v, want 1 fenced", result, err)
	}
	if err := proofQueue(database).Ack(t.Context(), work, runtime.Result{}); err != nil {
		t.Fatalf("ack after the fence: %v", err)
	}
	assertEndsOnSuccessor(t, database, scopeID, prior, "successor")
}

// TestFenceQueuesBehindAnAckHoldingTheScopeRowLive is the deterministic lock-order
// proof. A transaction takes the scope row, as Ack's first statement does, and the
// fence starts and blocks. The transaction then takes the prior generation, as
// Ack's supersede does. A fence that took the generation first would hold it here,
// and this would be a deadlock that one side loses with an error.
func TestFenceQueuesBehindAnAckHoldingTheScopeRowLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, prior := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	seedSuccessor(t, database, scopeID, "successor", fmt.Sprintf("%040x", 1))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	ack, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = ack.Rollback() }()
	if _, err := ack.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = 'successor' WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("take the scope row: %v", err)
	}
	var ackXID string
	if err := ack.QueryRowContext(ctx, `SELECT pg_current_xact_id()::text`).Scan(&ackXID); err != nil {
		t.Fatalf("read xid: %v", err)
	}

	type outcome struct {
		result scannercomponent.StaleEvidenceFenceResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		fence := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: prior}}
		result, err := fenceStore(database).FenceStaleGoEvidence(ctx, fence, time.Now().UTC())
		done <- outcome{result, err}
	}()
	waitForBlockedOn(ctx, t, database, ackXID)

	if _, err := ack.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = $1 AND status = 'active'`, prior); err != nil {
		t.Fatalf("take the prior generation behind a waiting fence: %v", err)
	}
	if _, err := ack.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'active', activated_at = now() WHERE generation_id = 'successor'`); err != nil {
		t.Fatalf("activate the successor: %v", err)
	}
	if err := ack.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	finished := <-done
	if finished.err != nil || finished.result.Fenced != 0 {
		t.Fatalf("result = %+v, err = %v, want a no-op behind the committed ack", finished.result, finished.err)
	}
	assertEndsOnSuccessor(t, database, scopeID, prior, "successor")
}

// TestAckQueuesBehindAFenceHoldingAHolderLive is the lock-order proof for the reopen. A
// holder's work row is held, so the fence blocks mid-reopen with the scope row and the
// generation already taken. A real Ack of the successor then starts and queues on the scope
// row. Releasing the holder lets the fence commit, and the Ack then ends on the successor.
// There is no cycle because the Ack waits on the fence and the fence waits on neither the
// Ack nor anything the Ack holds. It also proves the retraction and reopen commit together.
func TestAckQueuesBehindAFenceHoldingAHolderLive(t *testing.T) {
	database := openStoreProof(t)
	repository := seedRepository(t, database, 1, "go.mod")
	scopeID, prior := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
	itemID := seedFindingHolder(t, database, "holder-held", repository, "succeeded")
	work := seedSuccessor(t, database, scopeID, "successor", fmt.Sprintf("%040x", 1))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	var held string
	if err := holder.QueryRowContext(ctx,
		`SELECT work_item_id FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, itemID).Scan(&held); err != nil {
		t.Fatalf("hold the work row: %v", err)
	}
	var holderXID string
	if err := holder.QueryRowContext(ctx, `SELECT pg_current_xact_id()::text`).Scan(&holderXID); err != nil {
		t.Fatalf("read xid: %v", err)
	}

	type outcome struct {
		result scannercomponent.StaleEvidenceFenceResult
		err    error
	}
	fenceDone := make(chan outcome, 1)
	go func() {
		fence := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: prior}}
		result, err := fenceStore(database).FenceStaleGoEvidence(ctx, fence, time.Now().UTC())
		fenceDone <- outcome{result, err}
	}()
	waitForBlockedOn(ctx, t, database, holderXID)

	ackDone := make(chan error, 1)
	go func() { ackDone <- proofQueue(database).Ack(ctx, work, runtime.Result{}) }()
	waitForBlockedBackends(ctx, t, database, 2)

	if err := holder.Commit(); err != nil {
		t.Fatalf("release the holder: %v", err)
	}
	finished := <-fenceDone
	if finished.err != nil || finished.result.Fenced != 1 || finished.result.HoldersReopened != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced and 1 holder", finished.result, finished.err)
	}
	if err := <-ackDone; err != nil {
		t.Fatalf("ack behind the fence: %v", err)
	}
	assertEndsOnSuccessor(t, database, scopeID, prior, "successor")
	if status, _ := readItem(t, database, itemID); status != "pending" {
		t.Fatalf("holder item status = %q, want pending", status)
	}
}

// waitForBlockedBackends returns once at least count backends in this database wait on a
// lock, so the next step cannot run before the waiter it depends on is really queued.
func waitForBlockedBackends(ctx context.Context, t *testing.T, database *sql.DB, count int) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var waiting int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fewer than %d backends blocked: the fence or the ack did not queue", count)
}

// waitForBlockedOn returns once a backend waits on the transaction. The wait is
// scoped to that transaction id, so an unrelated backend cannot satisfy it.
func waitForBlockedOn(ctx context.Context, t *testing.T, database *sql.DB, xid string) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var waiting int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE NOT granted AND locktype = 'transactionid' AND transactionid::text = $1`, xid).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the fence never blocked behind the transaction holding the scope row")
}

// TestFenceAndAckRaceEndOnTheSuccessorLive runs the real Ack and the real fence
// together over many rounds, each on its own scope, with one holder to reopen so the
// fence takes its full lock chain. No round may deadlock or fail, and every round must
// end on the successor whichever side won.
func TestFenceAndAckRaceEndOnTheSuccessorLive(t *testing.T) {
	database := openStoreProof(t)
	store := fenceStore(database)
	queue := proofQueue(database)
	const rounds = 40
	wins := 0
	for round := range rounds {
		repository := seedRepository(t, database, round+1, "go.mod")
		scopeID, prior := seedStampedEvidence(t, database, repository, fenceLiveEvidenceCommit, 3*fenceLiveInterval, "vulnerability.go_module_evidence")
		successor := fmt.Sprintf("successor-%d", round)
		work := seedSuccessor(t, database, scopeID, successor, fmt.Sprintf("%040x", round+1))
		fence := []scannercomponent.StaleEvidenceFence{{ScopeID: scopeID, GenerationID: prior}}
		seedFindingHolder(t, database, fmt.Sprintf("holder-%d", round), repository, "succeeded")

		var group sync.WaitGroup
		start := make(chan struct{})
		var fenced scannercomponent.StaleEvidenceFenceResult
		var fenceErr, ackErr error
		group.Add(2)
		go func() {
			defer group.Done()
			<-start
			fenced, fenceErr = store.FenceStaleGoEvidence(t.Context(), fence, time.Now().UTC())
		}()
		go func() {
			defer group.Done()
			<-start
			ackErr = queue.Ack(t.Context(), work, runtime.Result{})
		}()
		close(start)
		group.Wait()

		if fenceErr != nil {
			t.Fatalf("round %d: fence: %v", round, fenceErr)
		}
		if ackErr != nil {
			t.Fatalf("round %d: ack: %v", round, ackErr)
		}
		assertEndsOnSuccessor(t, database, scopeID, prior, successor)
		wins += fenced.Fenced
	}
	t.Logf("fence won %d of %d rounds", wins, rounds)
}
