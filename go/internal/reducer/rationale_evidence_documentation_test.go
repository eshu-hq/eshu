// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRationaleEvidenceDocumentsCurrentCollectorAndReducerCost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path       string
		required   []string
		prohibited []string
	}{
		{
			path: filepath.Join("..", "collector", "doc.go"),
			required: []string{
				"Full and delta Git generations emit one unconditional",
				"rationale-materialization follow-up after their content-entity facts",
			},
			prohibited: []string{"delta snapshots skip those repo-wide follow-ups"},
		},
		{
			path: filepath.Join("..", "collector", "README.md"),
			required: []string{
				"rationale_materialization",
				"one follow-up fact and one reducer work item",
			},
		},
		{
			path: filepath.Join("..", "..", "..", "docs", "internal", "evidence", "5998-rationale-relative-path.md"),
			required: []string{
				"12-fact rationale cassette",
				"repo-relative `target_path`",
				"repository-qualified delta retract paths",
				"5,000 content entities",
				"The benchmark uses an in-memory",
				"`ListFactsByKind` test double",
				"excludes the Postgres query and decode",
				"Supported-backend live proof",
				"25f325fac347ef984be2546fb20c278d1d50253f",
				"LIVE_5998_DETERMINISM_RC=0",
				"The invoking shell\nrecorded `LIVE_5998_DETERMINISM_RC=0`",
				"fd9bb6155a0703cfb8242c60db4826a918a1dd0a8f0364215fefc0de9e399f5f",
				"LIVE_5998_FAULT_RC=0",
				"The invoking shell recorded `LIVE_5998_FAULT_RC=0`",
				"7ca78272a54664b6e6f153722e5f4e321b7ebecb296eeb8e19becce3b3361140",
				"7a140b96e26f994357c2ecafa820f8b60dfdd9dea087280f53df78e4dd9319bc",
				"aa0904cc09da0b95bf78a0f27dd1b5b0e2aec15c371e0077edb81312360a4998",
				"280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052",
				"b1ef9c70490174a4f3893568709063c7d0c1f51591efef165f031b814ad612c2",
				"The 15-cell fault matrix passed",
				"15 s, 18 s, and 18 s",
				"and exact one-record delta sets",
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
				"fb245bd3af9375b2cd86b23ec52cdd0550791088",
				"69de2944287e5c5ca8f5ec68160596628902aa70",
				"0323ada77cf58d3b53ab6ed875bb9d817e9fa0fd8f5ed793804889c3d862a234",
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
				"5 content-entity envelopes and 1 repository envelope",
				"No loop, allocation, query, or batch boundary moves",
				"remains pending",
				"The 13-cell fault matrix passed",
				"LIVE_DETERMINISM_RC=0",
				"LIVE_FAULT_RC=0",
				"The combined determinism matrix passed at N=1, N=2, and N=4 on commit\n`fb245bd3",
				"The 15-cell fault matrix passed on commit\n`69de2944",
				"printed the once-fired rationale marker",
			},
		},
		{
			path: "README.md",
			required: []string{
				"Rationale EXPLAINS reconciliation",
				"docs/internal/evidence/5998-rationale-relative-path.md",
				"completed live proof on the supported backend",
			},
			prohibited: []string{"pending supported-backend proof"},
		},
		{
			path: "doc.go",
			required: []string{
				"Rationale materialization",
				"repo-relative target paths",
				"repository-qualified delta paths",
			},
		},
		{
			path:       "AGENTS.md",
			required:   []string{"`reducer/rationale`"},
			prohibited: []string{"`reducer/rationale-edge`"},
		},
		{
			path: "rationale_edge_intents.go",
			required: []string{
				"target entity's repo-relative file path",
				"Delta retraction",
				"repository-qualified delta_file_paths",
			},
			prohibited: []string{"target entity's repo-qualified file path"},
		},
		{
			path:       "rationale_edge_materialization_test.go",
			prohibited: []string{`"relative_path": "/repo/src/`},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatalf("read %s: %v", test.path, err)
			}
			content := string(raw)
			for _, required := range test.required {
				if !strings.Contains(content, required) {
					t.Errorf("%s missing current rationale proof phrase %q", test.path, required)
				}
			}
			for _, prohibited := range test.prohibited {
				if strings.Contains(content, prohibited) {
					t.Errorf("%s retains stale rationale proof phrase %q", test.path, prohibited)
				}
			}
		})
	}
}
