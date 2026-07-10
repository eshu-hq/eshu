// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import "sort"

// denylistKeys names property keys Canonicalize drops before hashing because
// they carry run-local operational state rather than graph truth. Keep this
// list minimal and add an entry only once a real volatile key has been
// observed leaking through properties() — see the package doc for the
// over-normalize/under-normalize tradeoff this list controls. Every entry
// below must document the concrete write path that stamps it and why it is
// not part of graph content.
var denylistKeys = map[string]struct{}{
	// eshu_orphan_observed_at_unix is a wall-clock Unix timestamp the reducer's
	// orphan-sweep pass stamps onto a node each time the sweep revisits it
	// (see PR #4955, "gate orphan-sweep writes on counts to skip 0-row
	// cycles"). It records *when the sweep last looked at this node*, not any
	// fact about the node itself, so two runs of an otherwise-identical graph
	// churn this single value and would register as a false divergence if left
	// unnormalized.
	"eshu_orphan_observed_at_unix": {},
}

// normalizeProps returns a copy of props with denylisted keys removed. It
// never mutates props, and it always returns a non-nil map (even for a nil or
// empty input) so a node/edge with no properties canonicalizes to `{}`
// rather than `null`, keeping that case indistinguishable across Readers that
// return a nil map versus an empty one.
//
// Every key not on denylistKeys passes through unchanged, including
// deterministic content keys like uid, id, source_fact_id, observed_at, and
// generation_id: those are content derived from fact input, not run-local
// volatility, and stripping them would hide a real divergence behind a false
// green. See the package doc for why this list stays deliberately narrow.
func normalizeProps(props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		if _, denied := denylistKeys[k]; denied {
			continue
		}
		out[k] = v
	}
	return out
}

// sortedLabels returns a sorted copy of labels. It never mutates labels, and
// it always returns a non-nil slice (even for nil/empty input) so a node
// with no labels canonicalizes to `[]` rather than `null`.
func sortedLabels(labels []string) []string {
	out := make([]string, len(labels))
	copy(out, labels)
	sort.Strings(out)
	return out
}
