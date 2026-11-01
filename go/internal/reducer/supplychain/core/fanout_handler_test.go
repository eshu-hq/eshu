// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestChangedWriteReopensEveryHolderButTheOwnScope proves the happy path: a
// pass whose write changed a CVE asks for that CVE's holders, drops its own
// scope even when the read returns it, reopens the rest in sorted order, and
// stamps each reopen with where it came from.
func TestChangedWriteReopensEveryHolderButTheOwnScope(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"vuln-intel://osv/npm", "vuln-intel://nvd/cve", fanOutOperatorScope, "vuln-intel://kev/catalog"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := []string{fanOutOperatorScope, "vuln-intel://kev/catalog", "vuln-intel://osv/npm"}
	if !reflect.DeepEqual(port.reopened, want) {
		t.Fatalf("reopened = %v, want %v (sorted, own scope dropped, operator scope included)", port.reopened, want)
	}
	if got := port.findOwn; len(got) != 1 || got[0] != "vuln-intel://nvd/cve" {
		t.Fatalf("FindHolderScopes own scope = %v, want the committing scope", got)
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001"}}) {
		t.Fatalf("FindHolderScopes cve ids = %v, want the changed CVE only", port.findCalls)
	}
	for _, origin := range port.origins {
		wantOrigin := FanOutOrigin{SourceScopeID: "vuln-intel://nvd/cve", SourceGenerationID: "generation-fanout", SourceIntentID: "intent-fanout"}
		if origin != wantOrigin {
			t.Fatalf("origin = %+v, want %+v", origin, wantOrigin)
		}
	}
	if got := result.SubSignals["fanout_holders"]; got != 3 {
		t.Fatalf("SubSignals[fanout_holders] = %v, want 3", got)
	}
	if got := result.SubSignals["fanout_reopened"]; got != 3 {
		t.Fatalf("SubSignals[fanout_reopened] = %v, want 3", got)
	}
}

// TestUnchangedRederivationFansOutNothing proves an idempotent pass costs the
// queue nothing: no change, a first attempt, no list to drain, so no holder
// read and no reopen.
func TestUnchangedRederivationFansOutNothing(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder"}}
	writer := &changeWriter{}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.findCalls) != 0 || len(port.reopened) != 0 || len(port.stateCalls) != 0 {
		t.Fatalf("find=%d reopened=%d state=%d, want no fan-out for an unchanged first attempt", len(port.findCalls), len(port.reopened), len(port.stateCalls))
	}
}

// TestRetryFansOutEvenWhenTheRewriteChangedNothing closes the at-least-once
// hole: a worker that died after the writer commit and before the fan-out
// leaves a rerun that finds identical rows, so only the owed marker the writer
// set inside its transaction says the fan-out may be owed. The rerun asks for the
// recovery CVE set and counts the pass as crash_recovery.
func TestRetryFansOutEvenWhenTheRewriteChangedNothing(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{PayloadKeyFanOutOwed: true}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{RecoveryCVEIDs: []string{"CVE-2026-9001", "CVE-2026-9002"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(2, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !writer.writes[0].Recovery {
		t.Fatal("write.Recovery = false, want true when the owed marker is present")
	}
	if _, has := port.payload[PayloadKeyFanOutOwed]; has {
		t.Fatal("the owed marker survived a fan-out that reopened every holder")
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the holder reached by the recovery set", port.reopened)
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001", "CVE-2026-9002"}}) {
		t.Fatalf("FindHolderScopes cve ids = %v, want the recovery set", port.findCalls)
	}
	counts := fanOutCount(t, reader)
	if counts["crash_recovery"] != 1 || counts["reopened"] != 1 {
		t.Fatalf("fan-out counter = %v, want crash_recovery=1 reopened=1", counts)
	}
}

// TestStampedIntentDoesNotFanOutAgainAndConsumesItsStamp is the one-hop rule.
// A pass that was itself reopened by a fan-out never fans out, and it consumes
// the stamp so the next reopen by anything else is judged fresh.
func TestStampedIntentDoesNotFanOutAgainAndConsumesItsStamp(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	payload := map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope": "src"}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.findCalls) != 0 || len(port.reopened) != 0 {
		t.Fatalf("find=%d reopened=%d, want none for a stamped pass", len(port.findCalls), len(port.reopened))
	}
	if !reflect.DeepEqual(port.stateCalls, []FanOutState{{ConsumeOrigin: true, ExpectedOrigin: map[string]any{"scope": "src"}}}) {
		t.Fatalf("state calls = %+v, want one consume-the-stamp update", port.stateCalls)
	}
}

// TestStampedItemReopenedAgainWithoutAStampFansOut is gap 5: once the stamp is
// consumed, a reopen by a path that does not stamp is a normal pass.
func TestStampedItemReopenedAgainWithoutAStampFansOut(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope": "src"}}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("stamped pass reopened %v, want none", port.reopened)
	}
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the holder once the stamp is gone", port.reopened)
	}
}

// TestHolderReadErrorFailsThePassSoTheQueueRetriesIt proves the read is not
// swallowed: the pass errors, nothing is reopened, and the retry (attempt 2)
// fans out.
func TestHolderReadErrorFailsThePassSoTheQueueRetriesIt(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{holders: []string{"holder-a"}, findErr: errors.New("holder read timed out"), payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}, RecoveryCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(port, writer, inst)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err == nil {
		t.Fatal("Handle() error = nil, want the holder read error")
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v, want none after a failed read", port.reopened)
	}
	if got := fanOutCount(t, reader)["read_error"]; got != 1 {
		t.Fatalf("read_error outcome = %d, want 1", got)
	}
	if owed, _ := port.payload[PayloadKeyFanOutOwed].(bool); !owed {
		t.Fatalf("payload = %v, want the owed marker the commit left behind", port.payload)
	}
	port.findErr = nil
	if _, err := handler.Handle(context.Background(), fanOutIntent(2, port.payload)); err != nil {
		t.Fatalf("retry Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("retry reopened = %v, want the holder", port.reopened)
	}
}

// TestReopenErrorFailsThePass proves a failed reopen is a retry too, not a
// logged skip: a holder left unreopened is a silently stale copy.
func TestReopenErrorFailsThePass(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a", "holder-b"}, reopenErr: map[string]error{"holder-b": errors.New("serialization failure")}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err == nil {
		t.Fatal("Handle() error = nil, want the reopen error")
	}
}

// TestOverflowDrainsAcrossClaimsAndSurvivesACrash is the bounded-claim proof:
// 3*cap+1 holders are reached across the claims the queue would give the item,
// with one injected crash (a lost lease on the state update after the second
// chunk) that makes the claim run again from the stored list.
func TestOverflowDrainsAcrossClaimsAndSurvivesACrash(t *testing.T) {
	t.Parallel()

	holders := make([]string, 0, 3*fanOutHolderCap+1)
	for i := 0; i < 3*fanOutHolderCap+1; i++ {
		holders = append(holders, fmt.Sprintf("holder-%05d", i))
	}
	port := &stubFanOut{holders: holders, payload: map[string]any{}, stateFailures: []int{2}}
	writer := &changeWriter{}
	handler := fanOutHandler(port, writer, nil)

	attempt := 1
	claims := 0
	for ; claims < 12; claims++ {
		writer.result = SupplyChainImpactWriteResult{}
		if claims == 0 {
			writer.result.ChangedCVEIDs = []string{"CVE-2026-9001"}
		}
		if attempt > 1 {
			writer.result.RecoveryCVEIDs = []string{"CVE-2026-9001"}
		}
		_ = attempt
		payload := map[string]any{}
		for k, v := range port.payload {
			payload[k] = v
		}
		before := len(port.reopened)
		_, err := handler.Handle(context.Background(), fanOutIntent(attempt, payload))
		if got := len(port.reopened) - before; got > fanOutHolderCap {
			t.Fatalf("claim %d reopened %d holders, want at most the cap %d", claims+1, got, fanOutHolderCap)
		}
		if claims == 0 && len(port.reopened) != fanOutHolderCap {
			t.Fatalf("the first claim reopened %d holders, want exactly the cap %d", len(port.reopened), fanOutHolderCap)
		}
		if err != nil {
			attempt++
			continue
		}
		attempt = 1
		if _, more := port.payload[PayloadKeyFanOutRemaining]; !more {
			claims++
			break
		}
	}
	seen := map[string]bool{}
	for _, id := range port.reopened {
		seen[id] = true
	}
	if len(seen) != len(holders) {
		t.Fatalf("reached %d of %d holders after %d claims", len(seen), len(holders), claims)
	}
	if _, more := port.payload[PayloadKeyFanOutRemaining]; more {
		t.Fatalf("a continuation list is still stored after %d claims: %v", claims, port.payload)
	}
	if claims > 6 {
		t.Fatalf("took %d claims for four chunks plus one crash, want at most 6", claims)
	}
	sorted := append([]string(nil), port.reopened...)
	if !sort.StringsAreSorted(sorted[:fanOutHolderCap]) {
		t.Fatal("first chunk is not in sorted order")
	}
}

// TestFanOutReopenedOperatorPassDoesNotFanOutAgain pins one hop for the
// operator scope: a suppression pass reopened by a source pass that also changed
// its own copies must not reopen the sources back.
func TestFanOutReopenedOperatorPassDoesNotFanOutAgain(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"vuln-intel://osv/npm"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(port, writer, nil)
	intent := fanOutIntent(1, map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope": "vuln-intel://osv/npm"}})
	intent.ScopeID = fanOutOperatorScope
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.reopened) != 0 {
		t.Fatalf("reopened = %v, want none from a fan-out-reopened operator pass", port.reopened)
	}
}

// TestSuppressionMutationFanOutIsBoundedByChangedCVEs proves the operator pass
// fans out only for the CVEs whose copies it changed: a mutation touching one
// suppression asks for one CVE, not for every CVE the scope holds.
func TestSuppressionMutationFanOutIsBoundedByChangedCVEs(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"vuln-intel://osv/npm"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	intent := fanOutIntent(1, nil)
	intent.ScopeID = fanOutOperatorScope
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001"}}) {
		t.Fatalf("holder read cve ids = %v, want exactly the changed CVE", port.findCalls)
	}
}

// TestHolderReadIsChunkedByCVE proves a wide change set is read in bounded
// statements and the holders are de-duplicated across chunks.
func TestHolderReadIsChunkedByCVE(t *testing.T) {
	t.Parallel()

	ids := make([]string, 0, 2*fanOutCVEChunk+1)
	for i := 0; i < 2*fanOutCVEChunk+1; i++ {
		ids = append(ids, fmt.Sprintf("CVE-2026-%05d", i))
	}
	port := &stubFanOut{holders: []string{"holder-a", "holder-b"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.findCalls) != 3 {
		t.Fatalf("holder read statements = %d, want 3 chunks", len(port.findCalls))
	}
	for i, call := range port.findCalls {
		if len(call) > fanOutCVEChunk {
			t.Fatalf("chunk %d carries %d ids, want at most %d", i, len(call), fanOutCVEChunk)
		}
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a", "holder-b"}) {
		t.Fatalf("reopened = %v, want each holder once", port.reopened)
	}
}

// TestFanOutOutcomesAreCounted proves every per-holder outcome lands on the
// counter, so an operator can see reopened versus flagged versus no-op.
func TestFanOutOutcomesAreCounted(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	port := &stubFanOut{
		holders: []string{"a", "b", "c", "d"},
		outcomes: map[string]FanOutOutcome{
			"b": FanOutReplayFlagged, "c": FanOutPendingNoop, "d": FanOutEnqueued,
		},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := map[string]int64{"reopened": 1, "replay_flagged": 1, "pending_noop": 1, "enqueued": 1}
	if got := fanOutCount(t, reader); !reflect.DeepEqual(got, want) {
		t.Fatalf("fan-out counter = %v, want %v", got, want)
	}
}

// TestNilFanOutPortKeepsThePassWorking keeps the optional seam optional: a
// handler without the port behaves as it did before 1.5b.
func TestNilFanOutPortKeepsThePassWorking(t *testing.T) {
	t.Parallel()

	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(nil, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}
