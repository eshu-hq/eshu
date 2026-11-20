// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestServiceRunBatchPrefetchStopReleasesFullBuffer proves shutdown drains a
// full buffer without executing or acking it: with 2 workers blocked and 4
// items hold-heartbeated in the buffer, cancel returns promptly, every
// buffered item records ack_outcome_unknown exactly once, and the buffered
// intents never reach the executor or the ack path. This is the deadlock
// regression gate for the buffer: a claimer blocked on a full buffer must
// always observe cancellation.
func TestServiceRunBatchPrefetchStopReleasesFullBuffer(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(6)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 6),
	}
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     6,
		BatchPrefetchDepth: 4,
		Instruments:        instruments,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	executed := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-executor.started:
			executed[id] = true
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}
	// Both workers are blocked in execution, so the claimer must have
	// refilled the drained buffer to 6 and parked on the full buffer by now:
	// poll for that quiescent count, otherwise the drain below races the
	// refill and the unknown count comes out short.
	pollDeadline := time.Now().Add(5 * time.Second)
	for source.claimedCount() != 6 {
		if time.Now().After(pollDeadline) {
			t.Fatalf("claimed count while both workers are busy = %d, want 6 (first claim 4 plus a 2-item refill)", source.claimedCount())
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context ended waiting for the prefetch refill: %v", ctx.Err())
		}
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil after a clean stop-drain", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runMainLoop() did not return promptly after cancel with a full buffer")
	}

	if len(executed) != 2 {
		t.Fatalf("executed intents = %v, want exactly the 2 popped before cancel (buffered items never execute)", executed)
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	// The executing pair went through Fail after its cancelled Execute
	// returned (the pre-existing in-flight path); the buffered four were
	// never executed, so nothing acks and nothing else fails.
	if acked != 0 || failed != 2 {
		t.Fatalf("sink acked/failed = %d/%d, want 0/2 (stop-drain never acks; only the executing pair fails)", acked, failed)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	if counts["ack_outcome_unknown"] != 4 {
		t.Fatalf("ack_outcome_unknown count = %d, want 4 (one per buffered item, none for the 2 executing)", counts["ack_outcome_unknown"])
	}
	if got := prefetchDwellCount(t, reader); got != 2 {
		t.Fatalf("buffer dwell observations = %d, want 2 (the popped pair recorded a dwell before the stop check; drained items record none)", got)
	}
}

// TestServiceRunBatchPrefetchFIFOOrder proves the buffer preserves ClaimBatch
// return order with overtake bound 0. The assertion compares pair sets, not
// positions: a worker can be descheduled between its pop and its execution
// record, so within a pair either order is legal. What FIFO forbids is an
// item crossing the pair boundary, so gates stay closed until their pair is
// fully recorded and no worker can advance past a closed gate.
func TestServiceRunBatchPrefetchFIFOOrder(t *testing.T) {
	t.Parallel()

	intents := makeTestIntents(4)
	ids := []string{intents[0].IntentID, intents[1].IntentID, intents[2].IntentID, intents[3].IntentID}
	source := &fakeBatchWorkSource{intents: intents}
	sink := &fakeBatchWorkSink{}
	executor := newGatedStartReducerExecutor(ids[0], ids[1], ids[2], ids[3])
	// All gates start closed: workers pop the claimed pair and park before
	// recording, so the first recorded pair can only be the first two
	// claimed. Only then do the gates open.

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     4,
		BatchPrefetchDepth: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	waitStarts := func(n int) []string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got := executor.startOrder(); len(got) >= n {
				return got
			}
			select {
			case <-time.After(5 * time.Millisecond):
			case <-ctx.Done():
				t.Fatalf("context ended waiting for %d execution starts: %v", n, ctx.Err())
			}
		}
		t.Fatalf("timed out waiting for %d execution starts, got %v", n, executor.startOrder())
		return nil
	}

	pairSet := func(starts []string) map[string]bool {
		t.Helper()
		seen := make(map[string]bool, len(starts))
		for _, id := range starts {
			seen[id] = true
		}
		return seen
	}
	first := pairSet(waitStarts(2))
	if len(first) != 2 || !first[ids[0]] || !first[ids[1]] {
		t.Fatalf("first execution starts record %v, want exactly the claimed pair {%s %s}", first, ids[0], ids[1])
	}
	executor.open(ids[0])
	executor.open(ids[1])
	second := pairSet(waitStarts(4)[2:])
	if len(second) != 2 || !second[ids[2]] || !second[ids[3]] {
		t.Fatalf("second execution starts record %v, want exactly the buffered pair {%s %s}", second, ids[2], ids[3])
	}
	executor.open(ids[2])
	executor.open(ids[3])

	// Wait until every execution settled through the acker, then end the
	// run with an explicit cancel: the empty-poll wait would race buffered
	// items still waiting for their pop, and the background timeout alone
	// would leave the ending to a coin flip.
	ackDeadline := time.Now().Add(5 * time.Second)
	for {
		sink.mu.Lock()
		acked := sink.acked
		sink.mu.Unlock()
		if acked == 4 {
			break
		}
		if time.Now().After(ackDeadline) {
			t.Fatalf("sink acked did not reach 4, got %d", acked)
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context ended waiting for acks: %v", ctx.Err())
		}
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runMainLoop() did not return after cancel")
	}
	sink.mu.Lock()
	acked := sink.acked
	sink.mu.Unlock()
	if acked != 4 {
		t.Fatalf("sink acked = %d, want 4", acked)
	}
}

// TestServiceRunBatchPrefetchDepthGaugeTrajectory proves the buffer depth
// gauge reads 0 with an empty buffer, B with a full one, and 0 again after
// the drain: with 2 workers blocked and depth 2, the gauge must show 2
// mid-run and return to 0 at the end.
func TestServiceRunBatchPrefetchDepthGaugeTrajectory(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(4)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 4),
	}
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     4,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-executor.started:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}
	// Both workers execute while 2 holds sit buffered: the gauge must show
	// the full buffer. Poll with a deadline: insertion always completes
	// because both workers were already parked in pop.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := prefetchDepthValue(t, reader); got == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("buffer depth gauge = %d, want 2 while both workers are busy", prefetchDepthValue(t, reader))
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context ended waiting for full buffer: %v", ctx.Err())
		}
	}

	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := prefetchDepthValue(t, reader); got != 0 {
		t.Fatalf("buffer depth gauge after drain = %d, want 0", got)
	}
}

// TestServiceRunBatchPrefetchKeepsClaimSizeEmission proves the prefetch path
// records eshu_dp_reducer_batch_claim_size exactly once per ClaimBatch call,
// so claim admission stays observable on the same boundaries as the
// rendezvous path.
func TestServiceRunBatchPrefetchKeepsClaimSizeEmission(t *testing.T) {
	t.Parallel()

	instruments, reader := batchTelemetry(t)
	source := &fakeBatchWorkSource{intents: makeTestIntents(2)}
	service := Service{
		PollInterval:       time.Millisecond,
		WorkSource:         source,
		Executor:           &countingExecutor{},
		WorkSink:           &fakeBatchWorkSink{},
		Workers:            2,
		BatchClaimSize:     2,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
	}
	// A timeout context ends the run after the two intents settle:
	// cancelling on the first empty poll would race the buffered items
	// still waiting for their pop.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.runMainLoop(ctx); err != nil {
		t.Fatalf("runMainLoop() error = %v", err)
	}
	claims := len(source.claimLimits())
	if claims == 0 {
		t.Fatal("fake source saw no ClaimBatch calls")
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var (
		count uint64
		sum   int64
	)
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_claim_size" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("batch claim size data = %T, want Histogram[int64]", m.Data)
			}
			for _, point := range histogram.DataPoints {
				value, ok := point.Attributes.Value(attribute.Key("queue"))
				if !ok || value.AsString() != "reducer" {
					t.Fatalf("batch claim size point missing queue=reducer: %+v", point.Attributes)
				}
				count += point.Count
				sum += point.Sum
			}
		}
	}
	if count != uint64(claims) {
		t.Fatalf("batch claim size observations = %d, want one per ClaimBatch call (%d)", count, claims)
	}
	if sum != 2 {
		t.Fatalf("batch claim size sum = %d, want 2 (one two-item batch plus the terminal empty poll)", sum)
	}
}

func prefetchDepthValue(t *testing.T, reader *metric.ManualReader) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_buffer_depth" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("buffer depth data = %T, want UpDownCounter sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	return total
}
