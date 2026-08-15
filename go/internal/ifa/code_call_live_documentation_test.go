// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodeCallLiveProofDocumentationMatchesWiring keeps the human-facing
// inventory aligned with the now-live code_calls proof. These exact seams
// drifted together during #5991: the catalog moved files, both live gates were
// wired, and allProjectionDomains grew while prose still described the old
// state.
func TestCodeCallLiveProofDocumentationMatchesWiring(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	checks := []struct {
		path       string
		required   []string
		prohibited []string
	}{
		{
			path: filepath.Join("go", "internal", "ifa", "README.md"),
			required: []string{
				"`codeCallFamilyOdu` (`code_call_family_catalog.go`",
			},
			prohibited: []string{
				"`codeCallFamilyOdu` (`code_call_family_odu.go`",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "code_call_family_odu.go"),
			required: []string{
				"Both live gate scripts drive this cassette",
			},
			prohibited: []string{
				"no gate script drives this cassette",
				"Until then this proves the extractor only",
				"derives its Odù FROM",
				"hand-built twin is a second source of truth",
				"no consumer outside its own test",
				"registered nowhere yet",
				"nothing dispatches to it",
			},
		},
		{
			path: filepath.Join("specs", "ifa-materialized-edge-coverage.v1.yaml"),
			required: []string{
				"The sql_relationships BASELINE, DELTA, and FAULT rows and the code_calls",
				"BASELINE and FAULT rows are proven; neither family carries a waiver.",
			},
			prohibited: []string{
				"confirmed-false fault row is waived",
			},
		},
		{
			path: filepath.Join("go", "cmd", "ifa", "README.md"),
			required: []string{
				"`sql_relationships` and `code_calls`",
				"nine SQL edges and five code-call edges",
				"`ifa-determinism` live gate invokes it in every worker-count cell",
				"`ifa-fault-injection` live gate runs it in the fault-free baseline",
				"both domain-scoped `code_calls` recovery cells",
				"SQL delta-retract cell's collateral check",
				"both `proof_gate` rows for `materialized_edges:sql_relationships`",
			},
			prohibited: []string{
				"`ifa-fault-injection` (baseline) live",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges.go"),
			required: []string{
				"Current guards cover SQL relationships, documentation edges, code calls, and rationale edges.",
			},
			prohibited: []string{
				"for \"sql_relationships\" today",
			},
		},
		{
			path: filepath.Join("go", "internal", "reducer", "materialized_edge_families.go"),
			required: []string{
				"14 reducer-owned shared/edge projection domains",
				"codeowners_ownership_edges, submodule_pin_edges",
				"current 12 not-yet-covered allProjectionDomains families",
			},
			prohibited: []string{
				"12 reducer-owned shared/edge projection domains",
			},
		},
	}

	for _, check := range checks {
		check := check
		t.Run(check.path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, check.path)) // #nosec G304 -- fixed repo-relative test paths.
			if err != nil {
				t.Fatalf("read %s: %v", check.path, err)
			}
			text := string(raw)
			for _, required := range check.required {
				if !strings.Contains(text, required) {
					t.Errorf("%s does not contain current proof wording %q", check.path, required)
				}
			}
			for _, prohibited := range check.prohibited {
				if strings.Contains(text, prohibited) {
					t.Errorf("%s still contains stale proof wording %q", check.path, prohibited)
				}
			}
		})
	}
}
