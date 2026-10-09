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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// epochBarrierFactLoader serves no facts: the barrier proof exercises epoch
// gating, not decision building, so every load path stays empty.
type epochBarrierFactLoader struct{}

func (epochBarrierFactLoader) ListFacts(
	_ context.Context,
	_, _ string,
) ([]facts.Envelope, error) {
	return nil, nil
}

// epochBarrierWriter delegates the epoch read to the real scope-state store
// and stubs the decision write: the write path is proven by the digest-v3
// lifecycle test, while the barrier proof needs real epoch SQL semantics.
type epochBarrierWriter struct {
	store ContainerImageIdentityScopeStateStore
}

func (w epochBarrierWriter) ContainerImageIdentityActivationEpoch(
	ctx context.Context,
	scopeID, generationID string,
) (int64, error) {
	return w.store.ContainerImageIdentityActivationEpoch(ctx, scopeID, generationID)
}

func (epochBarrierWriter) WriteContainerImageIdentityDecisions(
	_ context.Context,
	_ containerimage.ContainerImageIdentityWrite,
) (containerimage.ContainerImageIdentityWriteResult, error) {
	return containerimage.ContainerImageIdentityWriteResult{}, nil
}

var (
	_ factload.FactLoader                         = epochBarrierFactLoader{}
	_ containerimage.ContainerImageIdentityWriter = epochBarrierWriter{}
)

// seedEpochBarrierScope seeds a scope whose active generation A is followed by
// a pending, strictly newer generation B. Activating B is the test's
// controlled barrier: until the test flips it, B has no scope-state row (the
// ingestion_scopes trigger writes that row in the activation transaction), so
// a projection that reads B's epoch first reproduces the #6502 race without
// load or sleeps.
func seedEpochBarrierScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationA, generationB string,
) {
	t.Helper()
	ingestedA := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	ingestedB := time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'repository', 'git', $1, 'git', $1,
    clock_timestamp(), clock_timestamp(), 'active'
)
`, scopeID); err != nil {
		t.Fatalf("seed barrier scope: %v", err)
	}
	for _, gen := range []struct {
		id       string
		ingested time.Time
		status   string
	}{
		{generationA, ingestedA, "active"},
		{generationB, ingestedB, "pending"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES ($1, $2, 'synthetic', FALSE, $3, $3, $4)
`, gen.id, scopeID, gen.ingested, gen.status); err != nil {
			t.Fatalf("seed barrier generation %s: %v", gen.id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generationA); err != nil {
		t.Fatalf("activate barrier generation A: %v", err)
	}
}

// TestContainerImageIdentityEpochBarrierDefersPendingLive holds generation B's
// activation behind a controlled barrier while the projection reads B's epoch
// first. A transient inactive generation must defer and retry (non-counting),
// never dead-letter; after the barrier releases, B proceeds and acks; the
// superseded A is then superseded rather than dead-lettered. The loud
// missing-epoch path is pinned by the sentinel test below. Issue #6502.
func TestContainerImageIdentityEpochBarrierDefersPendingLive(t *testing.T) {
	// Bridge the live-postgres-readiness runner's family DSN onto the shared
	// helper's generic variable, as the retention proofs do. Local runs keep
	// using ESHU_POSTGRES_TEST_DSN directly.
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN is set without ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

	const (
		scopeID = "repository:6502-epoch-barrier"
		genA    = "generation:6502-epoch-barrier-a"
		genB    = "generation:6502-epoch-barrier-b"
		workB1  = "work:6502-epoch-barrier-b1"
		workB2  = "work:6502-epoch-barrier-b2"
		workA3  = "work:6502-epoch-barrier-a3"
		owner   = "reducer-6502-epoch-barrier"
	)
	seedEpochBarrierScope(t, ctx, db, scopeID, genA, genB)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM fact_work_items WHERE scope_id = $1`, scopeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM container_image_identity_scope_state WHERE scope_id = $1`, scopeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	}()
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, genA)

	store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader:      epochBarrierFactLoader{},
		Writer:          epochBarrierWriter{store: store},
		GenerationCheck: NewGenerationFreshnessCheck(SQLDB{DB: db}),
	}
	queue := ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, RetryDelay: time.Second,
		MaxAttempts: 3, JitterFraction: 0,
		Now: func() time.Time { return now },
	}

	claim := func(workItemID, generationID string, attemptCount int) reducer.Intent {
		t.Helper()
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generationID,
			owner, now.Add(time.Minute), now,
		)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items SET attempt_count = $2 WHERE work_item_id = $1
`, workItemID, attemptCount); err != nil {
			t.Fatalf("seed barrier attempt count %s: %v", workItemID, err)
		}
		claimedAt, claimEpoch := stampContainerImageIdentityAckClaim(t, ctx, db, workItemID)
		return reducer.Intent{
			IntentID:     workItemID,
			ScopeID:      scopeID,
			GenerationID: generationID,
			SourceSystem: "git",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: attemptCount,
			ClaimEpoch:   claimEpoch,
			ClaimedAt:    &claimedAt,
		}
	}
	outcome := func(workItemID string) (status, class string, attempts int) {
		t.Helper()
		var (
			statusVal  string
			classVal   sql.NullString
			attemptVal int
		)
		if err := db.QueryRowContext(ctx, `
SELECT status, failure_class, attempt_count FROM fact_work_items WHERE work_item_id = $1
`, workItemID).Scan(&statusVal, &classVal, &attemptVal); err != nil {
			t.Fatalf("read barrier outcome %s: %v", workItemID, err)
		}
		return statusVal, classVal.String, attemptVal
	}

	// Leg 1: the barrier holds B pending while the projection reads first.
	// Attempt 100 of a budget of 3 proves the deferral is non-counting.
	intentB1 := claim(workB1, genB, 100)
	handleErr := func() error {
		_, err := handler.Handle(ctx, intentB1)
		return err
	}()
	var notYetActive reducercontract.GenerationNotYetActiveError
	if !errors.As(handleErr, &notYetActive) {
		t.Errorf("pending Handle() error = %v, want GenerationNotYetActiveError", handleErr)
	}
	if handleErr == nil {
		t.Fatalf("pending Handle() succeeded, want a deferral error")
	}
	if err := queue.Fail(ctx, intentB1, handleErr); err != nil {
		t.Fatalf("Fail(pending) error = %v", err)
	}
	if status, class, attempts := outcome(workB1); status != "retrying" ||
		class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 100 {
		t.Errorf(
			"pending outcome = (%s, %s, %d), want (retrying, %s, 100)",
			status, class, attempts,
			reducercontract.GenerationActivationNotReadyFailureClass,
		)
	}

	// Leg 2: release the barrier; B activates and the same generation
	// proceeds through Handle and acks.
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1
`, genA); err != nil {
		t.Fatalf("release barrier supersede A: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'active' WHERE generation_id = $1
`, genB); err != nil {
		t.Fatalf("release barrier generation status: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, genB); err != nil {
		t.Fatalf("release barrier activation: %v", err)
	}
	intentB2 := claim(workB2, genB, 1)
	resultB2, err := handler.Handle(ctx, intentB2)
	if err != nil {
		t.Errorf("released Handle() error = %v, want success", err)
	} else if resultB2.Status != reducercontract.ResultStatusSucceeded {
		t.Errorf("released Handle() status = %s, want succeeded", resultB2.Status)
	}
	if err == nil {
		if err := queue.Ack(ctx, intentB2, resultB2); err != nil {
			t.Errorf("Ack(released) error = %v", err)
		}
	}

	// Leg 3: A is now permanently superseded and must ack as superseded,
	// never dead-letter.
	intentA3 := claim(workA3, genA, 1)
	resultA3, err := handler.Handle(ctx, intentA3)
	if err != nil {
		t.Errorf("superseded Handle() error = %v, want a superseded result", err)
	} else if resultA3.Status != reducercontract.ResultStatusSuperseded {
		t.Errorf("superseded Handle() status = %s, want superseded", resultA3.Status)
	}

	var deadLetters int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_work_items
WHERE work_item_id IN ($1, $2, $3) AND status = 'dead_letter'
`, workB1, workB2, workA3).Scan(&deadLetters); err != nil {
		t.Fatalf("count barrier dead letters: %v", err)
	}
	if deadLetters != 0 {
		t.Errorf("barrier dead letters = %d, want 0", deadLetters)
	}
}

// TestContainerImageIdentityActivationEpochMissIsSentinelLive pins the store
// contract the #6502 gate matches on: an epoch miss wraps
// ErrContainerImageIdentityGenerationNotActive for a pending, superseded, or
// unknown generation alike, while the active generation reads its epoch.
func TestContainerImageIdentityActivationEpochMissIsSentinelLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN is set without ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID = "repository:6502-epoch-sentinel"
		genA    = "generation:6502-epoch-sentinel-a"
		genB    = "generation:6502-epoch-sentinel-b"
	)
	seedEpochBarrierScope(t, ctx, db, scopeID, genA, genB)
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM container_image_identity_scope_state WHERE scope_id = $1`, scopeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM scope_generations WHERE scope_id = $1`, scopeID)
		_, _ = db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	}()

	store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	if epoch, err := store.ContainerImageIdentityActivationEpoch(ctx, scopeID, genA); err != nil || epoch <= 0 {
		t.Errorf("active epoch = (%d, %v), want (positive, nil)", epoch, err)
	}
	assertMiss := func(scope, generation string) {
		t.Helper()
		_, err := store.ContainerImageIdentityActivationEpoch(ctx, scope, generation)
		if !errors.Is(err, reducercontract.ErrContainerImageIdentityGenerationNotActive) {
			t.Errorf("miss epoch(%s/%s) error = %v, want the not-active sentinel", scope, generation, err)
		}
	}
	assertMiss(scopeID, genB)
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`, genA); err != nil {
		t.Fatalf("supersede sentinel generation A: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations SET status = 'active' WHERE generation_id = $1`, genB); err != nil {
		t.Fatalf("activate sentinel generation B: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, genB); err != nil {
		t.Fatalf("flip sentinel activation: %v", err)
	}
	assertMiss(scopeID, genA)
	assertMiss(scopeID, "generation:6502-epoch-sentinel-missing")
	assertMiss("repository:6502-epoch-sentinel-unknown", genA)
}
