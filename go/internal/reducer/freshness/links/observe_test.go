// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"strings"
	"testing"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestRebaseIsALinkAndAPriorPrunedBreak proves a rebase (arbiter ruling
// arb-7127-3d, C2) reaches operators as both things it is: a committed root
// link (links_total{link_kind=root,outcome=linked}, counted as Linked, so the
// runner keeps draining) and one prior_pruned chain break, with the pruned
// prior on the log line.
func TestRebaseIsALinkAndAPriorPrunedBreak(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{
		"s": {{result: store.LinkResult{
			ScopeID: "s", GenerationID: "g1", Kind: store.LinkKindRoot,
			Break: store.BreakPriorPruned, RebasedFrom: "g0", Keys: 7,
		}}},
	}}
	r, reader, logs := observedRunner(t, linker, "s")
	result, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Linked != 1 || result.Breaks != 0 {
		t.Fatalf("cycle = %+v, want the rebase counted as one link and no break-only outcome", result)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_links_total", "outcome"); got["linked"] != 1 || got["break"] != 0 {
		t.Fatalf("links_total by outcome = %v, want linked 1", got)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_chain_breaks_total", "reason"); got[string(store.BreakPriorPruned)] != 1 {
		t.Fatalf("chain_breaks_total by reason = %v, want prior_pruned 1", got)
	}
	if !strings.Contains(logs.String(), `"rebased_from_generation_id":"g0"`) {
		t.Fatalf("link log line does not name the pruned prior:\n%s", logs.String())
	}
}
