// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeGroupIfIsTrue(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"":                                      true,
		"${{ always() }}":                       true,
		"${{ !cancelled() }}":                   true,
		"always()":                              true,
		"github.event_name == 'merge_group'":    true,
		"github.event_name != 'pull_request'":   true,
		"github.event_name == 'pull_request'":   false,
		"needs.changes.outputs.image == 'true'": false,
		"github.event_name == 'merge_group' || needs.changes.outputs.image == 'true'":                                                 true,
		"${{ github.event_name != 'pull_request' || needs.changes.outputs.code == 'true' }}":                                          true,
		"github.event_name != 'workflow_run' && (github.event_name != 'pull_request' || needs.changes.outputs.code == 'true')":        true,
		"${{ github.event_name != 'workflow_run' && (github.event_name != 'pull_request' || needs.changes.outputs.code == 'true') }}": true,
		"needs.changes.outputs.image == 'true' && github.ref_type == 'tag'":                                                           false,
		"startsWith(github.ref, 'refs/tags/v')":                                                                                       false,
		"${{ needs.changes.outputs.any == 'true' }}":                                                                                  false,
		"github.event_name == 'MERGE_GROUP'":                                                                                          true,
		"!(github.event_name == 'pull_request')":                                                                                      true,
		"github.event_name == 'pull_request' && always()":                                                                             false,
		"github.event_name != 'merge_group' && needs.x.result == 'success'":                                                           false,
	}
	for expr, want := range cases {
		if got := mergeGroupIfIsTrue(expr); got != want {
			t.Errorf("mergeGroupIfIsTrue(%q) = %v; want %v", expr, got, want)
		}
	}
}

const pathSelectiveBlockingWorkflow = `name: Publish
on:
  pull_request:
  merge_group:
    types: [checks_requested]
jobs:
  changes:
    runs-on: ubuntu-latest
    steps:
      - run: echo
  build:
    needs: changes
    if: needs.changes.outputs.image == 'true'
    runs-on: ubuntu-latest
    steps:
      - run: echo
  verify:
    needs: [changes, build]
    runs-on: ubuntu-latest
    steps:
      - run: echo
`

func writeMergeGroupJobFixture(t *testing.T, body string, jobs ...string) (string, *Registry) {
	t.Helper()
	root := writeRequiredWorkflowFixture(t, strings.Replace(trustedRequiredWorkflow, `workflows: ["Build Test"]`, `workflows: ["Build Test", "Publish"]`, 1))
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "publish.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := requiredWorkflowRegistry()
	for _, job := range jobs {
		reg.Gates = append(reg.Gates, Gate{ID: "gate-" + job, Blocking: true, CI: CI{Workflow: "publish.yml", Job: job}})
	}
	return root, reg
}

func mergeGroupSkipErrors(errs []error) []string {
	var out []string
	for _, err := range errs {
		if strings.Contains(err.Error(), "can be skipped on merge_group") {
			out = append(out, err.Error())
		}
	}
	return out
}

// TestCheckRequiredStatusWorkflows_FlagsBlockingJobSkippableOnMergeGroup is
// the regression for the truncated-selection false red: a queue entry whose
// compare hits the 300-file cap selects every blocking gate, so a blocking
// job whose own `if:` can be false on merge_group -- or that needs such a
// job -- is reported SKIPPED and publishes `failure` for a gate that does not
// apply.
func TestCheckRequiredStatusWorkflows_FlagsBlockingJobSkippableOnMergeGroup(t *testing.T) {
	t.Parallel()

	root, reg := writeMergeGroupJobFixture(t, pathSelectiveBlockingWorkflow, "build", "verify")
	got := mergeGroupSkipErrors(checkRequiredStatusWorkflows(root, reg))
	if len(got) != 2 {
		t.Fatalf("want build (own if) and verify (needs build) flagged, got %d: %v", len(got), got)
	}
}

func TestCheckRequiredStatusWorkflows_AcceptsBlockingJobThatRunsOnMergeGroup(t *testing.T) {
	t.Parallel()

	fixed := strings.Replace(pathSelectiveBlockingWorkflow,
		"if: needs.changes.outputs.image == 'true'",
		"if: github.event_name == 'merge_group' || needs.changes.outputs.image == 'true'", 1)
	root, reg := writeMergeGroupJobFixture(t, fixed, "build", "verify")
	if got := mergeGroupSkipErrors(checkRequiredStatusWorkflows(root, reg)); len(got) != 0 {
		t.Fatalf("jobs that always run on merge_group were flagged: %v", got)
	}
}

func TestCheckRequiredStatusWorkflows_AlwaysOverridesSkippedNeeds(t *testing.T) {
	t.Parallel()

	body := strings.Replace(pathSelectiveBlockingWorkflow,
		"    needs: [changes, build]\n",
		"    needs: [changes, build]\n    if: ${{ always() }}\n", 1)
	root, reg := writeMergeGroupJobFixture(t, body, "verify")
	if got := mergeGroupSkipErrors(checkRequiredStatusWorkflows(root, reg)); len(got) != 0 {
		t.Fatalf("an always() umbrella runs whatever its needs did, got: %v", got)
	}
}

func TestCheckRequiredStatusWorkflows_NotCancelledIsNotTreatedAsNeedsOverride(t *testing.T) {
	t.Parallel()

	// always() is the only needs override the guard models. A job gated on
	// !cancelled() is treated as skippable with its need, so it is flagged:
	// the guard errs toward a false red, never a false green.
	body := strings.Replace(pathSelectiveBlockingWorkflow,
		"    needs: [changes, build]\n",
		"    needs: [changes, build]\n    if: ${{ !cancelled() }}\n", 1)
	root, reg := writeMergeGroupJobFixture(t, body, "verify")
	if got := mergeGroupSkipErrors(checkRequiredStatusWorkflows(root, reg)); len(got) != 1 {
		t.Fatalf("a !cancelled() job whose need can skip is flagged once, got %d: %v", len(got), got)
	}
}

func TestCheckRequiredStatusWorkflows_ResolvesMatrixNamedJobs(t *testing.T) {
	t.Parallel()

	body := `name: Publish
on:
  merge_group:
jobs:
  changes:
    runs-on: ubuntu-latest
    steps:
      - run: echo
  gate:
    name: ${{ matrix.display }}
    needs: changes
    if: ${{ needs.changes.outputs.any == 'true' }}
    runs-on: ubuntu-latest
    steps:
      - run: echo
`
	root, reg := writeMergeGroupJobFixture(t, body, "Verify OpenAPI gate")
	if got := mergeGroupSkipErrors(checkRequiredStatusWorkflows(root, reg)); len(got) != 1 {
		t.Fatalf("append_gate display must resolve to its matrix host job and be flagged, got: %v", got)
	}
}

// TestCommittedBlockingJobsAllRunOnMergeGroup ties the two halves of the
// truncated-selection contract together on the real tree. When a queue
// entry's compare reaches the 300-file cap, ci-gates await selects
// AllBlockingGates -- exactly the set this guard walks -- so every one of
// those jobs must run (never be SKIPPED) on merge_group, or the entry fails
// on a gate that does not apply to it (#7111 review F1).
func TestCommittedBlockingJobsAllRunOnMergeGroup(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	reg, err := Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	all, err := reg.AllBlockingGates()
	if err != nil {
		t.Fatalf("AllBlockingGates: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no blocking gates: the guard below would be vacuous")
	}
	for _, check := range reg.RequiredStatusChecks {
		if !check.AggregatesBlockingGates {
			continue
		}
		if got := mergeGroupSkipErrors(validateBlockingJobsRunOnMergeGroup(repoRoot, check, reg)); len(got) != 0 {
			t.Fatalf("blocking jobs a truncated merge_group selection would see SKIPPED:\n%s", strings.Join(got, "\n"))
		}
	}
}
