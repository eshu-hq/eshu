// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// TestActivationObligationAtomicAckLive proves the #7584 Ack boundary on the
// full production schema: an accepted Ack writes exactly one pending
// obligation for its own scope and generation in the same transaction, a
// rejected, stale, duplicate, or superseded Ack writes none, and a failed
// obligation insert rolls the whole Ack back.
func TestActivationObligationAtomicAckLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_ack")
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	commitFluxSourceGeneration(t, ctx, store, "git:scope-config-obligation", "repo-config", "gen-config-obligation")
	queue := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "eshu-7584-obligation", time.Minute)
	sourceWork := claimActivationProjectorWork(t, ctx, queue,
		"git:scope-config-obligation", "gen-config-obligation")
	if err := queue.Ack(ctx, sourceWork, projectorruntime.Result{}); err != nil {
		t.Fatalf("source Ack: %v", err)
	}
	assertExactActivationObligation(t, ctx, database, sourceWork,
		"activation obligation absent after accepted Ack")

	targetScope := "git:scope-deploy-obligation"
	target := testfixtures.ActivationRepositoryFact("fact-repo-deploy-obligation", targetScope,
		"gen-deploy-obligation", "repo-deploy", "https://github.com/acme/payments-deploy.git")
	commitActivationRepository(t, ctx, store, target, "repo-deploy")
	targetWork := claimActivationProjectorWork(t, ctx, queue, target.ScopeID, target.GenerationID)
	targetWorkStatus := activationWorkStatus(t, ctx, database, targetWork)
	wrongOwner := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "wrong-owner", time.Minute)
	if err := wrongOwner.Ack(ctx, targetWork, projectorruntime.Result{}); !errors.Is(err, postgres.ErrProjectorClaimRejected) {
		t.Fatalf("wrong-owner Ack error = %v, want claim rejection", err)
	}
	assertActivationActivePointer(t, ctx, database, targetScope, "")
	assertNoActivationObligation(t, ctx, database, targetWork)
	if status := activationWorkStatus(t, ctx, database, targetWork); status != targetWorkStatus {
		t.Fatalf("wrong-owner Ack changed work %q -> %q", targetWorkStatus, status)
	}
	staleAttempt := targetWork
	staleAttempt.AttemptCount++
	if err := queue.Ack(ctx, staleAttempt, projectorruntime.Result{}); !errors.Is(err, postgres.ErrProjectorClaimRejected) {
		t.Fatalf("stale-attempt Ack error = %v, want claim rejection", err)
	}
	assertActivationActivePointer(t, ctx, database, targetScope, "")
	assertNoActivationObligation(t, ctx, database, targetWork)
	if status := activationWorkStatus(t, ctx, database, targetWork); status != targetWorkStatus {
		t.Fatalf("stale-attempt Ack changed work %q -> %q", targetWorkStatus, status)
	}
	if err := queue.Ack(ctx, targetWork, projectorruntime.Result{}); err != nil {
		t.Fatalf("target Ack: %v", err)
	}
	assertExactActivationObligation(t, ctx, database, targetWork, "target activation obligation absent")
	if err := queue.Ack(ctx, targetWork, projectorruntime.Result{}); !errors.Is(err, postgres.ErrProjectorClaimRejected) {
		t.Fatalf("duplicate Ack error = %v, want claim rejection", err)
	}
	assertExactActivationObligation(t, ctx, database, targetWork, "duplicate Ack changed obligation")

	supersedeScope := "git:scope-supersede-obligation"
	oldFact := testfixtures.ActivationRepositoryFact("fact-repo-obligation-old", supersedeScope,
		"gen-obligation-old", "repo-supersede", "https://github.com/acme/old.git")
	commitActivationRepository(t, ctx, store, oldFact, "repo-supersede")
	oldWork := claimActivationProjectorWork(t, ctx, queue, oldFact.ScopeID, oldFact.GenerationID)
	newFact := testfixtures.ActivationRepositoryFact("fact-repo-obligation-new", supersedeScope,
		"gen-obligation-new", "repo-supersede", "https://github.com/acme/new.git")
	newFact.ObservedAt = oldFact.ObservedAt.Add(time.Hour)
	commitActivationRepository(t, ctx, store, newFact, "repo-supersede")
	if err := queue.Heartbeat(ctx, oldWork); !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("old work Heartbeat error = %v, want ErrWorkSuperseded", err)
	}
	assertNoActivationObligation(t, ctx, database, oldWork)
	assertActivationActivePointer(t, ctx, database, newFact.ScopeID, "")
	newWork := claimActivationProjectorWork(t, ctx, queue, newFact.ScopeID, newFact.GenerationID)
	if err := queue.Ack(ctx, newWork, projectorruntime.Result{}); err != nil {
		t.Fatalf("new generation Ack: %v", err)
	}
	assertExactActivationObligation(t, ctx, database, newWork, "new active generation obligation absent")
	if err := queue.Ack(ctx, oldWork, projectorruntime.Result{}); !errors.Is(err, postgres.ErrProjectorClaimRejected) {
		t.Fatalf("old generation Ack error = %v, want claim rejected", err)
	}
	assertNoActivationObligation(t, ctx, database, oldWork)
	assertActivationActivePointer(t, ctx, database, newFact.ScopeID, newFact.GenerationID)
	assertExactActivationObligation(t, ctx, database, newWork,
		"new active generation obligation missing after old Ack rejection")

	// Fault injection on the isolated schema only: one CHECK makes the
	// obligation insert for exactly this generation fail inside Ack.
	if _, err := database.ExecContext(ctx, `ALTER TABLE activation_obligations
ADD CONSTRAINT eshu_7584_injected_obligation_failure
CHECK (generation_id <> 'gen-obligation-insert-fail')`); err != nil {
		t.Fatalf("install injected obligation failure: %v", err)
	}
	failFact := testfixtures.ActivationRepositoryFact("fact-repo-obligation-fail", targetScope,
		"gen-obligation-insert-fail", "repo-deploy", "https://github.com/acme/fail.git")
	failFact.ObservedAt = target.ObservedAt.Add(2 * time.Hour)
	commitActivationRepository(t, ctx, store, failFact, "repo-deploy")
	failWork := claimActivationProjectorWork(t, ctx, queue, failFact.ScopeID, failFact.GenerationID)
	assertActivationActivePointer(t, ctx, database, targetScope, target.GenerationID)
	beforeStatus := activationWorkStatus(t, ctx, database, failWork)
	beforeActive := activationGenerationStatus(t, ctx, database, target.GenerationID)
	beforeFailed := activationGenerationStatus(t, ctx, database, failFact.GenerationID)
	insertErr := queue.Ack(ctx, failWork, projectorruntime.Result{})
	var pgErr *pgconn.PgError
	if !errors.As(insertErr, &pgErr) || pgErr.Code != "23514" ||
		pgErr.ConstraintName != "eshu_7584_injected_obligation_failure" {
		t.Fatalf("injected Ack error = %v, want exact obligation CHECK 23514", insertErr)
	}
	assertActivationActivePointer(t, ctx, database, targetScope, target.GenerationID)
	if after := activationWorkStatus(t, ctx, database, failWork); after != beforeStatus {
		t.Fatalf("failed Ack changed work status %q -> %q", beforeStatus, after)
	}
	if after := activationGenerationStatus(t, ctx, database, target.GenerationID); after != beforeActive {
		t.Fatalf("failed Ack changed prior generation status %q -> %q", beforeActive, after)
	}
	if after := activationGenerationStatus(t, ctx, database, failFact.GenerationID); after != beforeFailed {
		t.Fatalf("failed Ack changed target generation status %q -> %q", beforeFailed, after)
	}
	assertNoActivationObligation(t, ctx, database, failWork)
}
