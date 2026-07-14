// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageCIRunsSkipsBlankJoinIdentity is the regression guard for
// #5234 (codex review on #4685/PR #5233): a ci.run whose required join identity
// (provider or run_id) decodes as a present-but-blank string must not be indexed
// as a run anchor. cicdRunKeyFromParts still returns a non-empty key like
// "github_actions::1" from whichever part is present, so without the explicit
// blank-identity guard the malformed run would be indexed and lend its repository
// anchor to a matching malformed ci.artifact — the join the pre-typing raw
// cicdRunKey refused. The blank value decodes successfully (decodeAndValidate
// accepts an explicit empty value), so the run is skipped, not quarantined.
//
// Both branches of the guard (blank provider, blank run_id) plus a whitespace
// value are exercised so neither Boolean arm can regress unnoticed.
func TestContainerImageCIRunsSkipsBlankJoinIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "blank run_id",
			payload: map[string]any{
				"provider": "github_actions", "run_id": "", "run_attempt": "1",
				"repository_id": "repo-api", "commit_sha": "abc123",
			},
		},
		{
			name: "blank provider",
			payload: map[string]any{
				"provider": "", "run_id": "42", "run_attempt": "1",
				"repository_id": "repo-api", "commit_sha": "abc123",
			},
		},
		{
			name: "whitespace run_id",
			payload: map[string]any{
				"provider": "github_actions", "run_id": "   ", "run_attempt": "1",
				"repository_id": "repo-api", "commit_sha": "abc123",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := facts.Envelope{
				FactID:   "blank-" + tc.name,
				FactKind: facts.CICDRunFactKind,
				Payload:  tc.payload,
			}

			anchors, quarantined, err := containerImageCIRuns([]facts.Envelope{env})
			if err != nil {
				t.Fatalf("containerImageCIRuns returned an unexpected error: %v", err)
			}
			if len(anchors) != 0 {
				t.Fatalf("a blank join identity must not be indexed as a run anchor; got %d: %v", len(anchors), anchors)
			}
			if len(quarantined) != 0 {
				t.Fatalf("a present-but-blank required field decodes successfully and is skipped, not quarantined; got %d", len(quarantined))
			}
		})
	}
}
