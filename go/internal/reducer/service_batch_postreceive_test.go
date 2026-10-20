// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestServiceBatchPostReceiveDropRecordsOnce pins the Site-B drop window
// (#8636): a worker that completes a handoff rendezvous receive and then
// observes shutdown before executing must record exactly one
// ack_outcome_unknown with its real worker ID and never execute the item.
// The afterWorkReceive hook lands cancel deterministically between the
// receive and the post-receive check; a cancel issued from the test
// goroutine would race the select rendezvous-vs-cancel outcome and prove
// nothing.
func TestServiceBatchPostReceiveDropRecordsOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instruments, reader := batchTelemetry(t)
	sink := &fakeBatchWorkSink{}
	var logs bytes.Buffer
	logOut := &mutexGuardedBuffer{mu: &sync.Mutex{}, buf: &logs}
	intents := makeTestIntents(1)
	source := &overLimitBatchWorkSource{intents: intents}
	executor := &countingExecutor{}
	service := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       executor,
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 1,
		Logger:         slog.New(slog.NewJSONHandler(logOut, nil)),
		Instruments:    instruments,
	}
	var once sync.Once
	service.afterWorkReceive = func() { once.Do(cancel) }

	done := make(chan error, 1)
	go func() { done <- service.runMainLoop(ctx) }()

	// Wait for the post-receive record. Ownership partitions by rendezvous
	// completion: the single staged item always completes its handoff (the
	// worker is parked in receive with a live context), so the hook always
	// fires and the item always takes the post-receive path — no other
	// outcome can record before it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		counts, _ := batchMetricStatusCounts(t, reader)
		if counts["ack_outcome_unknown"] >= 1 {
			break
		}
		if time.Now().After(deadline) {
			counts, _ := batchMetricStatusCounts(t, reader)
			t.Fatalf("post-receive drop was never recorded: statuses = %v, want ack_outcome_unknown for the received item (one claimed item silent)", counts)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil on cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runMainLoop() did not stop after cancellation")
	}
	if got := executor.count.Load(); got != 0 {
		t.Fatalf("executor calls = %d, want zero: a received-then-cancelled item must never execute", got)
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	if acked != 0 || failed != 0 {
		t.Fatalf("sink acked = %d, failed = %d, want zero of both: the dropped item is never acked or failed", acked, failed)
	}
	counts, rm := batchMetricStatusCounts(t, reader)
	if counts["ack_outcome_unknown"] != 1 || len(counts) != 1 {
		t.Fatalf("reducer execution statuses = %v, want only ack_outcome_unknown:1", counts)
	}
	if got := strings.Count(logs.String(), `"intent_id":"`+intents[0].IntentID+`"`); got != 1 {
		t.Fatalf("record lines for %s = %d, want exactly 1: the received item is recorded exactly once", intents[0].IntentID, got)
	}
	// Either worker may win the single handoff rendezvous, but the record
	// must carry a real worker ID: workerID -1 would mean the claimer-owned
	// mid-handoff/before-enqueue path recorded it instead of Site B.
	if strings.Contains(logs.String(), `"worker_id":"-1"`) || !strings.Contains(logs.String(), `"status":"ack_outcome_unknown"`) {
		t.Fatalf("post-receive drop log misattributes the worker or omits the unknown outcome: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"worker_id":"0"`) && !strings.Contains(logs.String(), `"worker_id":"1"`) {
		t.Fatalf("post-receive drop log carries no receiving worker id: %s", logs.String())
	}
	if strings.Contains(logs.String(), `"msg":"reducer execution succeeded"`) {
		t.Fatalf("canceled batch logs falsely report success: %s", logs.String())
	}
	for _, name := range []string{"eshu_dp_reducer_run_duration_seconds", "eshu_dp_reducer_queue_wait_seconds"} {
		if got := reducerHistogramCount(t, rm, name, map[string]string{"domain": string(DomainCodeCallMaterialization)}); got != 1 {
			t.Fatalf("%s count = %d, want 1", name, got)
		}
	}
}
