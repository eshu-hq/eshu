// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

// TestOverlayDeltaBaseDropsObservabilityRowsUnderTouchedPaths is the #8780
// observability finding: the git collector keys observability facts on the
// generation id, so a delta's rows never replace the base's by stable key.
// They carry relative_path, so they must be file-attributable: a changed
// file's base rows drop beside the delta's fresh ones, a deleted file's rows
// drop, and an untouched file's base rows survive.
func TestOverlayDeltaBaseDropsObservabilityRowsUnderTouchedPaths(t *testing.T) {
	t.Parallel()
	for _, kind := range deltaUnionObservabilityKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			row := func(rel string) map[string]any { return map[string]any{"relative_path": rel} }
			base := []Envelope{
				deltaUnionBaseEnvelope("repository", "repository:repo-1", "base-repo", deltaUnionRepoPayload(false, nil, nil)),
				deltaUnionBaseEnvelope(kind, kind+":changed.yaml:gen-base-1", "base-changed", row("changed.yaml")),
				deltaUnionBaseEnvelope(kind, kind+":gone.yaml:gen-base-1", "base-gone", row("gone.yaml")),
				deltaUnionBaseEnvelope(kind, kind+":kept.yaml:gen-base-1", "base-kept", row("kept.yaml")),
			}
			delta := []Envelope{
				deltaUnionTestEnvelope("repository", "repository:repo-1", "delta-repo",
					deltaUnionRepoPayload(true, []string{"changed.yaml"}, []string{"gone.yaml"})),
				deltaUnionTestEnvelope(kind, kind+":changed.yaml:gen-delta-1", "delta-changed", row("changed.yaml")),
			}
			deltaUnionEqualKeys(t, OverlayDeltaBase(base, delta, ParseDeltaScope(delta)), []string{
				kind + ":" + kind + ":changed.yaml:gen-delta-1",
				kind + ":" + kind + ":kept.yaml:gen-base-1",
				"repository:repository:repo-1",
			})
		})
	}
}
