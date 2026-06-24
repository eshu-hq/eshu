// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proofofvalue

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
)

// composeCorpus builds a minimal Compose corpus: one compose file declaring an
// unused service (orphan) and a used service (api). A workflow file references
// only api via `docker compose up api`. No reference reaches orphan.
func composeCorpus() map[string][]iacreachability.File {
	return map[string][]iacreachability.File{
		"app": {
			{
				RepoID:       "app",
				RelativePath: "compose.yaml",
				Content: "services:\n" +
					"  api:\n    image: example/api:latest\n" +
					"  orphan:\n    image: example/orphan:latest\n",
			},
		},
		"ci": {
			{
				RepoID:       "ci",
				RelativePath: ".github/workflows/deploy.yaml",
				Content:      "run: docker compose up api\n",
			},
		},
	}
}

// TestBaselineExcludesComposeServiceOwnDefinitionFile proves the baseline does
// not count a Compose service's own declaration file as a reference to itself.
// Before the definer-mapping fix the baseline found "orphan:" inside the
// service's own compose.yaml and wrongly returned "used"; after the fix the
// compose file is excluded and the unused service is correctly "unused".
func TestBaselineExcludesComposeServiceOwnDefinitionFile(t *testing.T) {
	files := composeCorpus()
	rows := iacreachability.Analyze(files, iacreachability.Options{IncludeAmbiguous: true})
	definers := definingFilesByArtifact(rows, files)

	var orphan, api iacreachability.Row
	for _, row := range rows {
		switch row.ArtifactName {
		case "orphan":
			orphan = row
		case "api":
			api = row
		}
	}
	if orphan.ArtifactName == "" || api.ArtifactName == "" {
		t.Fatalf("analyzer did not produce both compose rows: %+v", rows)
	}

	// Sanity: the analyzer itself classifies orphan as unused and api as used.
	if orphan.Reachability != iacreachability.ReachabilityUnused {
		t.Fatalf("analyzer orphan reachability = %q, want unused", orphan.Reachability)
	}
	if api.Reachability != iacreachability.ReachabilityUsed {
		t.Fatalf("analyzer api reachability = %q, want used", api.Reachability)
	}

	orphanKey := orphan.RepoID + "/" + orphan.ArtifactPath
	if got := BaselineReachability(files, orphan.ArtifactName, definers[orphanKey]); got != "unused" {
		t.Errorf("baseline orphan = %q, want unused (own compose.yaml must be excluded)", got)
	}

	// The fix must not break the genuinely-used service: api is referenced by
	// the workflow command, so the baseline must still find it used.
	apiKey := api.RepoID + "/" + api.ArtifactPath
	if got := BaselineReachability(files, api.ArtifactName, definers[apiKey]); got != "used" {
		t.Errorf("baseline api = %q, want used (workflow references it)", got)
	}
}
