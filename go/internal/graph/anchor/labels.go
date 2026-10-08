// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import "github.com/eshu-hq/eshu/go/internal/graph"

// UIDLabels returns the labels the Neo4j entity-context anchor seeks by
// `uid = id` (uid-constrained labels), as a fresh sorted slice.
func UIDLabels() []string { return graph.UIDUniquenessConstrainedLabels() }

// IDLabels returns the labels the anchor seeks by `id` alone (id-constrained
// labels), as a fresh sorted slice.
func IDLabels() []string { return graph.IDUniquenessConstrainedLabels() }

// Labels returns the union of UIDLabels and IDLabels as a set: every label
// the Neo4j entity-context anchor can reach a node through.
func Labels() map[string]bool {
	out := make(map[string]bool)
	for _, label := range UIDLabels() {
		out[label] = true
	}
	for _, label := range IDLabels() {
		out[label] = true
	}
	return out
}
