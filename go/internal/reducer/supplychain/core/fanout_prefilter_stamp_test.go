// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// The #10972 stamp ruling, no database: the handler hands the prefilter the
// stamp set it writes, an uncovered queued holder reaches the reopen and takes
// a cap slot, and a widen is counted as stamp_widened, never as a no-op.

// TestPrefilterReadsTheStampSetTheFanOutWrites: bounded, empty (a drain with
// no change of its own), and sentinel (over the cap) stampOut each reach the
// port unchanged, and the reopen stamps the same set.
func TestPrefilterReadsTheStampSetTheFanOutWrites(t *testing.T) {
	t.Parallel()

	overCap := make([]string, 0, FanOutOriginCVEIDCap+1)
	for i := 0; i <= FanOutOriginCVEIDCap; i++ {
		overCap = append(overCap, fmt.Sprintf("CVE-2026-%05d", i))
	}
	cases := []struct {
		name    string
		changed []string
		payload map[string]any
		want    []string
	}{
		{"bounded", []string{"CVE-2026-0002"}, map[string]any{}, []string{"CVE-2026-0002"}},
		{"empty drain", nil, map[string]any{PayloadKeyFanOutOwed: true, PayloadKeyFanOutRemaining: []any{"holder-a"}}, []string{}},
		{"sentinel", overCap, map[string]any{}, []string{FanOutStampSentinel}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			port := &stubFanOut{holders: []string{"holder-a"}, payload: tc.payload}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: tc.changed}}
			if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, tc.payload)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(port.queuedStampOuts) != 1 || !slices.Equal(port.queuedStampOuts[0], tc.want) {
				t.Fatalf("prefilter stampOut = %v, want one read with %v", port.queuedStampOuts, tc.want)
			}
			if len(port.origins) != 1 || !slices.Equal(port.origins[0].CVEIDs, tc.want) {
				t.Fatalf("reopen origins = %+v, want the stamp %v", port.origins, tc.want)
			}
		})
	}
}

// TestAnUncoveredQueuedHolderReachesTheReopenTakesACapSlotAndCountsAsWidened:
// 300 holders, none covered, every reopen widens. 256 are reopened, 44 ride the
// continuation list, and the counter, sub-signal, and no-op count say widened.
func TestAnUncoveredQueuedHolderReachesTheReopenTakesACapSlotAndCountsAsWidened(t *testing.T) {
	t.Parallel()

	holders := prefilterHolders(300)
	outcomes := map[string]FanOutOutcome{}
	for _, id := range holders {
		outcomes[id] = FanOutStampWidened
	}
	inst, reader := prefilterInstruments(t)
	port := &stubFanOut{holders: holders, outcomes: outcomes, payload: map[string]any{}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-0002"}}}
	result, err := fanOutHandler(port, writer, inst).Handle(context.Background(), fanOutIntent(1, port.payload))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, holders[:fanOutHolderCap]) {
		t.Fatalf("reopened %d holders, want the first %d in order", len(port.reopened), fanOutHolderCap)
	}
	counts := fanOutCount(t, reader)
	if counts["stamp_widened"] != fanOutHolderCap || counts["pending_noop"] != 0 || counts["deferred"] != 300-fanOutHolderCap {
		t.Fatalf("outcomes = %v, want stamp_widened=%d, no pending_noop, deferred=%d", counts, fanOutHolderCap, 300-fanOutHolderCap)
	}
	if got := result.SubSignals["fanout_stamp_widened"]; got != fanOutHolderCap {
		t.Fatalf("fanout_stamp_widened sub-signal = %v, want %d", got, fanOutHolderCap)
	}
}

// mergingFanOut is a port whose reopen keeps one holder's stamp the way the
// Postgres merge does (sorted union), so the end-to-end case can read the
// holder's next intent.
type mergingFanOut struct {
	*stubFanOut
	holderStamp map[string]any
}

func (m *mergingFanOut) ReopenHolderDetailed(ctx context.Context, holder string, origin FanOutOrigin) (FanOutReopen, error) {
	if _, err := m.stubFanOut.ReopenHolderDetailed(ctx, holder, origin); err != nil {
		return FanOutReopen{}, err
	}
	stored := fanOutStampCVEIDs(fanOutIntent(1, map[string]any{PayloadKeyFanOutOrigin: m.holderStamp}))
	merged := append(append([]string{}, stored...), origin.CVEIDs...)
	sort.Strings(merged)
	ids := make([]any, 0, len(merged))
	for _, id := range merged {
		ids = append(ids, id)
	}
	m.holderStamp = map[string]any{"scope_id": origin.SourceScopeID, FanOutStampCVEKey: ids}
	return FanOutReopen{Outcome: FanOutStampWidened}, nil
}

// TestAHolderPendingForAThenFannedOutForBRunsScopedToBoth is the end-to-end
// case: a holder pending with stamp {A} is not covered by a fan-out for {B};
// the fan-out widens it, and the holder's next claim runs scoped to [A, B].
func TestAHolderPendingForAThenFannedOutForBRunsScopedToBoth(t *testing.T) {
	t.Parallel()

	port := &mergingFanOut{
		stubFanOut:  &stubFanOut{holders: []string{"holder-a"}, payload: map[string]any{}},
		holderStamp: map[string]any{"scope_id": "scope:a", FanOutStampCVEKey: []any{"CVE-2026-0001"}},
	}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-0002"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the uncovered holder", port.reopened)
	}
	next := fanOutIntent(1, map[string]any{PayloadKeyFanOutOrigin: port.holderStamp})
	if got := ScopedPassCVEIDs(next); !reflect.DeepEqual(got, []string{"CVE-2026-0001", "CVE-2026-0002"}) {
		t.Fatalf("next pass scope = %v, want [CVE-2026-0001 CVE-2026-0002]", got)
	}
}
