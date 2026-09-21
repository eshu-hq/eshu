// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// TestSplitValueFlowRefreshAckIntentsFailsOpenOnMissingResult is the #6884 P2
// regression: a refresh-producer intent with no paired entry in results must
// emit (fail-open), not degrade to the zero Result and suppress the refresh.
// A silent missed refresh is the worse outcome per the #6785 design; the
// production call site always pairs results, so a missing entry is a future
// caller bug that must surface as a bounded spurious refresh, never as a
// starved fixpoint.
func TestSplitValueFlowRefreshAckIntentsFailsOpenOnMissingResult(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	intent := func(id string, domain reducer.Domain) reducer.Intent {
		return reducer.Intent{IntentID: id, Domain: domain, ClaimedAt: &now}
	}
	intents := []reducer.Intent{
		intent("w-paired-emit", reducer.DomainWorkloadMaterialization),
		intent("w-paired-hold", reducer.DomainWorkloadMaterialization),
		intent("w-missing-result", reducer.DomainWorkloadMaterialization),
		intent("w-unrelated", reducer.DomainCICDRunCorrelation),
	}
	results := map[string]reducer.Result{
		"w-paired-emit": {CanonicalWrites: 4, SubSignals: map[string]float64{affected.RefreshAffectedReposSignal: 1}},
		"w-paired-hold": {CanonicalWrites: 4, SubSignals: map[string]float64{affected.RefreshAffectedReposSignal: 0}},
	}

	kept, groups := splitValueFlowRefreshAckIntents(intents, results)

	if len(kept) != 1 || kept[0].IntentID != "w-unrelated" {
		t.Fatalf("kept = %v, want only the non-producer intent", kept)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 workload-materialization group", len(groups))
	}
	emit := map[string]bool{}
	for _, id := range groups[0].emitIDs {
		emit[id] = true
	}
	for _, id := range []string{"w-paired-emit", "w-missing-result"} {
		if !emit[id] {
			t.Errorf("emitIDs = %v, want %s to emit", groups[0].emitIDs, id)
		}
	}
	if emit["w-paired-hold"] {
		t.Errorf("emitIDs = %v, explicit-zero hold item must not emit", groups[0].emitIDs)
	}
	if len(groups[0].ids) != 3 {
		t.Errorf("group ids = %v, every producer item still ACKs", groups[0].ids)
	}
}
