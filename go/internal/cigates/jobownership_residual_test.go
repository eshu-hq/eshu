// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// writeRepoScript writes a repo script fixture under root so the cross-file
// script-body check has a callee to resolve.
func writeRepoScript(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestJobOwnershipResidualShapesFailClosed pins issue #8014: the three
// residual fail-open shapes in readsNeedResult read as uncovered unless a
// genuine result read covers the dependency. The existing 16-case table in
// TestJobOwnershipResultReadMustGate is untouched; these shapes live here.
func TestJobOwnershipResidualShapesFailClosed(t *testing.T) {
	t.Parallel()

	const always = "${{ always() }}"
	cases := map[string]struct {
		jobIf   string
		steps   string
		scripts map[string]string
		covered bool
	}{
		"script ignoring its ref arg is not a gate": {
			jobIf: always,
			steps: "      - run: bash scripts/lib/ignore.sh \"${{ needs.dep.result }}\"\n",
			scripts: map[string]string{
				"scripts/lib/ignore.sh": "#!/usr/bin/env bash\necho \"saw $1\"\nexit 0\n",
			},
			covered: false,
		},
		"script gating on its ref arg gates": {
			jobIf: always,
			steps: "      - run: bash scripts/lib/gate.sh \"${{ needs.dep.result }}\"\n",
			scripts: map[string]string{
				"scripts/lib/gate.sh": "#!/usr/bin/env bash\nif [ \"$1\" != success ]; then\n  echo \"bad result\"\n  exit 1\nfi\n",
			},
			covered: true,
		},
		"missing script is not a gate": {
			jobIf:   always,
			steps:   "      - run: bash scripts/lib/missing.sh \"${{ needs.dep.result }}\"\n",
			scripts: nil,
			covered: false,
		},
		"tautological job if never gates": {
			jobIf:   "${{ always() || needs.dep.result == 'success' }}",
			steps:   "      - run: echo hi\n",
			covered: false,
		},
		"conjunctive job if gates": {
			jobIf:   "always() && needs.dep.result == 'success'",
			steps:   "      - run: echo hi\n",
			covered: true,
		},
		"if without failing body does not gate": {
			jobIf: always,
			steps: "      - run: |\n" +
				"          result=\"${{ needs.dep.result }}\"\n" +
				"          if [ \"$result\" != \"success\" ]; then\n" +
				"            echo \"bad: $result\"\n" +
				"          fi\n",
			covered: false,
		},
		"case with failing arm gates": {
			jobIf: always,
			steps: "      - run: |\n" +
				"          result=\"${{ needs.dep.result }}\"\n" +
				"          case \"$result\" in\n" +
				"            success) ;;\n" +
				"            *) exit 1 ;;\n" +
				"          esac\n",
			covered: true,
		},
		"if with failing body gates": {
			jobIf: always,
			steps: "      - run: |\n" +
				"          result=\"${{ needs.dep.result }}\"\n" +
				"          if [ \"$result\" != \"success\" ]; then\n" +
				"            echo \"bad: $result\"\n" +
				"            exit 1\n" +
				"          fi\n",
			covered: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			for rel, content := range tc.scripts {
				writeRepoScript(t, root, rel, content)
			}
			writeWorkflow(t, root, "g.yml", gatingWorkflow(tc.jobIf, tc.steps))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "g.yml", "owned-job")}, nil, nil)
			errs := ownershipErrors(t, root, reg)
			if tc.covered && len(errs) != 0 {
				t.Errorf("want dep covered, got: %v", errs)
			}
			if !tc.covered && (len(errs) != 1 || !strings.Contains(errs[0], `"dep"`)) {
				t.Errorf("want one finding for dep, got: %v", errs)
			}
		})
	}
}
