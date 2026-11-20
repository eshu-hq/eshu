// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11000 cross-scope producer-terminal waits: both claim orders converge, per-domain independence across the two declared producers, and the same-scope narrowed waits keep their byte-identical keys and wakes; disposable database per test, about 60 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// waitingTerminalShape is the comparable terminal state of one parked and
// reopened queue row.
type waitingTerminalShape struct {
	status   string
	attempts int
	class    string
	visible  time.Time
	waits    int
}

// TestCrossScopeProducerBothOrdersLive: park-before-activation and
// activation-before-park converge on the same terminal shape. Activation
// before park reopens at once through the set-aside re-check, so no wait
// row survives the Fail. RED before #11000: order A never reopens (no
// wake) and order B parks despite a satisfied provider (the predicate
// checks the consumer scope).
func TestCrossScopeProducerBothOrdersLive(t *testing.T) {
	clock := time.Now().UTC().Truncate(time.Second)

	shapeA := func() waitingTerminalShape {
		ctx := context.Background()
		base, _ := openWaitingDB(t, ctx)
		consumerScope := "waiting-11000-consumer-ordA"
		producerScope := "waiting-11000-producer-ordA"
		addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
		busyProducerScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", "p1", "cross-scope-11000", clock)
		addOwnGateItem(t, ctx, base, "wait-11000-ordA", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
		parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

		activateProducerGenWithKind(t, ctx, base, producerScope, "p1", scope.CollectorOCIRegistry, &clock)

		if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-ordA"); len(keys) != 0 {
			t.Fatalf("order A wait keys = %q, want none", keys)
		}
		state := readWaitingRow(t, ctx, base, "wait-11000-ordA")
		return waitingTerminalShape{
			status: state.status, attempts: state.attempts,
			class: state.failureClass.String, visible: state.visibleAt.Time.Truncate(time.Second), waits: state.waitRows,
		}
	}()

	shapeB := func() waitingTerminalShape {
		ctx := context.Background()
		base, _ := openWaitingDB(t, ctx)
		consumerScope := "waiting-11000-consumer-ordB"
		producerScope := "waiting-11000-producer-ordB"
		addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
		producerKindScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", clock)
		producerPendingGen(t, ctx, base, producerScope, "p1", clock)
		activateProducerGenWithKind(t, ctx, base, producerScope, "p1", scope.CollectorOCIRegistry, &clock)
		addOwnGateItem(t, ctx, base, "wait-11000-ordB", consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
		parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

		if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-ordB"); len(keys) != 0 {
			t.Fatalf("order B wait keys = %q, want none (set-aside re-check reopens at once)", keys)
		}
		state := readWaitingRow(t, ctx, base, "wait-11000-ordB")
		return waitingTerminalShape{
			status: state.status, attempts: state.attempts,
			class: state.failureClass.String, visible: state.visibleAt.Time.Truncate(time.Second), waits: state.waitRows,
		}
	}()

	if shapeA != shapeB {
		t.Fatalf("terminal shapes differ:\nA = %+v\nB = %+v", shapeA, shapeB)
	}
	if shapeA.status != "retrying" || shapeA.attempts != 1 || shapeA.waits != 0 {
		t.Fatalf("converged shape = %+v, want retrying/attempt 1/no waits", shapeA)
	}
}

// TestCrossScopeProducerMultiProducerLive: the two declared producers of
// supply_chain_impact park independently; acking one reopens only its own
// wait. RED before #11000: one scope-keyed row parks and no producer Ack
// reopens anything.
func TestCrossScopeProducerMultiProducerLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-multi"
	identityScope := "waiting-11000-producer-ident"
	cicdScope := "waiting-11000-producer-cicd"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	busyProducerScope(t, ctx, base, identityScope, scope.CollectorOCIRegistry, "i0", "i1", "cross-scope-11000", clock)
	producerKindScope(t, ctx, base, cicdScope, scope.CollectorCICDRun, "c0", clock)
	if err := seedProjectorRunningErr(ctx, base, cicdScope, "c0", "cicd-keeper"); err != nil {
		t.Fatalf("seed busy cicd projector work: %v", err)
	}
	addOwnGateItem(t, ctx, base, "wait-11000-multi", consumerScope, "g1", reducer.DomainSupplyChainImpact, clock)
	parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainSupplyChainImpact,
		reducer.DomainContainerImageIdentity, reducer.DomainCICDRunCorrelation)

	keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-multi")
	if len(keys) != 2 {
		t.Fatalf("wait keys = %q, want one per declared producer", keys)
	}
	assertDomainOnlyKey(t, keys[0], string(reducer.DomainCICDRunCorrelation))
	assertDomainOnlyKey(t, keys[1], string(reducer.DomainContainerImageIdentity))

	// Ack the ci_cd_run_correlation producer through the batch path: only
	// its own wait reopens.
	addOwnGateItem(t, ctx, base, "prod-11000-cicd", cicdScope, "c0", reducer.DomainCICDRunCorrelation, clock)
	batchQueue, cicdIntent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation)
	if err := batchQueue.AckBatch(ctx, []reducercontract.Intent{cicdIntent}, nil); err != nil {
		t.Fatalf("batch ack cicd producer: %v", err)
	}
	keys = crossScopeWaitKeys(t, ctx, base, "wait-11000-multi")
	if len(keys) != 1 {
		t.Fatalf("wait keys after cicd ack = %q, want only the identity wait", keys)
	}
	assertDomainOnlyKey(t, keys[0], string(reducer.DomainContainerImageIdentity))

	// Activate the identity producer scope: the last wait reopens.
	activateProducerGenWithKind(t, ctx, base, identityScope, "i1", scope.CollectorOCIRegistry, &clock)
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-multi"); len(keys) != 0 {
		t.Fatalf("wait keys after identity activation = %q, want none", keys)
	}
	state := readWaitingRow(t, ctx, base, "wait-11000-multi")
	if state.status != "retrying" || state.attempts != 1 {
		t.Fatalf("reopened state = %+v, want retrying at attempt 1", state)
	}
}

// runtimeNotReadyCause self-classifies as the service-catalog runtime
// readiness miss, parking through the narrowed same-scope producer row.
type runtimeNotReadyCause struct{}

func (runtimeNotReadyCause) Error() string   { return "service catalog runtime not ready" }
func (runtimeNotReadyCause) Retryable() bool { return true }
func (runtimeNotReadyCause) FailureClass() string {
	return servicecatalog.RuntimeNotReadyFailureClass
}

// TestCrossScopeProducerWakeNonInterferenceLive: the existing same-scope
// narrowed producer waits keep their byte-identical keys and wakes while
// the new domain-only waits and wakes operate beside them. The narrowed
// half is a GREEN guard; the cross-scope half is RED before #11000.
func TestCrossScopeProducerWakeNonInterferenceLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)

	narrowScope := "waiting-11000-narrow"
	consumerScope := "waiting-11000-consumer-ni"
	identityScope := "waiting-11000-producer-ni"
	addOwnGateGenerationSeed(t, ctx, base, narrowScope, "ng0", "ng1", clock)
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "cg0", "cg1", clock)
	busyProducerScope(t, ctx, base, identityScope, scope.CollectorOCIRegistry, "ig0", "ig1", "cross-scope-11000", clock)

	// Park the narrowed same-scope wait through the production Fail path
	// with a self-classifying cause, exactly as the handler deferral does.
	addOwnGateItem(t, ctx, base, "wait-11000-narrow", narrowScope, "ng1", reducer.DomainWorkloadMaterialization, clock)
	narrowQueue, narrowClaimed := claimWaitingRow(t, ctx, base, &clock, reducer.DomainWorkloadMaterialization)
	if narrowClaimed.IntentID != "wait-11000-narrow" {
		t.Fatalf("claimed narrowed = %q, want wait-11000-narrow", narrowClaimed.IntentID)
	}
	if err := narrowQueue.Fail(ctx, narrowClaimed, runtimeNotReadyCause{}); err != nil {
		t.Fatalf("fail narrowed wait: %v", err)
	}
	keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-narrow")
	if len(keys) != 1 {
		t.Fatalf("narrowed wait keys = %q, want one", keys)
	}
	// Byte-identical narrowed key: scope, generation, and producer domain
	// (jsonb-normalized spacing, as the column stores it).
	if want := `{"scope_id": "waiting-11000-narrow", "generation_id": "ng1", "producer_domain": "workload_materialization"}`; keys[0] != want {
		t.Fatalf("narrowed wait key = %q, want byte-identical %q", keys[0], want)
	}

	// Park the cross-scope domain wait beside it.
	addOwnGateItem(t, ctx, base, "wait-11000-ni", consumerScope, "cg1", reducer.DomainCICDRunCorrelation, clock)
	parkCrossScopeConsumer(t, ctx, base, &clock, reducer.DomainCICDRunCorrelation, reducer.DomainContainerImageIdentity)

	// Ack the narrowed producer in its own scope and generation: only the
	// narrowed wait reopens.
	addOwnGateItem(t, ctx, base, "prod-11000-wm", narrowScope, "ng1", reducer.DomainWorkloadMaterialization, clock)
	producerQueue, producerIntent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainWorkloadMaterialization)
	if err := producerQueue.Ack(ctx, producerIntent, reducercontract.Result{}); err != nil {
		t.Fatalf("ack narrowed producer: %v", err)
	}
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-narrow"); len(keys) != 0 {
		t.Fatalf("narrowed wait keys after scope ack = %q, want none", keys)
	}
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-ni"); len(keys) != 1 {
		t.Fatalf("cross-scope wait keys after unrelated scope ack = %q, want the parked wait untouched", keys)
	}

	// Activate the identity producer scope: the cross-scope wait reopens.
	activateProducerGenWithKind(t, ctx, base, identityScope, "ig1", scope.CollectorOCIRegistry, &clock)
	if keys := crossScopeWaitKeys(t, ctx, base, "wait-11000-ni"); len(keys) != 0 {
		t.Fatalf("cross-scope wait keys after identity activation = %q, want none", keys)
	}
}
