// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// memoryProjectorQueue is a hermetic, in-memory stand-in for the durable
// fact_work_items queue that couples concurrentreplay.Driver's commit step to
// projector.Service's claim step in production. It implements
// collector.Committer (the commit side Driver drives) and
// projector.ProjectorWorkSource / projector.FactStore / projector.
// ProjectorWorkSink (the three claim-side contracts projector.Service.Run
// depends on), backed by one mutex-guarded FIFO plus a facts map instead of
// Postgres.
//
// It intentionally reimplements none of projector.Service's own logic (the
// claim/wait loop, heartbeat, large-generation semaphore, telemetry) — only
// the durable queue that logic depends on through Service's own exported
// interfaces. Driving the real projector.Service.Run loop over this fake,
// rather than reimplementing claim/drain/ack here, is what makes
// TestDriverCommitsFeedProjectorClaimDrainAck a meaningful proof of wiring
// instead of a self-referential one: see go/internal/projector/service.go's
// ProjectorWorkSource, FactStore, and ProjectorWorkSink interfaces.
type memoryProjectorQueue struct {
	mu          sync.Mutex
	pending     []projector.ScopeGenerationWork // FIFO of committed, unclaimed work
	factsByKey  map[string][]facts.Envelope     // scope:generation -> committed facts
	ackOrder    []string                        // scope:generation keys, in ack order
	commitCount int
	failCount   int
}

// newMemoryProjectorQueue returns an empty memoryProjectorQueue ready for a
// Driver to commit into and a projector.Service to drain from.
func newMemoryProjectorQueue() *memoryProjectorQueue {
	return &memoryProjectorQueue{factsByKey: make(map[string][]facts.Envelope)}
}

// projectorQueueKey is the scope-generation identity memoryProjectorQueue
// indexes committed facts and acks by.
func projectorQueueKey(scopeID, generationID string) string {
	return fmt.Sprintf("%s:%s", scopeID, generationID)
}

// CommitScopeGeneration implements collector.Committer. It drains factStream
// (mirroring how a real committer consumes the fact channel), records the
// envelopes under the scope-generation key, and enqueues a
// projector.ScopeGenerationWork item — the in-memory analogue of a Postgres
// commit that both writes fact_records and enqueues a fact_work_items row for
// the same generation.
func (q *memoryProjectorQueue) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	var envs []facts.Envelope
	for env := range factStream {
		envs = append(envs, env)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	key := projectorQueueKey(scopeValue.ScopeID, generation.GenerationID)
	q.factsByKey[key] = envs
	q.pending = append(q.pending, projector.ScopeGenerationWork{Scope: scopeValue, Generation: generation})
	q.commitCount++
	return nil
}

// Claim implements projector.ProjectorWorkSource: pop the oldest pending work
// item, or report ok=false when nothing is pending, matching the empty-poll
// contract projector.Service.Run already knows how to wait out.
func (q *memoryProjectorQueue) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return projector.ScopeGenerationWork{}, false, nil
	}
	work := q.pending[0]
	q.pending = q.pending[1:]
	return work, true, nil
}

// LoadFacts implements projector.FactStore: return the envelopes committed
// for work's scope generation.
func (q *memoryProjectorQueue) LoadFacts(_ context.Context, work projector.ScopeGenerationWork) ([]facts.Envelope, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := projectorQueueKey(work.Scope.ScopeID, work.Generation.GenerationID)
	envs, ok := q.factsByKey[key]
	if !ok {
		return nil, fmt.Errorf("memoryProjectorQueue: no committed facts for %q", key)
	}
	cloned := make([]facts.Envelope, len(envs))
	copy(cloned, envs)
	return cloned, nil
}

// Ack implements projector.ProjectorWorkSink: mark the work item drained.
func (q *memoryProjectorQueue) Ack(_ context.Context, work projector.ScopeGenerationWork, _ projector.Result) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ackOrder = append(q.ackOrder, projectorQueueKey(work.Scope.ScopeID, work.Generation.GenerationID))
	return nil
}

// Fail implements projector.ProjectorWorkSink. The happy-path drain test
// expects this is never called; recording the count lets the test assert
// that explicitly instead of assuming it.
func (q *memoryProjectorQueue) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failCount++
	return nil
}

// snapshot returns the queue's residual pending count, total commits
// observed, a copy of the ack order recorded so far, and the fail count.
// Safe to call once Driver.Run and/or projector.Service.Run have returned.
func (q *memoryProjectorQueue) snapshot() (pending, committed int, ackedKeys []string, failed int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, len(q.ackOrder))
	copy(out, q.ackOrder)
	return len(q.pending), q.commitCount, out, q.failCount
}

// recordingProjectionRunner is a trivial projector.ProjectionRunner: it does
// no real projection, only records which scope generations it was asked to
// project, under a mutex, so the test can assert every committed generation
// reached the runner exactly once. Projection logic itself is out of scope
// for this hermetic drain-semantics proof; only the claim -> load -> run ->
// ack sequencing projector.Service.Run performs is under test.
type recordingProjectionRunner struct {
	mu    sync.Mutex
	calls []string
}

// Project implements projector.ProjectionRunner by recording the call and
// returning a Result that echoes the scope generation it was given.
func (r *recordingProjectionRunner) Project(
	_ context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	r.mu.Lock()
	r.calls = append(r.calls, projectorQueueKey(scopeValue.ScopeID, generation.GenerationID))
	r.mu.Unlock()
	return projector.Result{ScopeID: scopeValue.ScopeID, GenerationID: generation.GenerationID}, nil
}

// snapshot returns a copy of the recorded call keys.
func (r *recordingProjectionRunner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// hermeticReplaySource is a minimal collector.Source that hands out n
// distinct generations, each carrying two fact envelopes (via the
// factEnvelope helper factslicesource_test.go already defines), then reports
// permanent exhaustion. It plays the same "recorded tape" role as
// driver_test.go's scriptedGenerationSource, but with non-empty facts so the
// projector side of this test's FactStore.LoadFacts has real content to hand
// the runner rather than an always-empty stream.
type hermeticReplaySource struct {
	mu   sync.Mutex
	next int
	gens []collector.CollectedGeneration
}

// newHermeticReplaySource builds a hermeticReplaySource with n distinct
// generations, each with a unique scope/generation ID and two fact envelopes.
func newHermeticReplaySource(n int) *hermeticReplaySource {
	gens := make([]collector.CollectedGeneration, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("drain-gen-%03d", i)
		scopeID := "drain-scope-" + id
		envs := []facts.Envelope{
			factEnvelope(scopeID, id, id+"-fact-0"),
			factEnvelope(scopeID, id, id+"-fact-1"),
		}
		gens = append(gens, collector.FactsFromSlice(
			scope.IngestionScope{ScopeID: scopeID, SourceSystem: "fake"},
			scope.ScopeGeneration{GenerationID: id},
			envs,
		))
	}
	return &hermeticReplaySource{gens: gens}
}

// Next returns the next scripted generation, or permanent exhaustion once the
// tape is consumed. Safe for concurrent use on its own (guarded by its own
// mutex), independent of any Source wrapper.
func (s *hermeticReplaySource) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.gens) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := s.gens[s.next]
	s.next++
	return gen, true, nil
}

// TestDriverCommitsFeedProjectorClaimDrainAck is the hermetic (no
// docker/Postgres) approximation of the Ifá P2 pipeline's claim/drain
// contract: it proves that every generation concurrentreplay.Driver commits
// becomes a work item the REAL projector.Service.Run loop then claims, loads,
// projects, and acks. The claim/load/project/ack sequencing under test is
// projector.Service's own unmodified logic (go/internal/projector/service.go)
// — memoryProjectorQueue only stands in for the fact_work_items queue that
// logic depends on through its own ProjectorWorkSource, FactStore, and
// ProjectorWorkSink interfaces, so this test cannot be satisfied by a stub
// that merely echoes back what it was given.
//
// This is a hermetic approximation, not the authoritative proof: the real
// fact_work_items residual-zero assertion against a live Postgres queue is
// delivered by the full-stack slice of #4395 (slice 6; see this package's
// README "Same-DB idempotency caveat" section, which already reserves that
// proof for slice 6 rather than this one).
//
// Meaningful under -race: Driver runs 6 concurrent workers committing into
// memoryProjectorQueue, and projector.Service.Run then runs 4 concurrent
// workers claiming from the same queue — every shared field mutation on both
// sides is guarded by its own mutex.
func TestDriverCommitsFeedProjectorClaimDrainAck(t *testing.T) {
	t.Parallel()

	const generationCount = 30
	delegate := newHermeticReplaySource(generationCount)
	src := concurrentreplay.NewSource(delegate)
	queue := newMemoryProjectorQueue()

	driver := concurrentreplay.Driver{
		Source:    src,
		Committer: queue,
		Workers:   6,
	}

	driverCtx, driverCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer driverCancel()

	report, err := driver.Run(driverCtx)
	if err != nil {
		t.Fatalf("Driver.Run: unexpected error %v", err)
	}
	if got, want := report.GenerationsCommitted, generationCount; got != want {
		t.Fatalf("Driver.Run: GenerationsCommitted = %d, want %d", got, want)
	}

	pendingAfterCommit, committedAfterCommit, _, _ := queue.snapshot()
	if got, want := pendingAfterCommit, generationCount; got != want {
		t.Fatalf("queue pending after Driver.Run = %d, want %d (every commit must enqueue a work item)", got, want)
	}
	if got, want := committedAfterCommit, generationCount; got != want {
		t.Fatalf("queue commitCount after Driver.Run = %d, want %d", got, want)
	}

	runner := &recordingProjectionRunner{}
	service := projector.Service{
		PollInterval: time.Millisecond,
		WorkSource:   queue,
		FactStore:    queue,
		Runner:       runner,
		WorkSink:     queue,
		// Every commit already landed in queue before Service.Run starts (the
		// assertions above prove that half of the pipeline). Once the queue
		// reports empty, stop immediately rather than polling forever — the
		// same pre-loaded-queue pattern projector's own
		// TestServiceRunConcurrentMultipleItems uses.
		Wait:    func(context.Context, time.Duration) error { return context.Canceled },
		Workers: 4,
	}

	serviceCtx, serviceCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer serviceCancel()

	if err := service.Run(serviceCtx); err != nil {
		t.Fatalf("projector.Service.Run: unexpected error %v", err)
	}
	if serviceCtx.Err() != nil {
		t.Fatalf("projector.Service.Run: context deadline exceeded before Run returned (possible deadlock): %v", serviceCtx.Err())
	}

	pendingAfterDrain, _, ackedKeys, failed := queue.snapshot()
	if got, want := pendingAfterDrain, 0; got != want {
		t.Fatalf("queue pending after Service.Run = %d, want %d (residual work left unclaimed)", got, want)
	}
	if got, want := failed, 0; got != want {
		t.Fatalf("queue failCount = %d, want %d (no work item should have failed projection)", got, want)
	}
	if got, want := len(ackedKeys), generationCount; got != want {
		t.Fatalf("acked %d work items, want exactly %d (residual 0, nothing left unacked)", got, want)
	}

	seen := make(map[string]int, len(ackedKeys))
	for _, key := range ackedKeys {
		seen[key]++
	}
	if got, want := len(seen), generationCount; got != want {
		t.Fatalf("acked %d unique work items, want %d", got, want)
	}
	for key, count := range seen {
		if count != 1 {
			t.Errorf("work item %q acked %d times, want exactly once", key, count)
		}
	}

	runnerCalls := runner.snapshot()
	if got, want := len(runnerCalls), generationCount; got != want {
		t.Fatalf("runner.Project called %d times, want %d", got, want)
	}
}
