// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type failingGroupEnqueuer struct{ err error }

func (f failingGroupEnqueuer) EnqueueAdvisoryGroupProjections(
	context.Context, reducercontract.Intent, []string,
) error {
	return f.err
}

// TestAdvisoryGroupEnqueueFailureFailsPassRetryable pins the #10411 loss
// guard: a failed durable enqueue after the writer commit fails the pass
// with the retryable fan-out error instead of acking past the touch, and an
// empty touch set never calls the enqueuer.
func TestAdvisoryGroupEnqueueFailureFailsPassRetryable(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("queue unavailable")
	handler := SupplyChainImpactHandler{GroupProjectionEnqueuer: failingGroupEnqueuer{err: sentinel}}
	err := handler.enqueueAdvisoryGroupProjections(
		context.Background(),
		reducercontract.Intent{},
		SupplyChainImpactWriteResult{TouchedGroupIDs: []string{"CVE-2026-0001"}},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped enqueue failure", err)
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatal("enqueue failure is not retryable: the touch would be lost on ack")
	}

	if err := handler.enqueueAdvisoryGroupProjections(
		context.Background(),
		reducercontract.Intent{},
		SupplyChainImpactWriteResult{},
	); err != nil {
		t.Fatalf("empty touch set err = %v, want nil (enqueuer not called)", err)
	}
}

type recordingGroupEnqueuer struct {
	err    error
	groups [][]string
}

func (r *recordingGroupEnqueuer) EnqueueAdvisoryGroupProjections(
	_ context.Context, _ reducercontract.Intent, groupIDs []string,
) error {
	r.groups = append(r.groups, append([]string(nil), groupIDs...))
	return r.err
}

// TestGroupEnqueueFailureKeepsOwedMarkerForRetry is the composed #10411
// regression: commit (marker recorded) -> group enqueue fails -> the pass
// must abort BEFORE the holder fan-out clears the owed marker, so the
// queue retry is an owed rerun whose writer reports the scope's recovery
// groups and the enqueue fires again. Before the ordering fix the
// fan-out cleared the marker first and the retry acked past the touch.
func TestGroupEnqueueFailureKeepsOwedMarkerForRetry(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		ChangedCVEIDs:   []string{"CVE-2026-9001"},
		TouchedGroupIDs: []string{"CVE-2026-9001"},
	}}
	handler := fanOutHandler(port, writer, nil)
	handler.GroupProjectionEnqueuer = &recordingGroupEnqueuer{err: errors.New("queue unavailable")}

	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err == nil {
		t.Fatal("Handle() error = nil, want the enqueue failure to fail the pass")
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; !has {
		t.Fatal("owed marker was cleared before the failed enqueue: the retry would not be an owed rerun and the group touch is lost")
	}
	if len(port.stateCalls) != 0 {
		t.Fatalf("fan-out state updated %d times before the enqueue failure, want 0 (enqueue runs first)", len(port.stateCalls))
	}

	// The retry: the owed payload makes the writer report recovery groups
	// even though the rewrite changes nothing, and the enqueue now succeeds.
	retryPort := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{PayloadKeyFanOutOwed: true}}
	retryWriter := &changeWriter{result: SupplyChainImpactWriteResult{
		RecoveryCVEIDs:  []string{"CVE-2026-9001"},
		TouchedGroupIDs: []string{"CVE-2026-9001"},
	}}
	retryHandler := fanOutHandler(retryPort, retryWriter, nil)
	enq := &recordingGroupEnqueuer{}
	retryHandler.GroupProjectionEnqueuer = enq
	if _, err := retryHandler.Handle(context.Background(), fanOutIntent(2, retryPort.payload)); err != nil {
		t.Fatalf("retry Handle() error = %v", err)
	}
	if len(enq.groups) != 1 || len(enq.groups[0]) != 1 || enq.groups[0][0] != "CVE-2026-9001" {
		t.Fatalf("retry enqueued = %v, want the recovery group [CVE-2026-9001]", enq.groups)
	}
	if _, has := retryPort.payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("owed marker still set after a successful retry: it must clear once the enqueue and fan-out both finish")
	}
}

// TestCVELessGroupTouchClearsItsOwnOwedMarker pins the mirror condition: a
// pass that touches only advisory groups (no changed CVE) records the owed
// marker in its writer transaction, and a successful pass clears it instead
// of leaving a spurious recovery for the next claim.
func TestCVELessGroupTouchClearsItsOwnOwedMarker(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{
		TouchedGroupIDs: []string{"GHSA-cve-less-0001"},
	}}
	handler := fanOutHandler(port, writer, nil)
	handler.GroupProjectionEnqueuer = &recordingGroupEnqueuer{}
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if port.owedRecorded != 1 {
		t.Fatalf("owed marker recorded %d times, want 1 for a group-touching commit", port.owedRecorded)
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("owed marker left set after a CVE-less group-touching pass: the next claim would run a spurious recovery")
	}
}
