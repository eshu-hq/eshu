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

// TestOwedMarkerIsRecordedOnlyWhenTheCommitChangedSomething pins what the
// writer is asked to record: a pass that changed rows leaves the marker inside
// its transaction, a pass that changed nothing leaves none.
func TestOwedMarkerIsRecordedOnlyWhenTheCommitChangedSomething(t *testing.T) {
	t.Parallel()

	changed := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	if _, err := fanOutHandler(changed, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}, nil).
		Handle(context.Background(), fanOutIntent(1, changed.payload)); err != nil {
		t.Fatal(err)
	}
	if changed.owedRecorded != 1 {
		t.Fatalf("owed marker recorded %d times, want 1 for a changed commit", changed.owedRecorded)
	}
	if _, has := changed.payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("the marker is still set after the fan-out finished: it must be cleared with the last holder")
	}
	if !reflect.DeepEqual(changed.stateCalls, []FanOutState{{ClearOwed: true}}) {
		t.Fatalf("state calls = %+v, want one marker clear that leaves the list alone", changed.stateCalls)
	}

	unchanged := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	if _, err := fanOutHandler(unchanged, &changeWriter{}, nil).Handle(context.Background(), fanOutIntent(1, unchanged.payload)); err != nil {
		t.Fatal(err)
	}
	if unchanged.owedRecorded != 0 || len(unchanged.stateCalls) != 0 {
		t.Fatalf("owed=%d state=%d, want nothing for a commit that changed nothing", unchanged.owedRecorded, len(unchanged.stateCalls))
	}
}

// TestOneHopPassRecordsNoMarker keeps the one-hop re-derivation from owing a
// fan-out it will not run.
func TestOneHopPassRecordsNoMarker(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{payload: fanOutStampPayload()}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatal(err)
	}
	if writer.writes[0].Owed != nil || port.owedRecorded != 0 {
		t.Fatal("the one-hop pass asked the writer to record an owed marker")
	}
}

// TestFailureBeforeTheCommitLeavesNoMarkerAndTheRetryFansOutOnlyItsOwnChanges is
// the P2-2 fix: a retry is not evidence that an attempt committed. When the first
// attempt failed before its writer committed, the retry asks for no recovery set
// and fans out only what its own write changed.
func TestFailureBeforeTheCommitLeavesNoMarkerAndTheRetryFansOutOnlyItsOwnChanges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}}
	writer := &changeWriter{err: errors.New("evidence store unavailable")}
	handler := fanOutHandler(port, writer, inst)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err == nil {
		t.Fatal("Handle() error = nil, want the pre-commit failure")
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("a failed write left an owed marker")
	}
	writer.err = nil
	writer.result = SupplyChainImpactWriteResult{
		ChangedCVEIDs:  []string{"CVE-2026-9001"},
		RecoveryCVEIDs: []string{"CVE-2026-9001", "CVE-2026-9002", "CVE-2026-9003"},
	}
	if _, err := handler.Handle(context.Background(), fanOutIntent(2, port.payload)); err != nil {
		t.Fatal(err)
	}
	if writer.writes[1].Recovery {
		t.Fatal("the retry asked for the recovery set although no attempt committed")
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001"}}) {
		t.Fatalf("holder read ids = %v, want only the retry's own change", port.findCalls)
	}
	if got := fanOutCount(t, reader)["crash_recovery"]; got != 0 {
		t.Fatalf("crash_recovery = %d, want 0", got)
	}
}

// TestMarkerAndStampTogetherMeanTheMarkerWins: an item that committed changes
// and then received a stamp is a genuine pass owing a fan-out, not a one-hop
// re-derivation, whatever its attempt count.
func TestMarkerAndStampTogetherMeanTheMarkerWins(t *testing.T) {
	t.Parallel()

	payload := fanOutStampPayload()
	payload[PayloadKeyFanOutOwed] = true
	port := &stubFanOut{holders: []string{"holder-a"}, payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{RecoveryCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the recovery fan-out to run", port.reopened)
	}
	if !writer.writes[0].Recovery {
		t.Fatal("write.Recovery = false: the marker must win over the stamp")
	}
	if _, has := payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("the marker survived a completed fan-out")
	}
	if _, has := payload[PayloadKeyFanOutOrigin]; has {
		t.Fatal("the stamp survived the pass that consumed it")
	}
}

// TestMarkerSurvivesAContinuationAndTheLastDrainClearsIt: while holders are still
// owed the marker stays, so a retry between claims still knows; the claim that
// reopens the last holder clears it.
func TestMarkerSurvivesAContinuationAndTheLastDrainClearsIt(t *testing.T) {
	t.Parallel()

	holders := make([]string, 0, fanOutHolderCap+1)
	for i := 0; i < fanOutHolderCap+1; i++ {
		holders = append(holders, fmt.Sprintf("holder-%05d", i))
	}
	port := &stubFanOut{holders: holders, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatal(err)
	}
	if owed, _ := port.payload[PayloadKeyFanOutOwed].(bool); !owed {
		t.Fatalf("payload = %v, want the marker kept while a holder is still owed", port.payload)
	}
	if _, has := port.payload[PayloadKeyFanOutRemaining]; !has {
		t.Fatal("no continuation list after an overflowing claim")
	}
	writer.result = SupplyChainImpactWriteResult{}
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatal(err)
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatalf("payload = %v, want the marker cleared by the last drain", port.payload)
	}
}

// TestInFlightScopesAreReopenedWithTheHolders is race 3 of the review: a pass in
// flight may have loaded evidence before the source's facts were visible. It is
// reopened with the row holders, once, and the asking scope never.
func TestInFlightScopesAreReopenedWithTheHolders(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{
		holders:  []string{"holder-b", "holder-a"},
		inFlight: []string{"holder-b", "in-flight-c", "vuln-intel://nvd/cve"},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"holder-a", "holder-b", "in-flight-c"}; !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v (union, deduplicated, own scope dropped)", port.reopened, want)
	}
	if port.inFlightCalls != 1 {
		t.Fatalf("in-flight reads = %d, want one per fan-out", port.inFlightCalls)
	}
}

// TestContinuationDrainMakesNoInFlightRead: a drain claim reopens only the stored
// list; the in-flight set belongs to the claim that wrote the change.
func TestContinuationDrainMakesNoInFlightRead(t *testing.T) {
	t.Parallel()

	payload := map[string]any{PayloadKeyFanOutRemaining: []any{"holder-a"}}
	port := &stubFanOut{inFlight: []string{"in-flight-c"}, payload: payload}
	if _, err := fanOutHandler(port, &changeWriter{}, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatal(err)
	}
	if port.inFlightCalls != 0 || !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("in-flight reads=%d reopened=%v, want only the stored list", port.inFlightCalls, port.reopened)
	}
}

// TestInFlightReadFailureFailsThePassWithTheReadOutcomes: the in-flight read has
// the same retry semantics as the holder read.
func TestInFlightReadFailureFailsThePassWithTheReadOutcomes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err  error
		want string
	}{"timeout": {ErrFanOutReadTimeout, "read_timeout"}, "error": {errors.New("boom"), "read_error"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatal(err)
			}
			port := &stubFanOut{holders: []string{"a"}, inFlightErr: tc.err}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			_, err = fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil))
			var retryable interface{ Retryable() bool }
			if !errors.As(err, &retryable) || !retryable.Retryable() {
				t.Fatalf("error %v is not retryable", err)
			}
			if got := fanOutCount(t, reader)[tc.want]; got != 1 {
				t.Fatalf("outcome %q = %d, want 1 (%v)", tc.want, got, fanOutCount(t, reader))
			}
			if len(port.reopened) != 0 {
				t.Fatalf("reopened = %v, want none after a failed read", port.reopened)
			}
		})
	}
}

// TestInFlightReadRunsBeforeTheRowRead pins the order the race proof depends on:
// a holder that acknowledges between the two reads has committed its rows by the
// time the second read runs only if the in-flight read came first.
func TestInFlightReadRunsBeforeTheRowRead(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"in-flight", "rows"}; !reflect.DeepEqual(port.reads, want) {
		t.Fatalf("read order = %v, want %v", port.reads, want)
	}
}

// TestAnOwedDrainDoesNotAskForTheRecoverySet: with a continuation list the claim
// drains the list; the writer is not asked for the recovery CVE set.
func TestAnOwedDrainDoesNotAskForTheRecoverySet(t *testing.T) {
	t.Parallel()

	payload := map[string]any{PayloadKeyFanOutOwed: true, PayloadKeyFanOutRemaining: []any{"holder-a"}}
	port := &stubFanOut{payload: payload}
	writer := &changeWriter{}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatal(err)
	}
	if writer.writes[0].Recovery {
		t.Fatal("write.Recovery = true on an owed drain: the list carries the debt")
	}
}
