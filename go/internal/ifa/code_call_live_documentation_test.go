// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializedEdgeLiveProofDocumentationMatchesWiring keeps the
// human-facing inventory aligned with the live code_calls and rationale_edges
// proofs. #5991 and #5998 both changed the live matrices, manifest rows, and
// package docs; this source lock keeps those claims together.
func TestMaterializedEdgeLiveProofDocumentationMatchesWiring(t *testing.T) {
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
				"`materialized_edges:documentation_edges`",
				"Both live matrices drive the documentation cassette and\n  exact-assert its three DOCUMENTS edges",
				"Both live matrices drive the rationale cassette\n  and exact-assert its full EXPLAINS records",
			},
			prohibited: []string{
				"`codeCallFamilyOdu` (`code_call_family_odu.go`",
				"live-gate activation remains a separate layer",
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
				"The sql_relationships BASELINE, DELTA, and FAULT rows, the code_calls",
				"five families carries a waiver.",
				"Both live gates drive the rationale cassette and exact-assert its full\n  # EXPLAINS records.",
			},
			prohibited: []string{
				"confirmed-false fault row is waived",
				"MaterializedEdgeDomainEdgeTypes rejects",
				"rejects documentation_edges",
				"rejects rationale_edges",
				"drives the documentation_edges cassette",
				"No live cassette drives rationale_edges",
				"neither live gate drives it or asserts its live EXPLAINS edge set",
				"both dimensions stay WAIVED",
				"live determinism-gate drive + absolute EXPLAINS assertion pending",
				"reducer.DomainRationaleEdges extraction proven by odu:ifa-rationale-family; fault dimension pending domain-scoped live cells",
			},
		},
		{
			path: filepath.Join("go", "cmd", "ifa", "README.md"),
			required: []string{
				"`sql_relationships`, `code_calls`,\n  `documentation_edges`, and `rationale_edges`",
				"nine SQL edges,\n  five code-call edges, three documentation edges, and three full rationale",
				"`ifa-determinism` live gate invokes it in every worker-count cell",
				"one row per (surface, scenario) under two proof-gate IDs",
				"`sql_relationships` has three rows. Baseline and delta use `ifa-determinism`;\n  fault uses `ifa-fault-injection`.",
				"`code_calls` has two rows: baseline uses `ifa-determinism`; fault uses\n  `ifa-fault-injection`.",
				"`documentation_edges` has two rows: baseline uses `ifa-determinism`; fault\n  uses `ifa-fault-injection`.",
				"`rationale_edges` has two rows: baseline uses `ifa-determinism`; fault uses\n  `ifa-fault-injection`.",
				"fault-free\n  baseline asserts every family with a cataloged expected-edge set",
				"SQL kill/reclaim cell compares its graph with the baseline digest",
				"SQL write-retry cell repeats the exact nine-edge assertion",
				"Both code-call recovery cells repeat the exact five-edge assertion",
				"Both documentation recovery cells repeat the exact three-edge assertion",
				"The rationale recovery cells repeat the exact three-record assertion",
				"generation-2 SQL assertion runs first",
				"post-delta code-call and one-record rationale assertions follow before the\n  collateral comparison; that full-record comparison keeps documentation graph\n  records exact",
			},
			prohibited: []string{
				"`ifa-fault-injection` (baseline) live",
				"collateral check invokes it",
				"both `proof_gate` rows for",
				"post-delta code-call, documentation, and one-record rationale assertions",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges.go"),
			required: []string{
				"Current guards cover SQL relationships, documentation edges, code calls, rationale edges, codeowners ownership edges, and deployable-unit edges.",
			},
			prohibited: []string{
				"for \"sql_relationships\" today",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges_documentation_test.go"),
			required: []string{
				"Both dimensions are now",
				"live-proven and covered (#5994)",
			},
			prohibited: []string{
				"MaterializedEdgeDomainEdgeTypes rejects",
				"rejects documentation_edges",
				"No live cassette drives documentation_edges",
				"drives the documentation_edges cassette",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges_rationale_test.go"),
			required: []string{
				"MaterializedEdgeDomainEdgeTypes recognizes rationale_edges as EXPLAINS",
				"Both live gates drive the rationale cassette and exact-assert its full EXPLAINS records.",
			},
			prohibited: []string{
				"MaterializedEdgeDomainEdgeTypes rejects",
				"rejects rationale_edges",
				"No live cassette drives rationale_edges",
				"neither live gate drives it or asserts its live EXPLAINS edge set",
				"family stays waived",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "doc.go"),
			required: []string{
				"Documentation edges require baseline and fault dimensions. Both live matrices\n// exact-assert their three DOCUMENTS edges",
				"Rationale edges require baseline and fault dimensions. Both live matrices\n// exact-assert full EXPLAINS records",
			},
			prohibited: []string{
				"live-gate activation remains a separate layer",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "live_matrix_generation_identity_test.go"),
			required: []string{
				"testdata/cassettes/documentation/ifa-documentation-family.json",
			},
		},
		{
			path: filepath.Join("docs", "internal", "evidence", "5998-rationale-relative-path.md"),
			required: []string{
				"## Supported-backend live proof",
				"The post-rebase determinism matrix passed at N=1, N=2, and N=4",
				"25f325fac347ef984be2546fb20c278d1d50253f",
				"fd9bb6155a0703cfb8242c60db4826a918a1dd0a8f0364215fefc0de9e399f5f",
				"LIVE_5998_DETERMINISM_RC=0",
				"The invoking shell\nrecorded `LIVE_5998_DETERMINISM_RC=0`",
				"15 s, 18 s, and 18 s",
				"and exact one-record delta sets",
				"fb245bd3af9375b2cd86b23ec52cdd0550791088",
				"7a140b96e26f994357c2ecafa820f8b60dfdd9dea087280f53df78e4dd9319bc",
				"aa0904cc09da0b95bf78a0f27dd1b5b0e2aec15c371e0077edb81312360a4998",
				"280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052",
				"b1ef9c70490174a4f3893568709063c7d0c1f51591efef165f031b814ad612c2",
				"The 15-cell fault matrix passed",
				"7ca78272a54664b6e6f153722e5f4e321b7ebecb296eeb8e19becce3b3361140",
				"69de2944287e5c5ca8f5ec68160596628902aa70",
				"0323ada77cf58d3b53ab6ed875bb9d817e9fa0fd8f5ed793804889c3d862a234",
				"LIVE_5998_FAULT_RC=0",
				"The invoking shell recorded `LIVE_5998_FAULT_RC=0`",
				"superseded historical evidence from before the #6137 rebase",
				"| baseline | 19 s |",
				"| deltaretract | 53 s |",
				"| killworkerdocumentation | 74 s |",
				"| killworkerrationale | 74 s |",
				"| failgraphwriterationale | 11 s |",
				"fault-free documentation and rationale retry baselines were\nboth `0`",
				"attempt-1 lease snapshot remained byte-identical",
				"documentation retry count was\n`1`, strictly above baseline `0`",
				"one blocked claimed/running rationale\nrow",
				"rationale retry count was `1`, strictly\nabove baseline `0`",
				"strict durable marker check for the targeted EXPLAINS MERGE\nreturned success",
				"silent on success",
				"found zero\ncontainers, volumes, and networks",
				"no process command line retained either",
				"The earlier live runs on commit `48dc7ebafcb80f82bf3cf4edbc28ce49fb1f442e`",
				"are diagnostic evidence, not accepted proof",
				"The initial combined fault run",
				"b0b59991c460b21facd98382ddfb650be59ee27f0bdacc19fc200b1e6084c08f",
				"LIVE_COMBINED_FAULT_RC=1",
				"no retry above baseline",
				"The next three post-ACK-barrier runs",
				"diagnostic evidence, not accepted proof",
				"The PostgreSQL 18 ACK-barrier lifecycle probe is a separate, environment-gated",
				"invoke it automatically.",
			},
			prohibited: []string{
				"supported-backend proof remains pending",
				"The 13-cell fault matrix passed",
				"LIVE_DETERMINISM_RC=0",
				"LIVE_FAULT_RC=0",
				"The combined determinism matrix passed at N=1, N=2, and N=4 on commit\n`fb245bd3",
				"The 15-cell fault matrix passed on commit\n`69de2944",
				"printed the once-fired rationale marker",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges_rationale.go"),
			required: []string{
				"Parser-source tests pin the production-reachable exclusions; reducer guard tests pin malformed-envelope and precedence cases.",
			},
			prohibited: []string{
				"three of this fixture's seven negative cases",
				"fixture's seven negative cases",
			},
		},
		{
			path: filepath.Join("go", "internal", "reducer", "materialized_edge_families.go"),
			required: []string{
				"14 reducer-owned shared/edge projection domains",
				"codeowners_ownership_edges, submodule_pin_edges",
				"current 9 not-yet-covered allProjectionDomains families",
			},
			prohibited: []string{
				"12 reducer-owned shared/edge projection domains",
			},
		},
		{
			path: filepath.Join("go", "internal", "ifa", "materialized_edges_family_coverage_test.go"),
			required: []string{
				"The waiver rows and the edge-type registries",
			},
			prohibited: []string{
				"The 22 waiver rows and the edge-type registries",
				"The 24 waiver rows and the edge-type registries",
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
