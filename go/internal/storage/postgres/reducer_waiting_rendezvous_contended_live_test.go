// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"testing"
	"time"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 contended rendezvous measurement: activation lock wait and park latency with concurrent activators and multi-scope parkers
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

var (
	rendezvousMeasureActivators = flag.Int("rendezvous-activators", 8, "concurrent activation stand-ins in TestRendezvousContendedLatencyLive")
	rendezvousMeasureParkers    = flag.Int("rendezvous-parkers", 8, "concurrent multi-scope parkers in TestRendezvousContendedLatencyLive")
	rendezvousMeasureScopes     = flag.Int("rendezvous-scopes", 32, "provider scopes in TestRendezvousContendedLatencyLive")
	rendezvousMeasureBlocking   = flag.Int("rendezvous-blocking", 20, "blocking scopes per parked wait in TestRendezvousContendedLatencyLive")
	rendezvousMeasureSeconds    = flag.Int("rendezvous-seconds", 6, "run length in TestRendezvousContendedLatencyLive")
)

// TestRendezvousContendedLatencyLive measures the rendezvous under the
// intended concurrency (#11256): A activators each loop a stand-in activation
// (the production exclusive lock, 5 ms of held work, commit) over random
// scopes, while P parkers loop the production awaitInFlightActivations over a
// random blocking set of B scopes. The conflict domain is one scope's
// activation, so the time an activator waits for its lock must stay near the
// work it queues behind (the 5 ms of one same-scope peer), never near the
// park bound. A park that waits on one scope while holding another's lock
// turns one slow activation into a convoy and fails the bound below. The run
// logs the latency table the evidence note quotes.
func TestRendezvousContendedLatencyLive(t *testing.T) {
	ctx := context.Background()
	database, _ := openNarrowFenceDB(t, ctx)
	database.SetMaxOpenConns(2**rendezvousMeasureActivators + 2**rendezvousMeasureParkers + 8)
	scopes := make([]string, 0, *rendezvousMeasureScopes)
	for i := range *rendezvousMeasureScopes {
		scopes = append(scopes, fmt.Sprintf("scope-contended-%04d", i))
	}
	const activationWork = 5 * time.Millisecond
	deadline := time.Now().Add(time.Duration(*rendezvousMeasureSeconds) * time.Second)

	var mu sync.Mutex
	var lockWaits, parkTimes []time.Duration
	var activations, parks int
	var wg sync.WaitGroup
	for a := range *rendezvousMeasureActivators {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for time.Now().Before(deadline) {
				scope := scopes[rng.Intn(len(scopes))]
				tx, err := database.BeginTx(ctx, nil)
				if err != nil {
					t.Errorf("begin activation stand-in: %v", err)
					return
				}
				began := time.Now()
				if _, err := tx.ExecContext(ctx, activationRendezvousLockSQL, activationRendezvousKey(scope)); err != nil {
					_ = tx.Rollback()
					t.Errorf("activation lock: %v", err)
					return
				}
				waited := time.Since(began)
				time.Sleep(activationWork)
				if err := tx.Commit(); err != nil {
					t.Errorf("commit activation stand-in: %v", err)
					return
				}
				mu.Lock()
				lockWaits = append(lockWaits, waited)
				activations++
				mu.Unlock()
			}
		}(int64(a + 1))
	}
	for p := range *rendezvousMeasureParkers {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(1000 + seed))
			for time.Now().Before(deadline) {
				blocking := make([]string, 0, *rendezvousMeasureBlocking)
				for _, idx := range rng.Perm(len(scopes))[:min(*rendezvousMeasureBlocking, len(scopes))] {
					blocking = append(blocking, scopes[idx])
				}
				slices.Sort(blocking)
				rawKey, err := json.Marshal(waitingKey{
					ScopeID: "scope-waiter-contended", GenerationID: "gen-waiter-contended", BlockingScopeIDs: blocking,
				})
				if err != nil {
					t.Errorf("marshal key: %v", err)
					return
				}
				keyJSON := string(rawKey)
				began := time.Now()
				if err := awaitInFlightActivations(ctx, SQLDB{DB: database}, nil,
					WaitingKindRelationshipGenerationActive, keyJSON); err != nil {
					t.Errorf("awaitInFlightActivations: %v", err)
					return
				}
				took := time.Since(began)
				mu.Lock()
				parkTimes = append(parkTimes, took)
				parks++
				mu.Unlock()
			}
		}(int64(p + 1))
	}
	wg.Wait()

	slices.Sort(lockWaits)
	slices.Sort(parkTimes)
	t.Logf("activators=%d parkers=%d scopes=%d blocking=%d seconds=%d activations=%d parks=%d",
		*rendezvousMeasureActivators, *rendezvousMeasureParkers, *rendezvousMeasureScopes,
		*rendezvousMeasureBlocking, *rendezvousMeasureSeconds, activations, parks)
	t.Logf("activation lock wait: p50=%s p99=%s max=%s", rendezvousQuantile(lockWaits, 0.50), rendezvousQuantile(lockWaits, 0.99), rendezvousQuantile(lockWaits, 1))
	t.Logf("park rendezvous:      p50=%s p99=%s max=%s", rendezvousQuantile(parkTimes, 0.50), rendezvousQuantile(parkTimes, 0.99), rendezvousQuantile(parkTimes, 1))
	if activations == 0 || parks == 0 {
		t.Fatalf("no work done: activations=%d parks=%d", activations, parks)
	}
	if worst := rendezvousQuantile(lockWaits, 1); worst > narrowedParkRendezvousTimeout/2 {
		t.Fatalf("an activation waited %s for its lock, want under %s: a park is stalling an activation of a scope it is not waiting on",
			worst, narrowedParkRendezvousTimeout/2)
	}
}

// quantile returns the q-quantile of an ascending slice (q=1 is the maximum).
func rendezvousQuantile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * q)
	return sorted[idx].Round(10 * time.Microsecond)
}
