// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// ownerRowTriggerErrors returns only the findings of the CI-only owner-row
// trigger check, so a fixture proves that one rule.
func ownerRowTriggerErrors(t *testing.T, root string, reg *cigates.Registry) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, reg) {
		if strings.Contains(err.Error(), "owner row trigger") {
			out = append(out, err.Error())
		}
	}
	return out
}

// ciOnlyGate returns a blocking CI-only gate (no local command) that owns job
// in workflow and selects on triggers.
func ciOnlyGate(id, workflow, job string, triggers ...string) cigates.Gate {
	g := ownershipGate(id, workflow, job)
	g.Local = nil
	g.CIOnlyReason = "the job only owns a CI check"
	g.Triggers = triggers
	return g
}

const ownerRowPathsWorkflow = "name: Paths\non:\n  pull_request:\n    paths:\n" +
	"      - 'covered/**'\n      - 'scripts/run.sh'\n      - '!covered/skip/**'\n" +
	"jobs:\n  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n"

// TestOwnerRowTriggerMustStartTheWorkflow pins the rule the replay-coverage
// static-mirror row broke: a CI-only owner row selects its check from its
// triggers, so a literal trigger that no pull_request paths: entry matches
// selects a check that never arrives, and `ci-gates await` waits out its
// timeout on a PR that touches only that file.
func TestOwnerRowTriggerMustStartTheWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("uncovered literal trigger is flagged", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", ownerRowPathsWorkflow)
		g := ciOnlyGate("a", "paths.yml", "owned-job", "scripts/run.sh", "scripts/lib/guard.sh")
		errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
		if len(errs) != 1 || !strings.Contains(errs[0], "scripts/lib/guard.sh") || !strings.Contains(errs[0], `"a"`) {
			t.Errorf("want one finding naming the gate and scripts/lib/guard.sh, got: %v", errs)
		}
	})
	t.Run("trigger excluded by a negated path is flagged", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", ownerRowPathsWorkflow)
		g := ciOnlyGate("a", "paths.yml", "owned-job", "covered/skip/x.sh")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 1 {
			t.Errorf("a trigger removed by a ! entry never starts the workflow, got: %v", errs)
		}
	})
	t.Run("covered triggers are clean", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", ownerRowPathsWorkflow)
		g := ciOnlyGate("a", "paths.yml", "owned-job", "scripts/run.sh", "covered/deep/file.go")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("triggers a paths: entry matches are fine, got: %v", errs)
		}
	})
	t.Run("a workflow with no pull_request paths is out of scope", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", orphanWorkflow)
		g := ciOnlyGate("a", "paths.yml", "owned-job", "anywhere/file.sh")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("a workflow that starts on every PR cannot miss a trigger, got: %v", errs)
		}
	})
	t.Run("a row with a local command is out of scope", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", ownerRowPathsWorkflow)
		g := ownershipGate("a", "paths.yml", "owned-job")
		g.Triggers = []string{"elsewhere/file.sh"}
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("only CI-only owner rows are checked, got: %v", errs)
		}
	})
	t.Run("glob triggers are not judged", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "paths.yml", ownerRowPathsWorkflow)
		g := ciOnlyGate("a", "paths.yml", "owned-job", "elsewhere/**")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("a glob trigger is not judged against paths:, got: %v", errs)
		}
	})
}

const ownerRowIgnoreWorkflow = "name: Ignore\non:\n  pull_request:\n    paths-ignore:\n" +
	"      - 'docs/**'\n      - 'notes/*.md'\n" +
	"jobs:\n  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n"

// TestOwnerRowTriggerMustNotBeIgnoredByThePullRequestFilter covers the
// paths-ignore form of the same defect: the workflow starts unless EVERY
// changed file matches an ignore entry, so a literal trigger that matches one
// selects a check the workflow does not start when only that file changes.
func TestOwnerRowTriggerMustNotBeIgnoredByThePullRequestFilter(t *testing.T) {
	t.Parallel()

	t.Run("trigger matching an ignore entry is flagged", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "ignore.yml", ownerRowIgnoreWorkflow)
		g := ciOnlyGate("a", "ignore.yml", "owned-job", "scripts/run.sh", "docs/guide.md")
		errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
		if len(errs) != 1 || !strings.Contains(errs[0], `"a"`) || !strings.Contains(errs[0], "docs/guide.md") ||
			!strings.Contains(errs[0], "docs/**") {
			t.Errorf("want one finding naming the gate, the trigger and the ignore entry docs/**, got: %v", errs)
		}
	})
	t.Run("trigger no ignore entry matches is clean", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "ignore.yml", ownerRowIgnoreWorkflow)
		g := ciOnlyGate("a", "ignore.yml", "owned-job", "scripts/run.sh", "notes/deep/file.md")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("a trigger outside every ignore entry starts the workflow, got: %v", errs)
		}
	})
	t.Run("glob triggers are not judged against paths-ignore", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "ignore.yml", ownerRowIgnoreWorkflow)
		g := ciOnlyGate("a", "ignore.yml", "owned-job", "docs/**")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("a glob trigger is not judged, got: %v", errs)
		}
	})
	t.Run("paths and paths-ignore together fail closed", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "both.yml", "name: Both\non:\n  pull_request:\n    paths:\n      - 'scripts/**'\n"+
			"    paths-ignore:\n      - 'docs/**'\n"+
			"jobs:\n  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n")
		g := ciOnlyGate("a", "both.yml", "owned-job", "scripts/run.sh")
		errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
		if len(errs) != 1 || !strings.Contains(errs[0], "paths-ignore") || !strings.Contains(errs[0], "both.yml") {
			t.Errorf("GitHub rejects both filters on one event, so the workflow never starts; want one finding, got: %v", errs)
		}
	})
	t.Run("a workflow with no pull_request filter stays out of scope", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "plain.yml", orphanWorkflow)
		g := ciOnlyGate("a", "plain.yml", "owned-job", "docs/guide.md")
		if errs := ownerRowTriggerErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("an unfiltered workflow starts on every PR, got: %v", errs)
		}
	})
}
