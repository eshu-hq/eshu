package cypher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// This file is the write-conflict handling proof for issue #3558. The retry
// design lives in retrying_executor.go (RetryingExecutor.ExecuteGroup retries a
// commit-time UNIQUE conflict when every statement in the group is
// MERGE-shaped). The existing tests in retrying_executor_test.go pin the
// single-threaded classification of injected errors. What remained unproven is
// the concurrency contract the project rule "Serialization Is Not A Fix"
// demands: that two workers driven into a real MERGE-vs-MERGE commit-time
// conflict on the SAME conflict key converge with no silent loss, no duplicate
// node, and via retry/idempotency rather than serialization.
//
// mergeConflictGraph is an in-memory stand-in for the NornicDB canonical store
// that reproduces that race deterministically. ExecuteGroup models a
// MERGE-shaped upsert to one shared uid (the Repository/Directory/Module class
// of node that multiple source-repo partitions legitimately write) as an
// optimistic read-then-commit: it snapshots the uid's commit version, waits on
// a one-shot read barrier so both writers observe the same pre-commit version,
// then on commit returns the typed NornicDB commit-time UNIQUE conflict if a
// concurrent writer advanced the version since the snapshot. Re-execution (the
// RetryingExecutor retry) re-snapshots and converges, exactly like an idempotent
// MERGE matching the now-committed node.
//
// The conflict domain is a single shared canonical uid. The transaction scope
// is one ExecuteGroup call; the retry scope is RetryingExecutor.runWithRetry.
// The write is idempotent under concurrent execution: the node is created at
// most once and every writer's contribution is recorded, so the proof covers
// the acceptance bar "prove the write is idempotent under concurrent execution".
type mergeConflictGraph struct {
	uid string

	mu           sync.Mutex
	version      int
	exists       bool
	createCount  int
	contributors map[string]struct{}
	conflicts    int
	seen         map[string]struct{}

	readBarrier *proofBarrier
}

func newMergeConflictGraph(uid string, writers int) *mergeConflictGraph {
	return &mergeConflictGraph{
		uid:          uid,
		contributors: make(map[string]struct{}),
		seen:         make(map[string]struct{}),
		readBarrier:  newProofBarrier(writers),
	}
}

func (g *mergeConflictGraph) Execute(_ context.Context, _ Statement) error {
	// The canonical phase-group write path uses ExecuteGroup; Execute is
	// unused here but required to satisfy the Executor interface.
	return nil
}

func (g *mergeConflictGraph) ExecuteGroup(_ context.Context, stmts []Statement) error {
	writer, _ := stmts[0].Parameters["writer"].(string)

	g.mu.Lock()
	_, retried := g.seen[writer]
	g.seen[writer] = struct{}{}
	snapshot := g.version
	g.mu.Unlock()

	// Only the first attempt of each distinct writer waits on the barrier, so
	// both writers hold the same pre-commit snapshot. A retrying writer must
	// not re-wait — the winner has already departed and would never arrive to
	// release a second round.
	if !retried {
		g.readBarrier.wait()
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.version != snapshot {
		// Lost the commit race: a concurrent writer committed the shared uid
		// between this writer's MERGE match and its commit. NornicDB surfaces
		// this as a commit-time UNIQUE conflict; re-executing the MERGE is safe
		// because it will match the now-committed node.
		g.conflicts++
		return typedCommitTimeUniqueConflict(g.uid)
	}
	if !g.exists {
		g.exists = true
		g.createCount++
	}
	g.contributors[writer] = struct{}{}
	g.version++
	return nil
}

// typedCommitTimeUniqueConflict returns the v1.0.45+ typed NornicDB commit-time
// UNIQUE conflict that isNornicDBCommitTimeUniqueConflictError classifies as
// retryable for a MERGE-shaped group.
func typedCommitTimeUniqueConflict(uid string) error {
	return &neo4jdriver.Neo4jError{
		Code: nornicDBTransactionCommitFailedCode,
		Msg: "commit failed: constraint violation: " +
			"Constraint violation (UNIQUE on Repository.[uid]): " +
			"Node with uid=" + uid + " already exists (nodeID: 508af30f)",
	}
}

func mergeStatementForWriter(uid, writer string) []Statement {
	return []Statement{
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    "UNWIND $rows AS row MERGE (r:Repository {uid: row.uid}) SET r.name = row.name",
			Parameters: map[string]any{
				"writer": writer,
				"rows":   []map[string]any{{"uid": uid, "name": writer}},
			},
		},
	}
}

// proofBarrier releases all waiters once n have arrived, then stays open. It
// makes the commit-time conflict deterministic so the proof is non-flaky under
// -race, instead of relying on a timing window.
type proofBarrier struct {
	mu      sync.Mutex
	cond    *sync.Cond
	n       int
	waiting int
	tripped bool
}

func newProofBarrier(n int) *proofBarrier {
	b := &proofBarrier{n: n}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *proofBarrier) wait() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tripped {
		return
	}
	b.waiting++
	if b.waiting >= b.n {
		b.tripped = true
		b.cond.Broadcast()
		return
	}
	for !b.tripped {
		b.cond.Wait()
	}
}

// TestRetryingExecutorConvergesUnderConcurrentMergeConflict is the positive
// proof for #3558: two workers MERGE the same shared uid concurrently and are
// driven into a deterministic commit-time UNIQUE conflict. Through the
// RetryingExecutor both converge — exactly one node is created (no duplicate),
// both writers' contributions land (no silent loss), both calls return nil
// (convergence via retry), and the retry metric fires (operator-visible
// contention signal). No worker-count reduction, batch-size-1, or
// single-threaded drain is used: the design absorbs the race.
func TestRetryingExecutorConvergesUnderConcurrentMergeConflict(t *testing.T) {
	t.Parallel()

	const uid = "1b579c9b2e26be17c853767e13c7c747"
	graph := newMergeConflictGraph(uid, 2)

	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	exec := &RetryingExecutor{
		Inner:       graph,
		MaxRetries:  3,
		BaseDelay:   1 * time.Millisecond,
		Instruments: instruments,
	}

	writers := []string{"projector-shard-a", "projector-shard-b"}
	errs := make([]error, len(writers))
	var wg sync.WaitGroup
	for i, w := range writers {
		wg.Add(1)
		go func(i int, w string) {
			defer wg.Done()
			errs[i] = exec.ExecuteGroup(context.Background(), mergeStatementForWriter(uid, w))
		}(i, w)
	}
	wg.Wait()

	for i, w := range writers {
		if errs[i] != nil {
			t.Fatalf("writer %q ExecuteGroup() error = %v, want nil (must converge via retry)", w, errs[i])
		}
	}

	graph.mu.Lock()
	defer graph.mu.Unlock()

	if graph.createCount != 1 {
		t.Fatalf("createCount = %d, want 1 (no duplicate node under concurrent MERGE)", graph.createCount)
	}
	if got, want := len(graph.contributors), len(writers); got != want {
		t.Fatalf("contributors = %d, want %d (no silent loss; every writer's MERGE must land)", got, want)
	}
	for _, w := range writers {
		if _, ok := graph.contributors[w]; !ok {
			t.Fatalf("writer %q contribution missing — silent loss under concurrency", w)
		}
	}
	if graph.conflicts < 1 {
		t.Fatal("conflicts = 0 — the test did not actually exercise a commit-time conflict (vacuous proof)")
	}

	if got := collectRetryCounter(t, reader); got < 1 {
		t.Fatalf("Neo4jDeadlockRetries counter = %d, want >=1 (retry must be operator-visible)", got)
	}
}

// TestBareGroupExecutorLosesConcurrentMergeWriteWithoutRetry is the
// failing-first companion: the SAME race, driven through the bare executor with
// NO RetryingExecutor, drops a write. It proves the conflict is real and that
// the retry layer — not serialization — is what makes the system converge.
func TestBareGroupExecutorLosesConcurrentMergeWriteWithoutRetry(t *testing.T) {
	t.Parallel()

	const uid = "deadbeefcafef00dba5eba11c0ffee00"
	graph := newMergeConflictGraph(uid, 2)

	writers := []string{"projector-shard-a", "projector-shard-b"}
	errs := make([]error, len(writers))
	var wg sync.WaitGroup
	for i, w := range writers {
		wg.Add(1)
		go func(i int, w string) {
			defer wg.Done()
			errs[i] = graph.ExecuteGroup(context.Background(), mergeStatementForWriter(uid, w))
		}(i, w)
	}
	wg.Wait()

	failed := 0
	for _, e := range errs {
		if e != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("bare-path failures = %d, want exactly 1 (the commit-race loser)", failed)
	}

	graph.mu.Lock()
	defer graph.mu.Unlock()
	if len(graph.contributors) != 1 {
		t.Fatalf("bare-path contributors = %d, want 1 — without retry the loser's write is silently lost", len(graph.contributors))
	}
}

// collectRetryCounter reads the Neo4jDeadlockRetries counter total from the
// manual reader. Returns 0 when the instrument has not recorded anything.
func collectRetryCounter(t *testing.T, reader metric.Reader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect() error = %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_neo4j_deadlock_retries_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}
