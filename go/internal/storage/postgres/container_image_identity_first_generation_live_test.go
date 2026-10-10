// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestContainerImageIdentityFirstGenerationEpochBarrierLive holds the first
// projector Ack while its already-enqueued reducer intent executes. The
// pending generation has no active predecessor and no activation epoch.
func TestContainerImageIdentityFirstGenerationEpochBarrierLive(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		expireLease      bool
		expectedAttempts int
	}{
		{name: "readiness_only", expectedAttempts: 1},
		{name: "expired_lease", expireLease: true, expectedAttempts: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runFirstGenerationEpochBarrier(t, scenario.expireLease, scenario.expectedAttempts)
		})
	}
}

func runFirstGenerationEpochBarrier(t *testing.T, expireLease bool, expectedAttempts int) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("epoch proof DSN requires ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID        = "repository:7916-first-generation-barrier"
		generationID   = "generation:7916-first-generation-barrier"
		owner          = "reducer-7916-first-generation-barrier"
		projectorOwner = "projector-7916-first-generation-barrier"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status
) VALUES ($1, $2, 'synthetic', FALSE, clock_timestamp(), clock_timestamp(), 'pending')
`, generationID, scopeID); err != nil {
		t.Fatalf("seed pending first generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'projector', 'source_local', 'running',
          1, $4, clock_timestamp() + INTERVAL '2 minutes', '{}'::jsonb,
          clock_timestamp(), clock_timestamp())
`, projectorWorkItemID(scopeID, generationID), scopeID, generationID, projectorOwner); err != nil {
		t.Fatalf("seed claimed projector work: %v", err)
	}

	now := time.Now().UTC()
	queue := ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, RetryDelay: time.Second,
		MaxAttempts: 3, ClaimDomains: []reducer.Domain{reducer.DomainContainerImageIdentity},
		Now: func() time.Time { return now },
	}
	work := runtime.ReducerIntent{
		ScopeID: scopeID, GenerationID: generationID,
		Domain: reducer.DomainContainerImageIdentity, EntityKey: "identity:7916",
		Reason: "first-generation barrier proof", FactID: "fact:7916",
		SourceSystem: "git",
	}
	if result, err := queue.Enqueue(ctx, []runtime.ReducerIntent{work}); err != nil || result.Count != 1 {
		t.Fatalf("enqueue first-generation intent = (%+v, %v), want one", result, err)
	}
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim before projector Ack = (%v, %v), want claimed", claimed, err)
	}
	if intent.GenerationID != generationID {
		t.Fatalf("claimed generation = %q, want %q", intent.GenerationID, generationID)
	}
	store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	writeCalls := new(int)
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader:      epochBarrierFactLoader{},
		Writer:          epochBarrierWriter{store: store, writes: writeCalls},
		GenerationCheck: NewGenerationFreshnessCheck(SQLDB{DB: db}),
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "before_reducer")
	_, handleErr := handler.Handle(ctx, intent)
	var pending reducercontract.GenerationNotYetActiveError
	if !errors.As(handleErr, &pending) {
		t.Errorf("pending first-generation Handle() error = %v, want GenerationNotYetActiveError", handleErr)
	}
	if handleErr == nil {
		t.Fatal("pending first-generation Handle() succeeded, want deferral")
	}
	if err := queue.Fail(ctx, intent, handleErr); err != nil {
		t.Fatalf("Fail before projector Ack: %v", err)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "after_reducer_fail")
	if status, class, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "retrying" ||
		class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 1 {
		t.Errorf("pre-Ack outcome = (%s, %s, %d), want (retrying, %s, 1)",
			status, class, attempts, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	if *writeCalls != 0 {
		t.Fatalf("pre-Ack writes = %d, want zero", *writeCalls)
	}
	if result, err := queue.Enqueue(ctx, []runtime.ReducerIntent{work}); err != nil || result.Count != 0 {
		t.Fatalf("duplicate pending Enqueue = (%+v, %v), want zero new rows", result, err)
	}
	var reducerRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer'`, scopeID).Scan(&reducerRows); err != nil || reducerRows != 1 {
		t.Fatalf("reducer rows after pending duplicate Enqueue = (%d, %v), want one", reducerRows, err)
	}

	if expireLease {
		// A claimed retry is real execution, so reclaiming its expired lease
		// spends one attempt. Readiness retries after that still freeze at two.
		now = now.Add(time.Minute)
		stale, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed || stale.IntentID != intent.IntentID || stale.AttemptCount != 1 {
			t.Fatalf("claim before lease expiry = (%+v, %v, %v), want same intent at attempt 1", stale, claimed, err)
		}
		now = now.Add(2 * time.Minute)
		reclaimed, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed || reclaimed.IntentID != intent.IntentID || reclaimed.AttemptCount != expectedAttempts ||
			reclaimed.ClaimEpoch <= stale.ClaimEpoch {
			t.Fatalf("claim after lease expiry = (%+v, %v, %v), want same intent at attempt %d with newer epoch than %d",
				reclaimed, claimed, err, expectedAttempts, stale.ClaimEpoch)
		}
		if err := queue.Ack(ctx, stale, reducer.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
			t.Fatalf("stale Ack = %v, want ErrReducerClaimRejected", err)
		}
		if err := queue.Fail(ctx, stale, handleErr); !errors.Is(err, ErrReducerClaimRejected) {
			t.Fatalf("stale Fail = %v, want ErrReducerClaimRejected", err)
		}
		if status, class, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "claimed" ||
			class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != expectedAttempts || *writeCalls != 0 {
			t.Fatalf("after stale settlements = (%s, %s, %d, %d writes), want claimed/%s/%d/0",
				status, class, attempts, *writeCalls, reducercontract.GenerationActivationNotReadyFailureClass, expectedAttempts)
		}
		_, reclaimedErr := handler.Handle(ctx, reclaimed)
		if !errors.As(reclaimedErr, &pending) {
			t.Fatalf("reclaimed pending Handle() error = %v, want GenerationNotYetActiveError", reclaimedErr)
		}
		if err := queue.Fail(ctx, reclaimed, reclaimedErr); err != nil {
			t.Fatalf("reclaimed pending Fail: %v", err)
		}
		if status, class, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "retrying" ||
			class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != expectedAttempts || *writeCalls != 0 {
			t.Fatalf("reclaimed pending settlement = (%s, %s, %d, %d writes), want retrying/%s/%d/0",
				status, class, attempts, *writeCalls, reducercontract.GenerationActivationNotReadyFailureClass, expectedAttempts)
		}
		assertNoFirstGenerationReducerDeadLetters(t, ctx, db, scopeID)
	}

	// More retries than MaxAttempts must leave the readiness deferral non-counting.
	for cycle := 0; cycle < 4; cycle++ {
		now = now.Add(time.Minute)
		retry, retryClaimed, claimErr := queue.Claim(ctx)
		if claimErr != nil || !retryClaimed || retry.IntentID != intent.IntentID {
			t.Fatalf("pre-Ack retry %d claim = (%+v, %v, %v), want same intent", cycle, retry, retryClaimed, claimErr)
		}
		_, retryErr := handler.Handle(ctx, retry)
		if !errors.As(retryErr, &pending) {
			t.Fatalf("pre-Ack retry %d error = %v, want GenerationNotYetActiveError", cycle, retryErr)
		}
		if err := queue.Fail(ctx, retry, retryErr); err != nil {
			t.Fatalf("pre-Ack retry %d Fail: %v", cycle, err)
		}
		if status, class, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "retrying" ||
			class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != expectedAttempts || *writeCalls != 0 {
			t.Fatalf("pre-Ack retry %d = (%s, %s, %d, %d writes), want retrying/%s/%d/0",
				cycle, status, class, attempts, *writeCalls, reducercontract.GenerationActivationNotReadyFailureClass, expectedAttempts)
		}
		logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "repeated_deferral")
	}

	projectorQueue := NewProjectorQueue(SQLDB{DB: db}, projectorOwner, time.Minute)
	if err := projectorQueue.Ack(ctx, projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID, ScopeID: scopeID},
		AttemptCount: 1,
	}, runtime.Result{}); err != nil {
		t.Fatalf("ProjectorQueue.Ack(first generation): %v", err)
	}
	epoch, err := store.ContainerImageIdentityActivationEpoch(ctx, scopeID, generationID)
	if err != nil || epoch <= 0 {
		t.Fatalf("epoch after projector Ack = (%d, %v), want positive", epoch, err)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "after_projector_ack")
	now = now.Add(time.Minute)
	replayed, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Errorf("reclaim after projector Ack = (%v, %v), want same intent", claimed, err)
		return
	}
	if replayed.IntentID != intent.IntentID {
		t.Fatalf("reclaimed intent = %q, want %q", replayed.IntentID, intent.IntentID)
	}
	result, err := handler.Handle(ctx, replayed)
	if err != nil {
		t.Fatalf("Handle after projector Ack: %v", err)
	}
	if err := queue.Ack(ctx, replayed, result); err != nil {
		t.Fatalf("Ack replayed reducer intent: %v", err)
	}
	if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "succeeded" || attempts != expectedAttempts || *writeCalls != 1 {
		t.Fatalf("post-Ack reducer = (%s, %d attempts, %d writes), want succeeded/%d/1", status, attempts, *writeCalls, expectedAttempts)
	}
	if result, err := queue.Enqueue(ctx, []runtime.ReducerIntent{work}); err != nil || result.Count != 0 {
		t.Fatalf("duplicate terminal Enqueue = (%+v, %v), want zero new rows", result, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer'`, scopeID).Scan(&reducerRows); err != nil || reducerRows != 1 {
		t.Fatalf("reducer rows after duplicate Enqueue = (%d, %v), want one", reducerRows, err)
	}
	if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "succeeded" || attempts != expectedAttempts {
		t.Fatalf("reducer after duplicate Enqueue = (%s, %d attempts), want succeeded/%d", status, attempts, expectedAttempts)
	}
	now = now.Add(time.Minute)
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("claim after duplicate terminal Enqueue = (%v, %v), want no work", claimed, err)
	}
	if *writeCalls != 1 {
		t.Fatalf("writer calls after duplicate Enqueue = %d, want one", *writeCalls)
	}
	assertNoFirstGenerationReducerDeadLetters(t, ctx, db, scopeID)
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "after_reducer_ack")
}

func firstGenerationBarrierOutcome(t *testing.T, ctx context.Context, db *sql.DB, workID string) (string, string, int) {
	t.Helper()
	var status string
	var class sql.NullString
	var attempts int
	if err := db.QueryRowContext(ctx, `
SELECT status, failure_class, attempt_count FROM fact_work_items WHERE work_item_id = $1
`, workID).Scan(&status, &class, &attempts); err != nil {
		t.Fatalf("read first-generation reducer outcome: %v", err)
	}
	return status, class.String, attempts
}

func logFirstGenerationBarrierSnapshot(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, workID, stage string) {
	t.Helper()
	var active, generationStatus, workStatus, failureClass sql.NullString
	var attemptCount, deadLetters int
	var epoch sql.NullInt64
	var at time.Time
	if err := db.QueryRowContext(ctx, `
SELECT clock_timestamp(), scope.active_generation_id, generation.status,
       state.activation_epoch, work.status, work.failure_class, work.attempt_count,
       (SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer' AND status = 'dead_letter')
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.scope_id = scope.scope_id AND generation.generation_id = $2
JOIN fact_work_items AS work ON work.work_item_id = $3
LEFT JOIN container_image_identity_scope_state AS state ON state.scope_id = scope.scope_id
WHERE scope.scope_id = $1
`, scopeID, generationID, workID).Scan(&at, &active, &generationStatus, &epoch, &workStatus, &failureClass, &attemptCount, &deadLetters); err != nil {
		t.Fatalf("snapshot %s: %v", stage, err)
	}
	t.Logf("barrier=%s db_time=%s active=%q generation=%q epoch=%d epoch_valid=%t work=%q class=%q attempts=%d dead_letters=%d",
		stage, at.Format(time.RFC3339Nano), active.String, generationStatus.String, epoch.Int64, epoch.Valid,
		workStatus.String, failureClass.String, attemptCount, deadLetters)
}

// TestContainerImageIdentityFirstGenerationFailureExitsLive proves that a
// deferred first-generation intent terminates when its projector fails.
func TestContainerImageIdentityFirstGenerationFailureExitsLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("epoch proof DSN requires disposable=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	const scopeID = "repository:7916-failed-probe"
	const generationID = "generation:7916-failed-probe"
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ($1, $2, 'synthetic', FALSE, clock_timestamp(), clock_timestamp(), 'pending')
`, generationID, scopeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
  attempt_count, lease_owner, claim_until, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'projector', 'source_local', 'running',
  1, 'projector-7916-failed-probe', clock_timestamp() + INTERVAL '2 minutes', '{}'::jsonb,
  clock_timestamp(), clock_timestamp())
`, projectorWorkItemID(scopeID, generationID), scopeID, generationID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queue := ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: "reducer-7916-failed-probe",
		LeaseDuration: time.Minute, RetryDelay: time.Second, MaxAttempts: 3,
		ClaimDomains: []reducer.Domain{reducer.DomainContainerImageIdentity}, Now: func() time.Time { return now },
	}
	work := runtime.ReducerIntent{
		ScopeID: scopeID, GenerationID: generationID,
		Domain: reducer.DomainContainerImageIdentity, EntityKey: "identity:7916-failed-probe",
		Reason: "failed-firstgen probe", FactID: "fact:7916-failed-probe", SourceSystem: "git",
	}
	if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{work}); err != nil {
		t.Fatal(err)
	}
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim = (%v, %v)", claimed, err)
	}
	store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	writeCalls := new(int)
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader: epochBarrierFactLoader{}, Writer: epochBarrierWriter{store: store, writes: writeCalls},
		GenerationCheck: NewGenerationFreshnessCheck(SQLDB{DB: db}),
	}
	_, firstErr := handler.Handle(ctx, intent)
	var pending reducercontract.GenerationNotYetActiveError
	if !errors.As(firstErr, &pending) {
		t.Fatalf("initial error = %v, want deferral", firstErr)
	}
	if err := queue.Fail(ctx, intent, firstErr); err != nil {
		t.Fatal(err)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "before_projector_failure")
	projectorQueue := NewProjectorQueue(SQLDB{DB: db}, "projector-7916-failed-probe", time.Minute)
	projectorQueue.MaxAttempts = 1
	if err := projectorQueue.Fail(ctx, projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID, ScopeID: scopeID},
		AttemptCount: 1,
	}, errors.New("projection failed permanently")); err != nil {
		t.Fatal(err)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "after_projector_failure")
	now = now.Add(time.Minute)
	replay, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim failed generation = (%v, %v)", claimed, err)
	}
	result, replayErr := handler.Handle(ctx, replay)
	if replayErr != nil {
		t.Fatalf("failed first-generation replay error = %v, want superseded result", replayErr)
	}
	if result.Status != reducercontract.ResultStatusSuperseded || result.CanonicalWrites != 0 || *writeCalls != 0 {
		t.Fatalf("failed replay result = (%s, %d canonical writes, %d writer calls), want superseded/0/0", result.Status, result.CanonicalWrites, *writeCalls)
	}
	if err := queue.Ack(ctx, replay, result); err != nil {
		t.Fatalf("Ack superseded replay: %v", err)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, generationID, intent.IntentID, "after_failed_replay_ack")
	if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "succeeded" || attempts != 1 {
		t.Fatalf("failed replay work = (%s, %d attempts), want succeeded/1", status, attempts)
	}
	var reducerDeadLetters int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer' AND status = 'dead_letter'`, scopeID).Scan(&reducerDeadLetters); err != nil || reducerDeadLetters != 0 {
		t.Fatalf("reducer dead letters = (%d, %v), want zero", reducerDeadLetters, err)
	}
	now = now.Add(time.Minute)
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("claim after terminal Ack = (%v, %v), want no work", claimed, err)
	}
	var projectorStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, projectorWorkItemID(scopeID, generationID)).Scan(&projectorStatus); err != nil || projectorStatus != "dead_letter" {
		t.Fatalf("projector work = (%s, %v), want preserved dead_letter", projectorStatus, err)
	}
}
