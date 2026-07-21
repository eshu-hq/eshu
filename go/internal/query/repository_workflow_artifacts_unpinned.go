// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/ghactionsref"

// unpinnedActionRefs pairs dependencyRefs.actionRepositories with its
// index-aligned raw ref in dependencyRefs.actionRefs (see
// githubActionsDependencyRefs's doc comment in
// content_relationships_github_actions.go) and returns the raw
// `owner/repo@ref` string for every entry whose ref is not a full-length
// commit SHA, per ghactionsref.Pinned -- the same classifier the
// reducer/graph-projection path uses, so this rollup's unpinned signal agrees
// with ref_pinned on the deployment-evidence artifact surface. Issue #5372.
func unpinnedActionRefs(actionRepositories, actionRefs []string) []string {
	if len(actionRepositories) != len(actionRefs) {
		// Defensive: the two slices are built in lockstep by
		// githubActionsDependencyRefs.collectSteps, so a length mismatch
		// means a caller bypassed that invariant. Degrade to no signal
		// rather than pair mismatched indexes and fabricate a wrong ref.
		return nil
	}
	unpinned := make([]string, 0, len(actionRefs))
	for _, raw := range actionRefs {
		_, _, refValue := ghactionsref.Parse(raw)
		if refValue == "" {
			continue
		}
		if !ghactionsref.Pinned(refValue) {
			// raw is the original `owner/repo[/path]@ref` scalar, not a
			// slug+ref reconstruction, so a subpath (an action defined in a
			// subdirectory of its repository) is preserved exactly as
			// written in the workflow file.
			unpinned = append(unpinned, raw)
		}
	}
	return unpinned
}
