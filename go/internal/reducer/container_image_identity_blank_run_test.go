// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageCIRunsSkipsBlankRunID is the regression guard for #5234
// (codex review on #4685/PR #5233): a ci.run whose required run_id decodes as a
// present-but-blank string must not be indexed as a run anchor. cicdRunKeyFromParts
// still returns a non-empty key like "github_actions::1" from the provider and
// attempt alone, so without the explicit blank-identity guard the malformed run
// would be indexed and lend its repository anchor to a matching malformed
// ci.artifact — the exact join the pre-typing raw cicdRunKey refused. The blank
// value decodes successfully (decodeAndValidate accepts an explicit empty value),
// so the run is skipped, not quarantined.
func TestContainerImageCIRunsSkipsBlankRunID(t *testing.T) {
	t.Parallel()

	blankRun := facts.Envelope{
		FactID:   "blank-run",
		FactKind: facts.CICDRunFactKind,
		Payload: map[string]any{
			"provider":      "github_actions",
			"run_id":        "", // present but blank — must not index a malformed anchor.
			"run_attempt":   "1",
			"repository_id": "repo-api",
			"commit_sha":    "abc123",
		},
	}

	anchors, quarantined, err := containerImageCIRuns([]facts.Envelope{blankRun})
	if err != nil {
		t.Fatalf("containerImageCIRuns returned an unexpected error: %v", err)
	}
	if len(anchors) != 0 {
		t.Fatalf("a present-but-blank run_id must not be indexed as a run anchor; got %d: %v", len(anchors), anchors)
	}
	if len(quarantined) != 0 {
		t.Fatalf("a present-but-blank run_id decodes successfully and is skipped, not quarantined; got %d quarantined", len(quarantined))
	}
}
