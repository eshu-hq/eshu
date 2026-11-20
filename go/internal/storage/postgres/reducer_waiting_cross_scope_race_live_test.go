// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11000 cross-scope producer-terminal waits under -race: concurrent parks, a mid-flight producer reducer Ack, and a concurrent projector-Ack activation converge on exactly-once reopen with the attempt budget frozen; disposable database, about 45 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestCrossScopeProducerConcurrentWakeRaceLive: four consumers park on the
// same producer domain from independent connections while a producer
// reducer Ack and a projector-Ack activation race mid-flight. Every waiter
// reopens exactly once, no wait row lingers, and no attempt advances.
// RED before #11000: no wake matches the scope-keyed waits, so all four
// linger to the backstop.
func TestCrossScopeProducerConcurrentWakeRaceLive(t *testing.T) {
	ctx := context.Background()
	base, dsn := openWaitingDB(t, ctx)
	base.SetMaxOpenConns(8)
	clock := time.Now().UTC().Truncate(time.Second)

	consumerScope := "waiting-11000-consumer-race"
	producerScope := "waiting-11000-producer-race"
	addOwnGateGenerationSeed(t, ctx, base, consumerScope, "g0", "g1", clock)
	producerKindScope(t, ctx, base, producerScope, scope.CollectorOCIRegistry, "p0", clock)
	producerPendingGen(t, ctx, base, producerScope, "p1", clock)
	if err := seedProjectorRunningErr(ctx, base, producerScope, "p0", "race-keeper"); err != nil {
		t.Fatalf("seed keeper projector work: %v", err)
	}

	const parkers = 4
	ids := make([]string, 0, parkers)
	for i := 0; i < parkers; i++ {
		id := fmt.Sprintf("wait-11000-race-%d", i)
		ids = append(ids, id)
		addOwnGateItem(t, ctx, base, id, consumerScope, "g1", reducer.DomainCICDRunCorrelation, clock)
	}

	// Four parkers race to claim and defer from independent connections.
	// SKIP LOCKED deals each a distinct row; the parked set must be all four.
	parkedIDs := make(chan string, parkers)
	parkErrs := make(chan error, parkers)
	var parked sync.WaitGroup
	for i := 0; i < parkers; i++ {
		parked.Add(1)
		go func() {
			defer parked.Done()
			conn, err := sql.Open("pgx", dsn)
			if err != nil {
				parkErrs <- err
				return
			}
			defer func() { _ = conn.Close() }()
			queue := newOwnGateQueue(conn, &clock, reducer.DomainCICDRunCorrelation)
			intent, ok, err := queue.Claim(ctx)
			if err != nil || !ok {
				parkErrs <- fmt.Errorf("race claim: ok=%v err=%w", ok, err)
				return
			}
			if err := queue.Fail(ctx, intent, crossScopeProducerNotReady(
				reducer.DomainCICDRunCorrelation, intent.ScopeID, intent.GenerationID,
				reducer.DomainContainerImageIdentity)); err != nil {
				parkErrs <- err
				return
			}
			parkedIDs <- intent.IntentID
		}()
	}
	parked.Wait()
	close(parkedIDs)
	close(parkErrs)
	for err := range parkErrs {
		if err != nil {
			t.Fatalf("park racer: %v", err)
		}
	}
	seen := map[string]bool{}
	for id := range parkedIDs {
		seen[id] = true
		if keys := crossScopeWaitKeys(t, ctx, base, id); len(keys) != 1 {
			t.Fatalf("parked %s keys = %q, want one domain wait", id, keys)
		}
	}
	if len(seen) != parkers {
		t.Fatalf("parked ids = %v, want all %d consumers", seen, parkers)
	}

	// Mid-flight providers race: the producer reducer Ack and the projector
	// activation fire concurrently on independent connections.
	addOwnGateItem(t, ctx, base, "prod-11000-race", producerScope, "p0", reducer.DomainContainerImageIdentity, clock)
	var providers sync.WaitGroup
	providerErrs := make(chan error, 2)
	providers.Add(2)
	go func() {
		defer providers.Done()
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			providerErrs <- err
			return
		}
		defer func() { _ = conn.Close() }()
		queue := newOwnGateQueue(conn, &clock, reducer.DomainContainerImageIdentity)
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			providerErrs <- fmt.Errorf("race producer claim: ok=%v err=%w", ok, err)
			return
		}
		providerErrs <- queue.Ack(ctx, intent, reducercontract.Result{})
	}()
	go func() {
		defer providers.Done()
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			providerErrs <- err
			return
		}
		defer func() { _ = conn.Close() }()
		if err := seedProjectorRunningErr(ctx, conn, producerScope, "p1", "cross-scope-11000-race"); err != nil {
			providerErrs <- err
			return
		}
		queue := NewProjectorQueue(SQLDB{DB: conn}, "cross-scope-11000-race", 10*time.Minute)
		queue.Now = func() time.Time { return clock }
		work := projector.ScopeGenerationWork{
			Scope:        scope.IngestionScope{ScopeID: producerScope, CollectorKind: scope.CollectorOCIRegistry},
			Generation:   scope.ScopeGeneration{GenerationID: "p1"},
			AttemptCount: 1,
		}
		providerErrs <- queue.Ack(ctx, work, runtime.Result{})
	}()
	providers.Wait()
	close(providerErrs)
	for err := range providerErrs {
		if err != nil {
			t.Fatalf("provider racer: %v", err)
		}
	}

	for _, id := range ids {
		if keys := crossScopeWaitKeys(t, ctx, base, id); len(keys) != 0 {
			t.Fatalf("wait keys for %s after racing providers = %q, want none", id, keys)
		}
		state := readWaitingRow(t, ctx, base, id)
		if state.status != "retrying" || state.attempts != 1 {
			t.Fatalf("reopened %s = %+v, want retrying at attempt 1", id, state)
		}
	}
}
