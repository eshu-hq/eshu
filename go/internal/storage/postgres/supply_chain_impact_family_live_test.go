// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	impactdigest "github.com/eshu-hq/eshu/go/internal/ifa/impact/digest"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	projectorimpact "github.com/eshu-hq/eshu/go/internal/projector/supply/chain/impact"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The impact family of the Ifá fault and determinism gates (#10880 PR-C2; arbiter-impact-ifa section 2): a cassette
// driven through the REAL pipeline entry points, the real reducer queue and the real impact handler, in process, at 1,
// 2 and 4 workers. The impact domain writes no graph, so the assertion is a digest of the supports and winners tables,
// which must be equal across worker counts and to the fault-free baseline.
//
// Seeding uses no raw SQL for facts, generations or work items: the replay driver commits the cassettes through the
// ingestion store (as `ifa drive` does), the projector queue's own Claim and Ack activate each generation (the real
// pipeline's activation, with the graph projection, which impact never reads, skipped), the projector's own
// BuildSupplyChainImpactReducerIntent builds each impact intent from the generation's facts, and the reducer queue's
// Enqueue stores them. The production wiring is otherwise unchanged: the handler runs with its fan-out store and the
// queue's lease is the production field.
//
// This file uses only APIs present at the PR-C1 head, so the same family can be run there and its digest compared with
// the PR-C2 head's (the accuracy proof that supports built from the writer's rows equal supports built from facts).
// The faults, the duplicate delivery and the trigger assertions are in the sibling file, which is PR-C2 only.

// impactFamily is one disposable database with the family's seeded intents and the counters a run needs.
type impactFamily struct {
	t   *testing.T
	ctx context.Context
	db  *sql.DB
	// intents is every impact intent the seeding enqueued, as the projector's builder produced it.
	intents []projectorintent.ReducerIntent

	mu         sync.Mutex
	executions map[string]int
	claimed    map[string]reducercontract.Intent
	failures   []error
}

// newImpactFamily opens an ARMED disposable database (a clone of the bootstrapped template: the finding-fact reject
// trigger is enabled, as in production) and seeds the cassettes through the real path.
func newImpactFamily(t *testing.T) *impactFamily {
	t.Helper()
	_ = supplyChainImpactLiveDSN(t, "#10880 PR-C2 impact family")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	f := &impactFamily{t: t, ctx: ctx, db: db, executions: map[string]int{}, claimed: map[string]reducercontract.Intent{}}
	f.seed()
	return f
}

// seed commits the cassettes, activates their generations through the projector queue, builds and enqueues the impact
// intents, and settles the supports backfill marker (a fresh install marks immediately; the writers carry the
// maintainer's setting, so no scope is marked dirty afterwards).
func (f *impactFamily) seed() {
	f.t.Helper()
	committer := postgres.NewIngestionStore(postgres.SQLDB{DB: f.db})
	for _, relative := range impactdigest.Cassettes {
		path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "testdata", "cassettes", relative))
		if err != nil {
			f.t.Fatalf("resolve cassette %s: %v", relative, err)
		}
		var delegate collector.Source
		if delegate, err = cassette.NewSource(path); err != nil {
			f.t.Fatalf("load cassette %s: %v", relative, err)
		}
		driver := concurrentreplay.Driver{Source: concurrentreplay.NewSource(delegate), Committer: committer, Workers: 1}
		if _, err := driver.Run(f.ctx); err != nil {
			f.t.Fatalf("drive cassette %s: %v", relative, err)
		}
	}

	projectorQueue := postgres.NewProjectorQueue(postgres.SQLDB{DB: f.db}, "family-projector", time.Minute)
	factStore := postgres.NewFactStore(postgres.SQLDB{DB: f.db})
	reducerQueue := f.queue("family-seeder", time.Minute)
	for {
		work, claimed, err := projectorQueue.Claim(f.ctx)
		if err != nil {
			f.t.Fatalf("claim projector work: %v", err)
		}
		if !claimed {
			break
		}
		envelopes, err := factStore.LoadFacts(f.ctx, work)
		if err != nil {
			f.t.Fatalf("load facts of %s: %v", work.Scope.ScopeID, err)
		}
		if intent, ok := projectorimpact.BuildSupplyChainImpactReducerIntent(work.Scope.ScopeID, work.Generation.GenerationID, projectorintent.NewFactLookup(envelopes)); ok {
			if _, err := reducerQueue.Enqueue(f.ctx, []projectorintent.ReducerIntent{intent}); err != nil {
				f.t.Fatalf("enqueue the impact intent of %s: %v", work.Scope.ScopeID, err)
			}
			f.intents = append(f.intents, intent)
		}
		if err := projectorQueue.Ack(f.ctx, work, projectorruntime.Result{}); err != nil {
			f.t.Fatalf("ack (activate) %s: %v", work.Scope.ScopeID, err)
		}
	}
	if len(f.intents) == 0 {
		f.t.Fatal("the cassettes produced no impact intent: the family would pass on nothing")
	}
	if err := settleSupportsWithBackfill(f.ctx, f.db); err != nil {
		f.t.Fatalf("settle the supports backfill marker: %v", err)
	}
}

// queue is a reducer queue over the family's database claiming only impact work. lease is the production field: the
// fault cells pass a short REAL lease and rely on real elapsed time, never an injected clock (a fake clock would create a
// lease state the owed marker's database-clock fence cannot have).
func (f *impactFamily) queue(owner string, lease time.Duration) postgres.ReducerQueue {
	q := postgres.NewReducerQueue(postgres.SQLDB{DB: f.db}, owner, lease)
	q.RetryDelay, q.MaxAttempts = 200*time.Millisecond, 5
	q.ClaimDomains = []reducercontract.Domain{reducercontract.DomainSupplyChainImpact}
	return q
}

// familyWorker is one reducer worker: a queue and the production impact handler, wired as the reducer wires it (the
// fan-out store carries the worker's own lease owner, which the owed marker's lease fence compares).
type familyWorker struct {
	family  *impactFamily
	queue   postgres.ReducerQueue
	handler supplychaincore.SupplyChainImpactHandler
	fanOut  postgres.SupplyChainImpactFanOutStore
}

// tokenWrap decorates the fencing token issuer of one worker. The lease-expiry fault parks the worker right after its
// token is issued, so the parked pass holds the older token of the two passes on its scope.
type tokenWrap func(supplychaincore.SupplyChainImpactFencingTokenIssuer) supplychaincore.SupplyChainImpactFencingTokenIssuer

// newWorker builds a worker. wrap decorates its fencing token issuer (nil uses it directly).
func (f *impactFamily) newWorker(owner string, lease time.Duration, wrap tokenWrap) *familyWorker {
	var evidence factload.FactLoader = postgres.NewFactStore(postgres.SQLDB{DB: f.db})
	var issuer supplychaincore.SupplyChainImpactFencingTokenIssuer = postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: f.db}}
	if wrap != nil {
		issuer = wrap(issuer)
	}
	fanOut := postgres.SupplyChainImpactFanOutStore{
		Beginner: postgres.SQLDB{DB: f.db}, DB: postgres.SQLDB{DB: f.db}, LeaseOwner: owner,
	}
	return &familyWorker{
		family: f, queue: f.queue(owner, lease), fanOut: fanOut,
		handler: supplychaincore.SupplyChainImpactHandler{
			FactLoader:         evidence,
			FencingTokenIssuer: issuer,
			Writer: supplychaincore.PostgresSupplyChainImpactWriter{
				DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: f.db}},
			},
			FanOut: fanOut,
		},
	}
}

// execute runs one claimed intent through the handler and acks or fails it as the reducer worker does, counting the
// execution first. It returns the handler error.
func (w *familyWorker) execute(intent reducercontract.Intent) error {
	w.family.mu.Lock()
	w.family.executions[intent.IntentID]++
	w.family.claimed[intent.IntentID] = intent
	w.family.mu.Unlock()
	result, err := w.handler.Handle(w.family.ctx, intent)
	if err != nil {
		_ = w.queue.Fail(w.family.ctx, intent, err)
		return err
	}
	return w.queue.Ack(w.family.ctx, intent, result)
}

// step claims and executes one intent. It reports whether one was claimed.
func (w *familyWorker) step() (bool, error) {
	intent, claimed, err := w.queue.Claim(w.family.ctx)
	if err != nil || !claimed {
		return false, err
	}
	if err := w.execute(intent); err != nil {
		w.family.mu.Lock()
		w.family.failures = append(w.family.failures, err)
		w.family.mu.Unlock()
	}
	return true, nil
}

// pendingErr counts the impact work items that are not terminal. It is safe to call from a worker goroutine.
func (f *impactFamily) pendingErr() (int, error) {
	var n int
	err := f.db.QueryRowContext(f.ctx, `
SELECT count(*) FROM fact_work_items
WHERE stage = 'reducer' AND domain = $1 AND status NOT IN ('succeeded', 'dead_letter', 'failed')`,
		string(reducercontract.DomainSupplyChainImpact)).Scan(&n)
	return n, err
}

// pending is pendingErr for the test goroutine.
func (f *impactFamily) pending() int {
	f.t.Helper()
	n, err := f.pendingErr()
	if err != nil {
		f.t.Fatalf("count pending impact work: %v", err)
	}
	return n
}

// drain runs workers concurrent workers over the real queue until no impact work is pending. A worker that finds
// nothing to claim waits briefly and re-checks, so a row held by a parked or abandoned worker is picked up once its
// real lease expires.
func (f *impactFamily) drain(workers int, lease time.Duration, wrap tokenWrap) {
	f.t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		worker := f.newWorker(fmt.Sprintf("family-worker-%d", i), lease, wrap)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f.ctx.Err() == nil {
				claimed, err := worker.step()
				if err != nil {
					errs <- err
					return
				}
				if !claimed {
					n, err := f.pendingErr()
					if err != nil {
						errs <- err
						return
					}
					if n == 0 {
						return
					}
					time.Sleep(25 * time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		f.t.Fatalf("a worker stopped on a queue error: %v", err)
	}
	if err := f.ctx.Err(); err != nil {
		f.t.Fatalf("the drain did not finish: %v (pending impact work %d)", err, f.pending())
	}
}

// resweep runs one full winners resweep, as the maintainer does once the supports are written.
func (f *impactFamily) resweep() {
	f.t.Helper()
	result, err := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: f.db}).ResweepWinners(f.ctx)
	if err != nil || result.Skipped {
		f.t.Fatalf("winners resweep: %+v, %v", result, err)
	}
}

// digest returns the family digest and requires the tables it covers to be non-empty (impactdigest.Read), so a
// family that derived nothing cannot pass.
func (f *impactFamily) digest() string {
	f.t.Helper()
	result, err := impactdigest.Read(f.ctx, f.db)
	if err != nil {
		f.t.Fatalf("%v", err)
	}
	return result.Digest
}

// deadLetters counts impact work items in a terminal failure state.
func (f *impactFamily) deadLetters() int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, `
SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND domain = $1 AND status IN ('dead_letter', 'failed')`,
		string(reducercontract.DomainSupplyChainImpact)).Scan(&n); err != nil {
		f.t.Fatalf("count dead letters: %v", err)
	}
	return n
}

// impactItems counts every impact work item ever created, including the ones the handler's fan-out reopened.
func (f *impactFamily) impactItems() int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, `
SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND domain = $1`, string(reducercontract.DomainSupplyChainImpact)).Scan(&n); err != nil {
		f.t.Fatalf("count impact work items: %v", err)
	}
	return n
}

// totalExecutions sums the handler executions of every intent.
func (f *impactFamily) totalExecutions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, n := range f.executions {
		total += n
	}
	return total
}

// rowVersions returns, per primary key, the transaction id that wrote the current row version of table. A row that
// was not rewritten keeps its version, so equal maps mean no row version was written.
func (f *impactFamily) rowVersions(table, key string) map[string]string {
	f.t.Helper()
	rows, err := f.db.QueryContext(f.ctx, fmt.Sprintf(`SELECT %s::text, xmin::text FROM %s`, key, table))
	if err != nil {
		f.t.Fatalf("read row versions of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var pk, version string
		if err := rows.Scan(&pk, &version); err != nil {
			f.t.Fatalf("scan row versions of %s: %v", table, err)
		}
		out[pk] = version
	}
	return out
}

// derivedRowVersions is the row versions of every table the family digests plus the retracted keys: the rows a stale or
// duplicate pass must not rewrite.
func (f *impactFamily) derivedRowVersions() map[string]map[string]string {
	return map[string]map[string]string{
		"supports":       f.rowVersions("supply_chain_impact_finding_supports", "fact_id"),
		"winners":        f.rowVersions("supply_chain_impact_canonical_winners", "canonical_key"),
		"retracted keys": f.rowVersions("supply_chain_impact_retracted_finding_keys", "generation_id || '/' || scope_id || '/' || key_hash"),
	}
}

// rowVersionsEqual reports the tables whose row versions differ between a and b.
func rowVersionsEqual(a, b map[string]map[string]string) []string {
	var differing []string
	for table, before := range a {
		after := b[table]
		same := len(before) == len(after)
		for key, version := range before {
			if after[key] != version {
				same = false
			}
		}
		if !same {
			differing = append(differing, table)
		}
	}
	sort.Strings(differing)
	return differing
}

// requireFamilyDrained fails unless the drain left no dead letter, no failed attempt and no unexpected execution: in a
// fault-free run every impact work item was executed exactly once.
func (f *impactFamily) requireFaultFree() {
	f.t.Helper()
	if n := f.deadLetters(); n != 0 {
		f.t.Fatalf("%d impact work items are dead-lettered, want 0", n)
	}
	f.mu.Lock()
	failures := append([]error(nil), f.failures...)
	f.mu.Unlock()
	if len(failures) != 0 {
		f.t.Fatalf("%d handler executions failed in a fault-free run: %v", len(failures), failures)
	}
	if executions, items := f.totalExecutions(), f.impactItems(); executions != items {
		f.mu.Lock()
		breakdown := make([]string, 0, len(f.executions))
		for id, n := range f.executions {
			breakdown = append(breakdown, fmt.Sprintf("%s=%d", id, n))
		}
		f.mu.Unlock()
		sort.Strings(breakdown)
		f.t.Fatalf("handler executions = %d for %d impact work items, want one each in a fault-free run; per intent: %v", executions, items, breakdown)
	}
}

// familyBaseline runs the fault-free N=1 family once per test process and returns its digest and execution count.
var (
	familyBaselineOnce       sync.Once
	familyBaselineDigest     string
	familyBaselineExecutions int
)

// baseline returns the fault-free single-worker digest the faulted runs must equal. It runs in its own database.
func familyBaseline(t *testing.T) (digest string, executions int) {
	t.Helper()
	// Gate on the live DSN before the Once: newImpactFamily skips without one, and a
	// skip inside Once.Do marks it done with an empty digest, turning every later
	// caller into a "baseline did not run" failure instead of a clean skip.
	_ = supplyChainImpactLiveDSN(t, "#10880 PR-C2 impact family baseline")
	familyBaselineOnce.Do(func() {
		inner := newImpactFamily(t)
		inner.drain(1, time.Minute, nil)
		inner.resweep()
		inner.requireFaultFree()
		familyBaselineDigest, familyBaselineExecutions = inner.digest(), inner.totalExecutions()
	})
	if familyBaselineDigest == "" {
		t.Fatal("the fault-free baseline did not run (an earlier test failed inside it)")
	}
	return familyBaselineDigest, familyBaselineExecutions
}

// TestSupplyChainImpactSupportsFamilyDigestEqualAcrossWorkersLive drains the family at 1, 2 and 4 workers in separate
// databases and requires the same supports-and-winners digest, zero dead letters and exactly one handler execution per
// impact work item. It logs the digest so a run at the PR-C1 head can be compared with this head's.
func TestSupplyChainImpactSupportsFamilyDigestEqualAcrossWorkersLive(t *testing.T) {
	digests := map[int]string{}
	for _, workers := range []int{1, 2, 4} {
		f := newImpactFamily(t)
		f.drain(workers, time.Minute, nil)
		f.resweep()
		f.requireFaultFree()
		digests[workers] = f.digest()
		t.Logf("IMPACT_FAMILY_DIGEST workers=%d digest=%s executions=%d work_items=%d", workers, digests[workers], f.totalExecutions(), f.impactItems())
	}
	if digests[1] != digests[2] || digests[1] != digests[4] {
		t.Fatalf("the digest depends on the worker count: 1=%s 2=%s 4=%s", digests[1], digests[2], digests[4])
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "golden", "ifa-impact-family-digest.txt"))
	if err != nil {
		t.Fatalf("read the pinned digest: %v", err)
	}
	if pin := impactdigest.Pinned(string(raw)); pin != "" && pin != digests[1] {
		t.Fatalf("the family digest is %s, want the pinned %s (the Compose cell compares against the same pin)", digests[1], pin)
	}
}
