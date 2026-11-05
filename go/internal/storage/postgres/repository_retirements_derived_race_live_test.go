// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

// derivedLiveCommit builds the commit a claimed go-reachability scan makes: the
// host passes the planner's fairness key as the partition key.
func derivedLiveCommit(repoID, generationID string) (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Now().UTC()
	scopeID := derivedLiveScopeID(repoID)
	return scope.IngestionScope{
			ScopeID: scopeID, SourceSystem: "scanner_worker", ScopeKind: scope.KindScannerWorker,
			CollectorKind: scope.CollectorScannerWorker, PartitionKey: derivedLiveFairnessKey,
			Metadata: map[string]string{"component_id": "go-reachability"},
		}, scope.ScopeGeneration{
			GenerationID: generationID, ScopeID: scopeID, ObservedAt: now, IngestedAt: now,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		}
}

func derivedLiveFact(repoID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID: generationID + "/fact", ScopeID: derivedLiveScopeID(repoID), GenerationID: generationID,
		FactKind: "vulnerability.go_module_evidence", StableFactKey: generationID + "/key",
		ObservedAt: time.Now().UTC(), Payload: map[string]any{"repository_id": repoID},
		SourceRef: facts.Ref{SourceSystem: "scanner_worker", FactKey: generationID + "/key"},
	}
}

// TestRetirementRefusesLateDerivedCommitLive covers a claim queued or running
// when the retirement lands. A scan commits minutes after it was planned, and
// its partition key never names the repository, so the commit gate must read the
// parent from the scope id. It refuses while the tombstone is open and after it
// completes, and it never stamps a readmission: only the repository's own commit
// brings a repository back. After that commit a derived scan is admitted again.
func TestRetirementRefusesLateDerivedCommitLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_gate",
		"set ESHU_POSTGRES_DSN to run the derived-scope commit gate Postgres proof")
	const (
		repo     = "repository:r_00000011"
		neighbor = "repository:r_00000012"
	)
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, false)
	seedDerivedLiveRepository(t, ctx, database, neighbor, false)
	handle := SQLDB{DB: database}
	store := NewIngestionStore(handle)

	commit := func(repoID, generationID string) {
		t.Helper()
		scopeValue, generation := derivedLiveCommit(repoID, generationID)
		stream := make(chan facts.Envelope, 1)
		stream <- derivedLiveFact(repoID, generationID)
		close(stream)
		if err := store.CommitScopeGeneration(ctx, scopeValue, generation, stream); err != nil {
			t.Fatalf("commit %s: %v (a refusal is a nil skip)", generationID, err)
		}
	}
	residue := func(repoID string) int64 {
		t.Helper()
		scopes, _, _ := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(repoID))
		return scopes
	}

	if _, err := NewRetirementStore(handle).InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_derived_gate", RepoID: repo, ScopeID: gitScope,
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "key-rr_derived_gate",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	commit(repo, "gate-while-open")
	if got := residue(repo); got != 0 {
		t.Fatalf("derived scope rows after a commit under an open tombstone = %d, want 0", got)
	}

	if _, err := NewRetirementFencer(handle, handle).FenceRepository(ctx,
		derivedLiveFenceRequest(repo, gitScope, "rr_derived_gate")); err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	purged, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
	if err != nil || len(purged.Completed) != 1 {
		t.Fatalf("PurgeRetired = %+v, %v, want one completion", purged, err)
	}
	commit(repo, "gate-after-complete")
	if got := residue(repo); got != 0 {
		t.Fatalf("derived scope rows after a commit over a complete tombstone = %d, want 0", got)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM repository_retirements
		WHERE retirement_id = 'rr_derived_gate' AND readmitted_at IS NOT NULL`); got != 0 {
		t.Fatal("a derived commit readmitted the retired repository; only its own commit may")
	}

	// Another repository's scan is untouched by this tombstone.
	commit(neighbor, "gate-neighbor")
	if got := residue(neighbor); got != 1 {
		t.Fatalf("neighbor derived scope rows = %d, want 1", got)
	}

	// The repository's own commit readmits it; a scan after that is admitted.
	now := time.Now().UTC()
	if err := store.CommitScopeGeneration(ctx, scope.IngestionScope{
		ScopeID: gitScope, SourceSystem: "git", ScopeKind: scope.KindRepository,
		CollectorKind: scope.CollectorGit, PartitionKey: repo,
		Metadata: map[string]string{"repo_id": repo},
	}, scope.ScopeGeneration{
		GenerationID: "gate-readmit", ScopeID: gitScope, ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}, nil); err != nil {
		t.Fatalf("readmitting commit: %v", err)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM repository_retirements
		WHERE retirement_id = 'rr_derived_gate' AND readmitted_at IS NOT NULL`); got != 1 {
		t.Fatalf("readmitted tombstones = %d, want the repository's own commit to stamp it", got)
	}
	commit(repo, "gate-after-readmit")
	if got := residue(repo); got != 1 {
		t.Fatalf("derived scope rows after readmission = %d, want 1", got)
	}
}

// TestRetirementWaitsForInFlightFirstDerivedCommitLive is the race the gate
// alone cannot close: a repository's first scan passed the gate before the mark
// committed and is still streaming facts. Its scope row is uncommitted, so the
// fence's row locks cannot see it. Only the shared advisory key on the parent
// makes the fence wait, so the row is visible when the fence reads and the purge
// removes it. Without that key this commit lands after the purge and the derived
// scope outlives the repository.
func TestRetirementWaitsForInFlightFirstDerivedCommitLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_race",
		"set ESHU_POSTGRES_DSN to run the derived-scope commit race Postgres proof")
	const repo = "repository:r_00000013"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, false)
	handle := SQLDB{DB: database}

	scopeValue, generation := derivedLiveCommit(repo, "race-first-scan")
	stream := make(chan facts.Envelope)
	commitDone := make(chan error, 1)
	go func() {
		commitDone <- NewIngestionStore(handle).CommitScopeGeneration(ctx, scopeValue, generation, stream)
	}()
	// The store reads the stream only after the gate admits the scope, so a
	// received fact means the commit is past the gate with its transaction open.
	stream <- derivedLiveFact(repo, "race-first-scan")

	if _, err := NewRetirementStore(handle).InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_derived_race", RepoID: repo, ScopeID: gitScope,
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "key-rr_derived_race",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	fenceDone := make(chan error, 1)
	go func() {
		_, err := NewRetirementFencer(handle, handle).FenceRepository(ctx,
			derivedLiveFenceRequest(repo, gitScope, "rr_derived_race"))
		fenceDone <- err
	}()

	var fenceErr error
	fenceFinished := false
	deadline := time.After(20 * time.Second)
	for waiting := false; !waiting && !fenceFinished; {
		select {
		case fenceErr = <-fenceDone:
			fenceFinished = true
		case <-deadline:
			t.Fatal("the fence neither finished nor queued on the advisory key within 20s")
		case <-time.After(50 * time.Millisecond):
			waiting = liveCount(t, ctx, database, `SELECT count(*) FROM pg_locks
				WHERE locktype = 'advisory' AND mode = 'ExclusiveLock' AND NOT granted`) > 0
		}
	}

	if fenceFinished {
		// Unfixed order: the fence is already done, so the purge finishes while
		// the first scan is still open, and that scan then lands after it.
		if fenceErr != nil {
			t.Fatalf("FenceRepository: %v", fenceErr)
		}
		if _, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}}); err != nil {
			t.Fatalf("PurgeRetired: %v", err)
		}
	}
	close(stream)
	if err := <-commitDone; err != nil {
		t.Fatalf("in-flight commit: %v", err)
	}
	if !fenceFinished {
		if err := <-fenceDone; err != nil {
			t.Fatalf("FenceRepository: %v", err)
		}
		purged, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
		if err != nil || len(purged.Completed) != 1 {
			t.Fatalf("PurgeRetired = %+v, %v, want one completion", purged, err)
		}
	}
	if scopes, generations, factRows := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(repo)); scopes+generations+factRows != 0 {
		t.Fatalf("derived residue after the race = %d scopes, %d generations, %d facts, want none", scopes, generations, factRows)
	}
}

// commitDerivedLive commits one claimed scan's generation and its one fact.
func commitDerivedLive(ctx context.Context, store IngestionStore, repoID, generationID string) error {
	scopeValue, generation := derivedLiveCommit(repoID, generationID)
	stream := make(chan facts.Envelope, 1)
	stream <- derivedLiveFact(repoID, generationID)
	close(stream)
	return store.CommitScopeGeneration(ctx, scopeValue, generation, stream)
}

// TestRetirementPurgeBusyWhileAckHoldsDerivedScopeLive holds the derived scope row
// the way a projector Ack's first statement does. The purge must not queue behind
// it: NOWAIT reports scope_lock_busy with nothing written and the tombstone still
// fenced, and the retry after the holder leaves completes.
func TestRetirementPurgeBusyWhileAckHoldsDerivedScopeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_busy",
		"set ESHU_POSTGRES_DSN to run the derived-scope lock contention Postgres proof")
	const repo = "repository:r_00000020"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, true)
	handle := SQLDB{DB: database}
	if _, err := NewRetirementFencer(handle, handle).FenceRepository(ctx,
		derivedLiveFenceRequest(repo, gitScope, "rr_derived_busy")); err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, `SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE`,
		derivedLiveScopeID(repo)); err != nil {
		t.Fatalf("hold derived scope: %v", err)
	}

	purger := NewRetirementPurger(handle, handle)
	busy, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
	if err != nil {
		t.Fatalf("PurgeRetired under the holder: %v", err)
	}
	if len(busy.Blocked) != 1 || busy.Blocked[0].Reason != PurgeReasonScopeLockBusy {
		t.Fatalf("purge under the holder = %+v, want one scope_lock_busy", busy)
	}
	if scopes, _, factRows := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(repo)); scopes != 1 || factRows != 1 {
		t.Fatalf("blocked purge wrote: %d scopes, %d facts, want the row kept", scopes, factRows)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatalf("release holder: %v", err)
	}

	done, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
	if err != nil || len(done.Completed) != 1 {
		t.Fatalf("retry = %+v, %v, want one completion", done, err)
	}
	if scopes, generations, factRows := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(repo)); scopes+generations+factRows != 0 {
		t.Fatalf("residue after retry = %d/%d/%d, want none", scopes, generations, factRows)
	}
}

// TestRetirementFenceWaitsForLiveDerivedProjectorLeaseLive covers a projector run
// holding the derived scope's work. The fence drains it rather than racing it, so
// the Ack lands first, then the fence moves the generation it activated, then the
// purge removes the scope.
func TestRetirementFenceWaitsForLiveDerivedProjectorLeaseLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_lease",
		"set ESHU_POSTGRES_DSN to run the derived-scope projector lease Postgres proof")
	const repo = "repository:r_00000021"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, false)
	handle := SQLDB{DB: database}
	store := NewIngestionStore(handle)
	if err := commitDerivedLive(ctx, store, repo, "lease-gen"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	queue := NewProjectorQueue(handle, "derived-lease-owner", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Scope.ScopeID != derivedLiveScopeID(repo) {
		t.Fatalf("Claim = %+v, %v, %v, want the derived work", work.Scope, ok, err)
	}

	fenceDone := make(chan error, 1)
	go func() {
		_, err := NewRetirementFencer(handle, handle).FenceRepository(ctx,
			derivedLiveFenceRequest(repo, gitScope, "rr_derived_lease"))
		fenceDone <- err
	}()
	select {
	case err := <-fenceDone:
		t.Fatalf("the fence finished over a live derived lease: %v", err)
	case <-time.After(750 * time.Millisecond):
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack under the drain: %v", err)
	}
	if err := <-fenceDone; err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM scope_generations
		WHERE scope_id = $1 AND status <> 'superseded'`, derivedLiveScopeID(repo)); got != 0 {
		t.Fatalf("live derived generations after the fence = %d, want 0", got)
	}
	purged, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
	if err != nil || len(purged.Completed) != 1 {
		t.Fatalf("PurgeRetired = %+v, %v, want one completion", purged, err)
	}
	if scopes, generations, factRows := derivedLiveScopeResidue(t, ctx, database, derivedLiveScopeID(repo)); scopes+generations+factRows != 0 {
		t.Fatalf("residue = %d/%d/%d, want none", scopes, generations, factRows)
	}
}

// TestRetirementRefusesStaleDerivedAckAfterFenceLive covers a projector worker
// whose lease expired before the fence. The fence marks its row, and its late Ack
// must be refused rather than reactivate evidence of a retiring repository.
func TestRetirementRefusesStaleDerivedAckAfterFenceLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_stale_ack",
		"set ESHU_POSTGRES_DSN to run the derived-scope stale Ack Postgres proof")
	const repo = "repository:r_00000022"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, false)
	handle := SQLDB{DB: database}
	if err := commitDerivedLive(ctx, NewIngestionStore(handle), repo, "stale-gen"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	queue := NewProjectorQueue(handle, "derived-stale-owner", time.Minute)
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim = %v, %v", ok, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_work_items SET claim_until = NOW() - INTERVAL '1 minute'
		WHERE scope_id = $1 AND generation_id = 'stale-gen'`, derivedLiveScopeID(repo)); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if _, err := NewRetirementFencer(handle, handle).FenceRepository(ctx,
		derivedLiveFenceRequest(repo, gitScope, "rr_derived_stale")); err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err == nil {
		t.Fatal("a stale Ack after the fence succeeded")
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM ingestion_scopes
		WHERE scope_id = $1 AND active_generation_id IS NOT NULL`, derivedLiveScopeID(repo)); got != 0 {
		t.Fatalf("active derived pointers after the stale Ack = %d, want 0", got)
	}
	purged, err := NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
	if err != nil || len(purged.Completed) != 1 {
		t.Fatalf("PurgeRetired = %+v, %v, want one completion", purged, err)
	}
}

// TestRetirementDerivedScopeFenceAndPurgeAreIdempotentLive repeats the fence under
// the operator's same key, then races two purges. Both report completion, and the
// ledger counts the derived scope once.
func TestRetirementDerivedScopeFenceAndPurgeAreIdempotentLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_idem",
		"set ESHU_POSTGRES_DSN to run the derived-scope idempotency Postgres proof")
	const repo = "repository:r_00000023"
	gitScope := seedDerivedLiveRepository(t, ctx, database, repo, true)
	handle := SQLDB{DB: database}
	fencer := NewRetirementFencer(handle, handle)
	request := derivedLiveFenceRequest(repo, gitScope, "rr_derived_idem")
	if _, err := fencer.FenceRepository(ctx, request); err != nil {
		t.Fatalf("first fence: %v", err)
	}
	again, err := fencer.FenceRepository(ctx, request)
	if err != nil {
		t.Fatalf("second fence: %v", err)
	}
	if again.GenerationsFenced != 0 {
		t.Fatalf("second fence moved %d generations, want 0", again.GenerationsFenced)
	}

	var wg sync.WaitGroup
	results := make([]PurgeResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = NewRetirementPurger(handle, handle).PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{repo}})
		}(i)
	}
	wg.Wait()
	completions := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("purge %d: %v", i, errs[i])
		}
		completions += len(results[i].Completed)
	}
	if completions == 0 {
		t.Fatalf("racing purges = %+v, want at least one completion", results)
	}
	if _, ledger := derivedLiveLedger(t, ctx, database, "rr_derived_idem"); ledger["derived_ingestion_scopes"] != 1 {
		t.Fatalf("ledger = %v, want the derived scope counted once", ledger)
	}
}

// TestDerivedCommitAndMaintenanceLeaderDoNotDeadlockLive is the lock-order proof
// for the parent key. A leader takes exclusive keys in sorted order, parent before
// fairness key. A commit taking its fairness key first would hold it while waiting
// for the parent, and Postgres would report 40P01 within its deadlock timeout.
func TestDerivedCommitAndMaintenanceLeaderDoNotDeadlockLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_derived_deadlock",
		"set ESHU_POSTGRES_DSN to run the derived-scope barrier lock-order Postgres proof")
	handle := SQLDB{DB: database}
	store := NewIngestionStore(handle)
	repos := []string{"repository:r_00000024", "repository:r_00000025", "repository:r_00000026"}
	const rounds = 25

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	failures := make(chan error, (len(repos)+1)*rounds)
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			for round := range rounds {
				if err := commitDerivedLive(ctx, store, repo, fmt.Sprintf("%s-gen-%d", repo, round)); err != nil {
					failures <- fmt.Errorf("commit %s round %d: %w", repo, round, err)
				}
			}
		}(repo)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		keys := append([]string{derivedLiveFairnessKey}, repos...)
		for round := range rounds {
			if err := holdMaintenanceKeys(ctx, database, keys); err != nil {
				failures <- fmt.Errorf("leader round %d: %w", round, err)
			}
		}
	}()
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

// holdMaintenanceKeys takes the exclusive keys the way a maintenance leader does,
// holds them briefly, and releases them.
func holdMaintenanceKeys(ctx context.Context, database *sql.DB, keys []string) error {
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(ctx, tx, keys); err != nil {
		return err
	}
	time.Sleep(time.Millisecond)
	return tx.Commit()
}
