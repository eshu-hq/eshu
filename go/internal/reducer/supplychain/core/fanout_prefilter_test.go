// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// prefilterHolders returns n sorted holder ids.
func prefilterHolders(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("holder-%05d", i))
	}
	return out
}

func prefilterInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// TestAFreshPassWithOnlyQueuedHoldersReopensNoneAndStoresNoContinuation is the
// #10972 PR-2 acceptance: 300 holders, every one already pending or retrying.
// The prefilter drops them all in one set read, so no per-holder statement
// runs, no continuation entry is written, and the owed marker clears because
// nothing is left owed.
func TestAFreshPassWithOnlyQueuedHoldersReopensNoneAndStoresNoContinuation(t *testing.T) {
	t.Parallel()

	holders := prefilterHolders(300)
	queued := map[string]bool{}
	for _, id := range holders {
		queued[id] = true
	}
	inst, reader := prefilterInstruments(t)
	port := &stubFanOut{holders: holders, queued: queued, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("ReopenHolderDetailed calls = %d, want 0 for all-queued holders", len(port.reopened))
	}
	if len(port.queuedCalls) != 1 || !reflect.DeepEqual(port.queuedCalls[0], holders) {
		t.Fatalf("prefilter reads = %d, want exactly one over the whole sorted list", len(port.queuedCalls))
	}
	if _, has := port.payload[PayloadKeyFanOutRemaining]; has {
		t.Fatalf("payload = %v, want no continuation entry", port.payload)
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatalf("payload = %v, want the owed marker cleared: nothing is left owed", port.payload)
	}
	counts := fanOutCount(t, reader)
	if counts["prefiltered_pending"] != 300 || counts["pending_noop"] != 0 || counts["deferred"] != 0 {
		t.Fatalf("outcomes = %v, want prefiltered_pending=300 and no pending_noop or deferred", counts)
	}
}

// TestAContinuationDrainOfQueuedHoldersClearsTheListWithoutAReopen is the
// drain half: a stored list of 300 holders that all queued since it was
// written is cleared by one set read, with no per-holder statement and no
// in-flight read (a drain never asks for one).
func TestAContinuationDrainOfQueuedHoldersClearsTheListWithoutAReopen(t *testing.T) {
	t.Parallel()

	holders := prefilterHolders(300)
	stored := make([]any, 0, len(holders))
	queued := map[string]bool{}
	for _, id := range holders {
		stored = append(stored, id)
		queued[id] = true
	}
	payload := map[string]any{PayloadKeyFanOutOwed: true, PayloadKeyFanOutRemaining: stored}
	port := &stubFanOut{queued: queued, payload: payload}
	if _, err := fanOutHandler(port, &changeWriter{}, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.reopened) != 0 || port.inFlightCalls != 0 || len(port.queuedCalls) != 1 {
		t.Fatalf("reopened=%d inFlight=%d prefilterReads=%d, want 0, 0, 1", len(port.reopened), port.inFlightCalls, len(port.queuedCalls))
	}
	if _, has := port.payload[PayloadKeyFanOutRemaining]; has {
		t.Fatalf("payload = %v, want the continuation list cleared", port.payload)
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatalf("payload = %v, want the owed marker cleared with the last holder", port.payload)
	}
	last := port.stateCalls[len(port.stateCalls)-1]
	if !last.ReplaceRemaining || len(last.Remaining) != 0 {
		t.Fatalf("state = %+v, want ReplaceRemaining with an empty list", last)
	}
}

// TestPrefilterDropsOnlyQueuedHoldersAndTheCapAppliesAfterIt pins order and
// cap: 700 holders, every even one queued. The 350 others stay in sorted order,
// the first 256 are reopened, and the continuation list carries the other 94,
// not a single queued holder.
func TestPrefilterDropsOnlyQueuedHoldersAndTheCapAppliesAfterIt(t *testing.T) {
	t.Parallel()

	holders := prefilterHolders(700)
	queued := map[string]bool{}
	var kept []string
	for i, id := range holders {
		if i%2 == 0 {
			queued[id] = true
			continue
		}
		kept = append(kept, id)
	}
	port := &stubFanOut{holders: holders, queued: queued, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, kept[:fanOutHolderCap]) {
		t.Fatalf("reopened %d holders, want the first %d non-queued in sorted order", len(port.reopened), fanOutHolderCap)
	}
	stored, _ := port.payload[PayloadKeyFanOutRemaining].([]any)
	if len(stored) != len(kept)-fanOutHolderCap {
		t.Fatalf("continuation holds %d holders, want %d", len(stored), len(kept)-fanOutHolderCap)
	}
	for i, id := range stored {
		if id != kept[fanOutHolderCap+i] {
			t.Fatalf("continuation[%d] = %v, want %v", i, id, kept[fanOutHolderCap+i])
		}
	}
}

// TestPrefilterLeavesEveryNonQueuedStateToTheReopenStatement: a holder is
// dropped only when its item is pending or retrying. Claimed, succeeded,
// dead-letter, and no-item holders all reach ReopenHolderDetailed unchanged,
// in sorted order.
func TestPrefilterLeavesEveryNonQueuedStateToTheReopenStatement(t *testing.T) {
	t.Parallel()

	inst, reader := prefilterInstruments(t)
	port := &stubFanOut{
		holders: []string{"f-retrying", "e-no-item", "d-dead-letter", "c-succeeded", "b-claimed", "a-pending"},
		queued:  map[string]bool{"a-pending": true, "f-retrying": true},
		outcomes: map[string]FanOutOutcome{
			"b-claimed":     FanOutReplayFlagged,
			"d-dead-letter": FanOutDeadLetterSkipped,
			"e-no-item":     FanOutEnqueued,
		},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if want := []string{"b-claimed", "c-succeeded", "d-dead-letter", "e-no-item"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v", port.reopened, want)
	}
	counts := fanOutCount(t, reader)
	if counts["prefiltered_pending"] != 2 || counts["reopened"] != 1 || counts["replay_flagged"] != 1 || counts["enqueued"] != 1 {
		t.Fatalf("outcomes = %v", counts)
	}
}

// TestPrefilterReadFailureFailsThePassBeforeAnyReopen: the set read is not
// swallowed. A failed read fails the pass for a queue retry, reopens nothing,
// and the stored list is left as it was (the retry reruns the same list).
func TestPrefilterReadFailureFailsThePassBeforeAnyReopen(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want string
	}{"timeout": {ErrFanOutReadTimeout, "read_timeout"}, "error": {errors.New("boom"), "read_error"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst, reader := prefilterInstruments(t)
			port := &stubFanOut{holders: []string{"holder-a"}, queuedErr: tc.err, payload: map[string]any{}}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			_, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, port.payload))
			var retryable interface{ Retryable() bool }
			if !errors.As(err, &retryable) || !retryable.Retryable() {
				t.Fatalf("error %v is not retryable", err)
			}
			if len(port.reopened) != 0 || len(port.stateCalls) != 0 {
				t.Fatalf("reopened=%v stateCalls=%d, want none after a failed prefilter read", port.reopened, len(port.stateCalls))
			}
			if got := fanOutCount(t, reader)[tc.want]; got != 1 {
				t.Fatalf("outcome %q = %d, want 1 (%v)", tc.want, got, fanOutCount(t, reader))
			}
		})
	}
}

// TestPrefilterIsSkippedWhenThereAreNoHolders: an empty holder set costs no
// read.
func TestPrefilterIsSkippedWhenThereAreNoHolders(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.queuedCalls) != 0 {
		t.Fatalf("prefilter reads = %d, want 0 for an empty holder set", len(port.queuedCalls))
	}
}
