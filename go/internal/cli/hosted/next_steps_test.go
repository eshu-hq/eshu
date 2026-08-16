// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// TestSetupWarnStagesGetCategoryNextSteps is the regression test for the
// dead next-step branches found in review: the not-ready index path records
// its stage as StageWarn, and firstFailure used to match only StageFailed, so
// an operator with an empty, still-building, or stale index always got the
// generic "Re-run: eshu hosted-setup" instead of the category-specific step.
// Each case drives the real ExecuteSetup path and asserts the first next step
// is the one written for that category — and that the stage stays StageWarn,
// because the fix must not regress the human render's [~~] marker to [!!].
func TestSetupWarnStagesGetCategoryNextSteps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		verdict      scan.ReadinessVerdict
		repos        RepositoryList
		wantCategory FailCategory
		wantStep     string
	}{
		{
			name:         "empty index",
			verdict:      scan.ReadinessVerdict{Ready: true, Reason: "pipeline healthy and drained"},
			repos:        RepositoryList{},
			wantCategory: FailEmptyIndex,
			wantStep:     "Index a repository on the deployed service, then re-run: eshu hosted-setup",
		},
		{
			name:         "partial readiness",
			verdict:      scan.ReadinessVerdict{Ready: false, Reason: "queue still draining"},
			repos:        repoList("acme/api"),
			wantCategory: FailPartialReadiness,
			wantStep:     "Wait for indexing to drain on the deployed service, then re-run: eshu hosted-setup",
		},
		{
			name:         "stale readiness",
			verdict:      scan.ReadinessVerdict{Terminal: true, Reason: "dead-letter work present"},
			repos:        repoList("acme/api"),
			wantCategory: FailStaleReadiness,
			wantStep:     "Inspect the deployed pipeline status; clear failed or dead-letter work, then re-run.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps := okDeps()
			deps.Readiness = func() (scan.ReadinessVerdict, error) { return tc.verdict, nil }
			deps.ListRepos = func() (RepositoryList, error) { return tc.repos, nil }

			result, err := ExecuteSetup(deps, baseSetupOptions())
			if err == nil {
				t.Fatal("ExecuteSetup err = nil, want a not-ready index error")
			}
			stage := stageByName(result, StageIndexReadiness)
			if stage.Category != tc.wantCategory {
				t.Fatalf("index-readiness category = %q, want %q", stage.Category, tc.wantCategory)
			}
			if stage.Status != StageWarn {
				t.Fatalf("index-readiness status = %q, want %q (the fix must not flip warn to failed)", stage.Status, StageWarn)
			}
			if len(result.NextSteps) == 0 {
				t.Fatalf("NextSteps empty, want the %s step", tc.wantCategory)
			}
			if result.NextSteps[0] != tc.wantStep {
				t.Fatalf("NextSteps[0] = %q, want %q", result.NextSteps[0], tc.wantStep)
			}

			var human bytes.Buffer
			RenderSetupHuman(&human, result, err)
			rendered := human.String()
			if !strings.Contains(rendered, "[~~] "+string(StageIndexReadiness)) {
				t.Fatalf("human render lost the [~~] warn marker for %q:\n%s", StageIndexReadiness, rendered)
			}
			if !strings.Contains(rendered, tc.wantStep) {
				t.Fatalf("human render is missing the %s next step:\n%s", tc.wantCategory, rendered)
			}
		})
	}
}
