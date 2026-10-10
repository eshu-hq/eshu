// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestContainerImageIdentityFirstGenerationServiceDispatchLive drives actual
// sequential and batch Service.Run loops against a pending first generation
// and an unrelated already-active scope on disposable PostgreSQL.
func TestContainerImageIdentityFirstGenerationServiceDispatchLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("epoch proof DSN requires disposable=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	for _, mode := range []struct {
		name    string
		workers int
	}{
		{name: "single", workers: 1},
		{name: "batch", workers: 2},
	} {
		t.Run(mode.name, func(t *testing.T) {
			db := openContainerImageIdentityAckCapabilityProofDB(t)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			const (
				pendingScope = "repository:7916-service-pending"
				pendingGen   = "generation:7916-service-pending"
				readyScope   = "repository:7916-service-ready"
				readyGen     = "generation:7916-service-ready"
				projOwner    = "projector-7916-service"
			)
			seedFirstGenerationServiceScopes(t, ctx, db, pendingScope, pendingGen, readyScope, readyGen, projOwner)
			now := time.Now().UTC()
			queue := ReducerQueue{
				database: SQLDB{DB: db}, LeaseOwner: "reducer-7916-service-" + mode.name,
				LeaseDuration: time.Minute, RetryDelay: time.Second, MaxAttempts: 2,
				ClaimDomains: []reducer.Domain{reducer.DomainContainerImageIdentity},
				Now:          func() time.Time { return now },
			}
			for _, row := range []struct{ scopeID, generationID string }{
				{pendingScope, pendingGen}, {readyScope, readyGen},
			} {
				if result, err := queue.Enqueue(ctx, []projectorruntime.ReducerIntent{{
					ScopeID: row.scopeID, GenerationID: row.generationID,
					Domain:    reducer.DomainContainerImageIdentity,
					EntityKey: "identity:" + row.scopeID, Reason: "firstgen service dispatch",
					FactID: "fact:" + row.scopeID, SourceSystem: "git",
				}}); err != nil || result.Count != 1 {
					t.Fatalf("enqueue %s = (%+v, %v), want one", row.scopeID, result, err)
				}
			}
			store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
			writes := new(int)
			handler := containerimage.ContainerImageIdentityHandler{
				FactLoader:      epochBarrierFactLoader{},
				Writer:          epochBarrierWriter{store: store, writes: writes},
				GenerationCheck: NewGenerationFreshnessCheck(SQLDB{DB: db}),
			}
			var pendingHandlerCalls, readyHandlerCalls atomic.Int32
			spy := reducer.HandlerFunc(func(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
				switch intent.ScopeID {
				case pendingScope:
					pendingHandlerCalls.Add(1)
				case readyScope:
					readyHandlerCalls.Add(1)
				}
				return handler.Handle(ctx, intent)
			})
			registry := reducer.NewRegistry()
			if err := registry.Register(reducer.DomainDefinition{
				Domain:        reducer.DomainContainerImageIdentity,
				Summary:       "first-generation service proof",
				Ownership:     reducer.OwnershipShape{CrossSource: true, CrossScope: true, CanonicalWrite: true},
				TruthContract: testReducerTruthContract("container_image_identity"),
				Handler:       spy,
			}); err != nil {
				t.Fatalf("register identity handler: %v", err)
			}
			runtime, err := reducer.NewRuntime(registry)
			if err != nil {
				t.Fatalf("new reducer runtime: %v", err)
			}
			runtime.GenerationCheck = NewGenerationFreshnessCheck(SQLDB{DB: db})
			service := reducer.Service{
				PollInterval: time.Second, WorkSource: queue, WorkSink: queue, Executor: runtime,
				Workers: mode.workers, BatchClaimSize: 2,
				Wait: func(context.Context, time.Duration) error { return context.Canceled },
			}
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Service.Run before activation: %v", err)
			}
			pendingID := firstGenerationServiceWorkID(t, ctx, db, pendingScope, pendingGen)
			readyID := firstGenerationServiceWorkID(t, ctx, db, readyScope, readyGen)
			if status, class, attempts := firstGenerationBarrierOutcome(t, ctx, db, pendingID); status != "retrying" ||
				class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 1 {
				t.Fatalf("pending service work = (%s, %s, %d), want retrying/not_ready/1", status, class, attempts)
			}
			if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, readyID); status != "succeeded" || attempts != 1 || *writes != 1 {
				t.Fatalf("unrelated ready work = (%s, %d attempts, %d writes), want succeeded/1/1", status, attempts, *writes)
			}
			if pendingHandlerCalls.Load() != 0 || readyHandlerCalls.Load() != 1 {
				t.Fatalf("pre-Ack handler entries = (pending %d, ready %d), want 0/1", pendingHandlerCalls.Load(), readyHandlerCalls.Load())
			}
			assertNoFirstGenerationReducerDeadLetters(t, ctx, db, pendingScope)
			logFirstGenerationBarrierSnapshot(t, ctx, db, pendingScope, pendingGen, pendingID, mode.name+"_before_ack")
			projectorQueue := NewProjectorQueue(SQLDB{DB: db}, projOwner, time.Minute)
			if err := projectorQueue.Ack(ctx, projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: pendingScope},
				Generation:   scope.ScopeGeneration{GenerationID: pendingGen, ScopeID: pendingScope},
				AttemptCount: 1,
			}, projectorruntime.Result{}); err != nil {
				t.Fatalf("projector Ack: %v", err)
			}
			if epoch, err := store.ContainerImageIdentityActivationEpoch(ctx, pendingScope, pendingGen); err != nil || epoch <= 0 {
				t.Fatalf("activation epoch = (%d, %v), want positive", epoch, err)
			}
			now = now.Add(time.Minute)
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Service.Run after activation: %v", err)
			}
			if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, pendingID); status != "succeeded" || attempts != 1 || *writes != 2 {
				t.Fatalf("replayed service work = (%s, %d attempts, %d writes), want succeeded/1/2", status, attempts, *writes)
			}
			if pendingHandlerCalls.Load() != 1 || readyHandlerCalls.Load() != 1 {
				t.Fatalf("post-Ack handler entries = (pending %d, ready %d), want 1/1", pendingHandlerCalls.Load(), readyHandlerCalls.Load())
			}
			assertNoFirstGenerationReducerDeadLetters(t, ctx, db, pendingScope)
			logFirstGenerationBarrierSnapshot(t, ctx, db, pendingScope, pendingGen, pendingID, mode.name+"_after_ack")
		})
	}
}

func seedFirstGenerationServiceScopes(t *testing.T, ctx context.Context, db *sql.DB, pendingScope, pendingGen, readyScope, readyGen, projOwner string) {
	t.Helper()
	for _, scopeID := range []string{pendingScope, readyScope} {
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	}
	for _, row := range []struct{ scopeID, generationID, status string }{
		{pendingScope, pendingGen, "pending"}, {readyScope, readyGen, "active"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status)
VALUES ($1, $2, 'synthetic', FALSE, clock_timestamp(), clock_timestamp(), $3)
`, row.generationID, row.scopeID, row.status); err != nil {
			t.Fatalf("seed %s generation: %v", row.scopeID, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, readyScope, readyGen); err != nil {
		t.Fatalf("activate unrelated ready scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
  attempt_count, lease_owner, claim_until, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'projector', 'source_local', 'running', 1, $4,
  clock_timestamp() + INTERVAL '2 minutes', '{}'::jsonb, clock_timestamp(), clock_timestamp())
`, projectorWorkItemID(pendingScope, pendingGen), pendingScope, pendingGen, projOwner); err != nil {
		t.Fatalf("seed pending projector work: %v", err)
	}
}

func firstGenerationServiceWorkID(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `
SELECT work_item_id FROM fact_work_items
WHERE stage = 'reducer' AND scope_id = $1 AND generation_id = $2 AND domain = 'container_image_identity'
`, scopeID, generationID).Scan(&id); err != nil {
		t.Fatalf("read service work id: %v", err)
	}
	return id
}

// TestContainerImageIdentityFirstGenerationSupersededWhileNullLive proves a
// real projector claim can retire pending first-generation work before any
// generation activates, while its reducer retry is still deferred.
func TestContainerImageIdentityFirstGenerationSupersededWhileNullLive(t *testing.T) {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DISPOSABLE") != "1" {
			t.Fatal("epoch proof DSN requires disposable=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	const (
		scopeID  = "repository:7916-firstgen-superseded"
		olderGen = "generation:7916-firstgen-older"
		newerGen = "generation:7916-firstgen-newer"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedClaimMaintenanceWork(t, db, scopeID, olderGen, "pending", "pending", 2*time.Hour, nil)
	now := time.Now().UTC()
	queue := ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: "reducer-7916-superseded",
		LeaseDuration: time.Minute, RetryDelay: time.Second, MaxAttempts: 2,
		ClaimDomains: []reducer.Domain{reducer.DomainContainerImageIdentity},
		Now:          func() time.Time { return now },
	}
	if result, err := queue.Enqueue(ctx, []projectorruntime.ReducerIntent{{
		ScopeID: scopeID, GenerationID: olderGen, Domain: reducer.DomainContainerImageIdentity,
		EntityKey: "identity:7916-superseded", Reason: "older firstgen intent",
		FactID: "fact:7916-superseded", SourceSystem: "git",
	}}); err != nil || result.Count != 1 {
		t.Fatalf("enqueue older intent = (%+v, %v), want one", result, err)
	}
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim older intent = (%v, %v)", claimed, err)
	}
	store := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	writes := new(int)
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader:      epochBarrierFactLoader{},
		Writer:          epochBarrierWriter{store: store, writes: writes},
		GenerationCheck: NewGenerationFreshnessCheck(SQLDB{DB: db}),
	}
	_, pendingErr := handler.Handle(ctx, intent)
	var notActive reducercontract.GenerationNotYetActiveError
	if !errors.As(pendingErr, &notActive) {
		t.Fatalf("older pending intent error = %v, want deferral", pendingErr)
	}
	if err := queue.Fail(ctx, intent, pendingErr); err != nil {
		t.Fatalf("defer older reducer intent: %v", err)
	}
	seedClaimMaintenanceWork(t, db, scopeID, newerGen, "pending", "pending", time.Hour, nil)
	projectorQueue := NewProjectorQueue(SQLDB{DB: db}, "projector-7916-superseded", time.Minute)
	claimedProjector, ok, err := projectorQueue.Claim(ctx)
	if err != nil || !ok || claimedProjector.Generation.GenerationID != newerGen {
		t.Fatalf("projector Claim = (%q, %v, %v), want newer generation", claimedProjector.Generation.GenerationID, ok, err)
	}
	if status := generationState(t, db, olderGen); status != "superseded" {
		t.Fatalf("older generation status = %q, want superseded", status)
	}
	if status, class, _ := workState(t, db, scopeID, olderGen); status != "superseded" || class != "projector_superseded_by_newer_generation" {
		t.Fatalf("older projector work = (%s, %s), want superseded marker", status, class)
	}
	logFirstGenerationBarrierSnapshot(t, ctx, db, scopeID, olderGen, intent.IntentID, "after_legitimate_supersession")
	now = now.Add(time.Minute)
	replay, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim older reducer retry = (%v, %v)", claimed, err)
	}
	result, replayErr := handler.Handle(ctx, replay)
	if replayErr != nil || result.Status != reducercontract.ResultStatusSuperseded || result.CanonicalWrites != 0 || *writes != 0 {
		t.Fatalf("superseded retry = (%+v, %v, %d writer calls), want superseded/zero", result, replayErr, *writes)
	}
	if err := queue.Ack(ctx, replay, result); err != nil {
		t.Fatalf("Ack superseded reducer intent: %v", err)
	}
	if status, _, attempts := firstGenerationBarrierOutcome(t, ctx, db, intent.IntentID); status != "succeeded" || attempts != 1 {
		t.Fatalf("terminal older reducer intent = (%s, %d), want succeeded/1", status, attempts)
	}
	assertNoFirstGenerationReducerDeadLetters(t, ctx, db, scopeID)
	now = now.Add(time.Minute)
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("claim after superseded Ack = (%v, %v), want no work", claimed, err)
	}
	var active sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`, scopeID).Scan(&active); err != nil || active.Valid {
		t.Fatalf("active generation after projector Claim = (%v, %v), want SQL NULL", active, err)
	}
}

func assertNoFirstGenerationReducerDeadLetters(t *testing.T, ctx context.Context, db *sql.DB, scopeID string) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer' AND status = 'dead_letter'`, scopeID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("scope %s reducer dead letters = (%d, %v), want zero", scopeID, count, err)
	}
}
