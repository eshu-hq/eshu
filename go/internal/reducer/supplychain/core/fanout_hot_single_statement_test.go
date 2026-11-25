// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// TestHotSingletonHolderReadRunsAsSingleStatement is the #11430 routing
// contract: a hot singleton id (mass over the split threshold) runs the
// already-shipped single holder statement (FindSupplyChainImpactHoldersQuery),
// not one statement per fact_id key range. The union it reopens is the
// shipped read's answer, cold ids keep their packed statement, and no
// key-range statement runs.
func TestHotSingletonHolderReadRunsAsSingleStatement(t *testing.T) {
	t.Parallel()

	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"holder-a"},
			masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
		},
	}
	ids := []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2"}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.rangeCalls) != 0 {
		t.Fatalf("ranged statements = %v, want none: the hot singleton reads as one shipped statement", port.rangeCalls)
	}
	got := append([][]string(nil), port.findCalls...)
	for _, call := range got {
		sort.Strings(call)
	}
	sort.Slice(got, func(i, j int) bool { return len(got[i]) < len(got[j]) })
	want := [][]string{{"CVE-2026-hot"}, {"CVE-2026-c1", "CVE-2026-c2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("holder statements = %v, want the hot singleton alone plus one packed cold statement %v", port.findCalls, want)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the shipped read's union [holder-a]", port.reopened)
	}
	if got := result.SubSignals["fanout_holder_statements"]; got != 2 {
		t.Fatalf("fanout_holder_statements = %v, want 2 (hot singleton plus cold pack)", got)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != 0 {
		t.Fatalf("fanout_holder_key_range_statements = %v, want 0", got)
	}
	if got := result.SubSignals["fanout_key_ranged_ids"]; got != 0 {
		t.Fatalf("fanout_key_ranged_ids = %v, want 0", got)
	}
}
