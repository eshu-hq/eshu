// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11000 cross-scope producer-terminal waits re-keyed to the producer domain alone: park shape, reopen on the producer reducer Ack and on the projector-Ack generation activation with the audit disabled, and audit satisfiability through the shared quiescence probe; disposable database per test, about 60 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// producerKindScope seeds a scope of one collector kind with an active
// generation and no projector work behind it, so the kind reads quiescent.
func producerKindScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, kind scope.CollectorKind, activeGen string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, $2, $1, $3, $3, 'active', $4, '{}'::jsonb)`, scopeID, string(kind), at, activeGen); err != nil {
		t.Fatalf("seed producer kind scope %s: %v", scopeID, err)
	}
	addOwnGateGeneration(t, ctx, db, scopeID, activeGen, at, "active")
}

// producerPendingGen adds one pending generation the projector Ack can
// activate.
func producerPendingGen(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string, at time.Time) {
	t.Helper()
	addOwnGateGeneration(t, ctx, db, scopeID, genID, at, "pending")
}

// busyProducerScope seeds a producer kind scope whose kind is registered but
// not quiescent: live projector work on the pending generation. A park on a
// producer of this kind sticks until that work drains. A later projector
// Ack of the pending generation consumes the seeded row and quiesces the
// kind when no other scope of the kind stays busy.
func busyProducerScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, kind scope.CollectorKind, activeGen, pendingGen, owner string, at time.Time) {
	t.Helper()
	producerKindScope(t, ctx, db, scopeID, kind, activeGen, at)
	producerPendingGen(t, ctx, db, scopeID, pendingGen, at)
	if err := seedProjectorRunningErr(ctx, db, scopeID, pendingGen, owner); err != nil {
		t.Fatalf("seed busy projector work %s/%s: %v", scopeID, pendingGen, err)
	}
}

// crossScopeProducerNotReady builds the floor deferral for a consumer whose
// declared producers have not activated.
func crossScopeProducerNotReady(consumer reducercontract.Domain, scopeID, genID string, producers ...reducercontract.Domain) error {
	return crossscope.NewProducerNotReadyError(consumer, scopeID, genID, producers)
}

// parkCrossScopeConsumer claims the consumer item and fails it with the
// floor deferral, returning the intent.
func parkCrossScopeConsumer(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, consumer reducercontract.Domain, producers ...reducercontract.Domain) reducercontract.Intent {
	t.Helper()
	queue, intent := claimWaitingRow(t, ctx, db, clock, consumer)
	if err := queue.Fail(ctx, intent, crossScopeProducerNotReady(consumer, intent.ScopeID, intent.GenerationID, producers...)); err != nil {
		t.Fatalf("fail consumer %s into wait: %v", consumer, err)
	}
	return intent
}

// crossScopeWaitKeys reads the parked waits_for keys for one queue row.
func crossScopeWaitKeys(t *testing.T, ctx context.Context, db *sql.DB, id string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT waits_for_key FROM reducer_waiting_items WHERE work_item_id = $1 ORDER BY waits_for_key`, id)
	if err != nil {
		t.Fatalf("read wait keys %s: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan wait key %s: %v", id, err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate wait keys %s: %v", id, err)
	}
	return out
}

// assertDomainOnlyKey fails unless the key carries exactly one producer
// domain and no scope or generation.
func assertDomainOnlyKey(t *testing.T, raw, wantDomain string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode wait key %q: %v", raw, err)
	}
	if len(decoded) != 1 || decoded["producer_domain"] != wantDomain {
		t.Fatalf("wait key = %q, want exactly {\"producer_domain\":%q}", raw, wantDomain)
	}
}

// activateProducerGenWithKind runs a real projector Ack that activates the
// pending generation of a scope of the given collector kind.
func activateProducerGenWithKind(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string, kind scope.CollectorKind, clock *time.Time) {
	t.Helper()
	if err := seedProjectorRunningErr(ctx, db, scopeID, genID, "cross-scope-11000"); err != nil {
		t.Fatalf("seed projector running %s/%s: %v", scopeID, genID, err)
	}
	queue := NewProjectorQueue(SQLDB{DB: db}, "cross-scope-11000", 10*time.Minute)
	if clock != nil {
		at := *clock
		queue.Now = func() time.Time { return at }
	}
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID, CollectorKind: kind},
		Generation:   scope.ScopeGeneration{GenerationID: genID},
		AttemptCount: 1,
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("projector ack %s/%s: %v", scopeID, genID, err)
	}
}

// TestCrossScopeProducerWaitParksDomainOnlyLive: the parked wait is keyed
// {"producer_domain"} alone, while the row keeps the waiter's own
// scope/generation for observability. RED before #11000: the key carries
// the consumer scope and generation, which no producer commit can match.
func TestCrossScopeProducerWaitParksDomainOnlyLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-park"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	busyProducerScope(t, ctx, base, "waiting-11000-producer-park", scope.CollectorOCIRegistry, "p0", "p1", "park-keeper", clock)
	addOwnGateItem(t, ctx, base, "wait-11000-park", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)

	intent := parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

	state := readWaitingRow(t, ctx, base, "wait-11000-park")
	if state.status != "retrying" || state.attempts != 1 || state.waitRows != 1 {
		t.Fatalf("parked state = %+v, want retrying/attempt 1/1 wait row", state)
	}
	if !state.waitKind.Valid || state.waitKind.String != string(WaitingKindProducerTerminal) {
		t.Fatalf("wait kind = %v, want producer_domain_terminal", state.waitKind)
	}
	keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-park")
	if len(keys) != 1 {
		t.Fatalf("wait keys = %q, want exactly one domain-only key", keys)
	}
	assertDomainOnlyKey(t, keys[0], string(reducer.DomainContainerImageIdentity))

	var scopeID, generationID string
	if err := base.QueryRowContext(ctx, `SELECT scope_id, generation_id FROM reducer_waiting_items WHERE work_item_id = $1`,
		"wait-11000-park").Scan(&scopeID, &generationID); err != nil {
		t.Fatalf("read wait row scope: %v", err)
	}
	if scopeID != consumerScope || generationID != "g1" {
		t.Fatalf("wait row scope = %s/%s, want the waiter %s/g1", scopeID, generationID, consumerScope)
	}
	_ = intent
}

// TestCrossScopeProducerWaitReopensOnReducerAckLive: a parked domain wait
// reopens on the producer's terminal reducer Ack with the audit disabled
// and the backstop untouched. RED before #11000: the wake keys on the
// consumer scope, so the producer commit in its own scope never matches.
func TestCrossScopeProducerWaitReopensOnReducerAckLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-rack"
	producerScope := "waiting-11000-producer-rack"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	busyProducerScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", "p1", "rack-keeper", clock)
	addOwnGateItem(t, ctx, base, "wait-11000-rack", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
	parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

	addOwnGateItem(t, ctx, base, "prod-11000-rack", producerScope, "p0", reducer.DomainContainerImageIdentity, clock)
	producerQueue, producerIntent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainContainerImageIdentity)
	if producerIntent.IntentID != "prod-11000-rack" {
		t.Fatalf("claimed producer = %q, want prod-11000-rack", producerIntent.IntentID)
	}
	if err := producerQueue.Ack(ctx, producerIntent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack producer: %v", err)
	}

	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-rack"); len(keys) != 0 {
		t.Fatalf("wait keys after producer ack = %q, want none (reopened, audit disabled)", keys)
	}
	state := readWaitingRow(t, ctx, base, "wait-11000-rack")
	if state.status != "retrying" || state.attempts != 1 {
		t.Fatalf("reopened state = %+v, want retrying at attempt 1", state)
	}

	// The woken row is claimable at once, not at the backstop.
	_, intent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation)
	if intent.IntentID != "wait-11000-rack" {
		t.Fatalf("reclaimed = %q, want wait-11000-rack", intent.IntentID)
	}
}

// TestCrossScopeProducerWaitReopensOnProjectorActivationLive: a parked
// domain wait reopens on the projector Ack that activates the producer
// generation, with the audit disabled. RED before #11000: the
// projector Ack wakes generation_active only, never the producer wait.
func TestCrossScopeProducerWaitReopensOnProjectorActivationLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-pack"
	producerScope := "waiting-11000-producer-pack"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	busyProducerScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", "p1", "cross-scope-11000", clock)
	addOwnGateItem(t, ctx, base, "wait-11000-pack", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
	parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-pack"); len(keys) != 1 {
		t.Fatalf("wait keys before activation = %q, want one parked domain wait", keys)
	}

	activateProducerGenWithKind(t, ctx, base, producerScope, "p1", scope.CollectorOCIRegistry, &clock)

	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-pack"); len(keys) != 0 {
		t.Fatalf("wait keys after projector activation = %q, want none (reopened, audit disabled)", keys)
	}
	state := readWaitingRow(t, ctx, base, "wait-11000-pack")
	if state.status != "retrying" || state.attempts != 1 {
		t.Fatalf("reopened state = %+v, want retrying at attempt 1", state)
	}
}

// TestCrossScopeProducerAuditSatisfiesQuiescentWaitLive: the audit reopens
// a parked domain wait once the producer kind is quiescent, proving the
// predicate is satisfiable by a real producer commit. RED before #11000:
// the predicate checks the consumer scope for a producer terminal row and
// never fires.
func TestCrossScopeProducerAuditSatisfiesQuiescentWaitLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-audit"
	producerScope := "waiting-11000-producer-audit"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	producerKindScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", clock)
	if err := seedProjectorRunningErr(ctx, base, producerScope, "p0", "cross-scope-11000-audit"); err != nil {
		t.Fatalf("seed live projector work: %v", err)
	}
	addOwnGateItem(t, ctx, base, "wait-11000-audit", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
	parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

	// The kind is registered but not quiescent: the audit must not fire.
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, clock, DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("audit while busy: %v", err)
	}
	if reopened != 0 {
		t.Fatalf("audit reopened = %d while the producer kind is busy, want 0", reopened)
	}
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-audit"); len(keys) != 1 {
		t.Fatalf("wait keys while busy = %q, want the parked wait", keys)
	}

	// The producer drains its projector work: quiescent, so satisfied.
	if _, err := base.ExecContext(ctx, `DELETE FROM fact_work_items WHERE stage = 'projector' AND scope_id = $1`, producerScope); err != nil {
		t.Fatalf("drain projector work: %v", err)
	}
	reopened, _, missed, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, clock, DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("audit while quiescent: %v", err)
	}
	if reopened != 1 {
		t.Fatalf("audit reopened = %d once quiescent, want 1", reopened)
	}
	if missed != 0 {
		t.Fatalf("audit missed = %d on a fresh wait, want 0", missed)
	}
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-audit"); len(keys) != 0 {
		t.Fatalf("wait keys after audit = %q, want none", keys)
	}
}
