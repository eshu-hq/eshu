// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// TestCheckRequiredStatusWorkflows_PinsMainPushFanInFilter proves the
// aggregator's workflow_run trigger carries exactly the default-branch
// exclusion (#7111 F8). A main push runs ~21 of the listed workflows, each
// firing an in_progress and a completed workflow_run event into one per-SHA
// concurrency group; the job-level `if` skips them but only after concurrency cancels all
// but the last pending run, so every main push buried the run list in
// cancelled Required Gates rows. The exclusion drops that fan-in at the
// trigger. Pull-request and merge-queue heads never run on `main`, so the
// required-gates-complete status they read is unaffected; any other filter
// shape would silently stop aggregating some pull requests, so it is rejected.
func TestCheckRequiredStatusWorkflows_PinsMainPushFanInFilter(t *testing.T) {
	t.Parallel()

	const filter = "    branches-ignore: [main]\n"
	tests := map[string]string{
		"missing default-branch exclusion": strings.Replace(trustedRequiredWorkflow, filter, "", 1),
		"branches allowlist instead":       strings.Replace(trustedRequiredWorkflow, filter, "    branches: [main]\n", 1),
		"exclusion of a different branch":  strings.Replace(trustedRequiredWorkflow, filter, "    branches-ignore: [release]\n", 1),
		"exclusion widened past main":      strings.Replace(trustedRequiredWorkflow, filter, "    branches-ignore: [main, 'feature/**']\n", 1),
		"exclusion plus allowlist":         strings.Replace(trustedRequiredWorkflow, filter, "    branches-ignore: [main]\n    branches: ['**']\n", 1),
		"empty exclusion":                  strings.Replace(trustedRequiredWorkflow, filter, "    branches-ignore: []\n", 1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
			found := false
			for _, err := range errs {
				if strings.Contains(err.Error(), "branches-ignore") {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected a branches-ignore error, got: %v", errs)
			}
		})
	}
}

func TestCheckRequiredStatusWorkflows_AcceptsMainExclusionSpellings(t *testing.T) {
	t.Parallel()

	for name, filter := range map[string]string{
		"flow list":  "    branches-ignore: [main]\n",
		"block list": "    branches-ignore:\n      - main\n",
		"scalar":     "    branches-ignore: main\n",
	} {
		body := strings.Replace(trustedRequiredWorkflow, "    branches-ignore: [main]\n", filter, 1)
		if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
			t.Fatalf("%s: equivalent exclusion returned errors: %v", name, errs)
		}
	}
}
