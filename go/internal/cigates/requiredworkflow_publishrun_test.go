// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// These test the harness itself rather than the workflow. The contract tests
// in required_workflow_outcome_test.go trust that what EvaluatePublisher
// reports is what the shell did; these are what makes that trustworthy, and
// they pin the boundary its doc comment claims.

// TestEvaluatePublisherReportsThePostExpansionArgv is the core promise: the
// values reported are what the shell handed `gh`, not the text that produced
// them. The fixture spells every field indirectly so a reader of the TEXT
// would see none of the reported values.
func TestEvaluatePublisherReportsThePostExpansionArgv(t *testing.T) {
	t.Parallel()

	const run = `verb=suc
tail=cess
state="${verb}${tail}"
note='a gate was cancelled'
gh api -X POST "repos/${GITHUB_REPOSITORY}/statuses/${HEAD_SHA}" \
  -f state="${state}" \
  -f context=required-gates-complete \
  -f description="${note}"
`
	observed, err := EvaluatePublisher(run, 0)
	if err != nil {
		t.Fatalf("evaluate publisher: %v", err)
	}
	if len(observed.Publishes) != 1 {
		t.Fatalf("observed %d publishes, want 1: %+v", len(observed.Publishes), observed)
	}
	published := observed.Publishes[0]
	if published.State != "success" {
		t.Errorf("state = %q, want %q; the harness must report what the shell expanded, not the text",
			published.State, "success")
	}
	if published.Description != "a gate was cancelled" {
		t.Errorf("description = %q, want %q", published.Description, "a gate was cancelled")
	}
	if published.Context != "required-gates-complete" {
		t.Errorf("context = %q, want %q", published.Context, "required-gates-complete")
	}
	if !strings.Contains(published.URL, "/statuses/"+publisherProbeHeadSHA) {
		t.Errorf("url = %q, want it to target the probe head SHA %s", published.URL, publisherProbeHeadSHA)
	}
}

// TestEvaluatePublisherSeesTheAggregateCode proves the exit code actually
// reaches the script, so a per-code assertion is testing the arm it names.
func TestEvaluatePublisherSeesTheAggregateCode(t *testing.T) {
	t.Parallel()

	const run = `case "${AGGREGATE_CODE}" in
  13) state=error ;;
  *) state=success ;;
esac
gh api -X POST "repos/x/y/statuses/${HEAD_SHA}" -f state="${state}"
`
	for code, want := range map[int]string{13: "error", 0: "success"} {
		observed, err := EvaluatePublisher(run, code)
		if err != nil {
			t.Fatalf("evaluate publisher for %d: %v", code, err)
		}
		if len(observed.Publishes) != 1 || observed.Publishes[0].State != want {
			t.Errorf("code %d published %+v, want one publish with state=%q", code, observed.Publishes, want)
		}
	}
}

// TestEvaluatePublisherRecordsNothingWhenThePublisherExitsEarly pins the
// still-running shape: a script that returns before the publish records no
// publish, which is how "publishes nothing" is distinguished from "publishes
// something wrong".
func TestEvaluatePublisherRecordsNothingWhenThePublisherExitsEarly(t *testing.T) {
	t.Parallel()

	observed, err := EvaluatePublisher("exit 0\ngh api -X POST \"repos/x/y/statuses/${HEAD_SHA}\" -f state=success\n", 11)
	if err != nil {
		t.Fatalf("evaluate publisher: %v", err)
	}
	if len(observed.Publishes) != 0 {
		t.Fatalf("observed %+v, want no publish at all", observed.Publishes)
	}
}

// TestEvaluatePublisherFailsClosedWhenGhBypassesTheRecorder pins the boundary
// EvaluatePublisher's doc comment states. Interception is a bash function, so
// `command gh` skips it -- and PATH holds one empty directory precisely so
// that spelling finds no `gh` at all, dies under `-e`, and shows up as
// "published nothing" rather than as a silent pass or a call to a real `gh`.
func TestEvaluatePublisherFailsClosedWhenGhBypassesTheRecorder(t *testing.T) {
	t.Parallel()

	observed, err := EvaluatePublisher("command gh api -X POST \"repos/x/y/statuses/${HEAD_SHA}\" -f state=success\n", 0)
	if err != nil {
		t.Fatalf("evaluate publisher: %v", err)
	}
	if len(observed.Publishes) != 0 {
		t.Fatalf("observed %+v; a bypassed recorder must record nothing", observed.Publishes)
	}
	if observed.ExitCode == 0 {
		t.Fatal("a publisher whose gh call cannot resolve must not exit 0; the failure has to be visible")
	}
}

// TestEvaluatePublisherIgnoresNonStatusGhCalls keeps the harness from
// mistaking some other use of `gh` for the publish it is judging.
func TestEvaluatePublisherIgnoresNonStatusGhCalls(t *testing.T) {
	t.Parallel()

	const run = `gh api "repos/${GITHUB_REPOSITORY}/pulls/1" > /dev/null
gh api -X POST "repos/${GITHUB_REPOSITORY}/statuses/${HEAD_SHA}" -f state=success
`
	observed, err := EvaluatePublisher(run, 0)
	if err != nil {
		t.Fatalf("evaluate publisher: %v", err)
	}
	if len(observed.Publishes) != 1 {
		t.Fatalf("observed %d publishes, want only the /statuses/ one: %+v",
			len(observed.Publishes), observed.Publishes)
	}
}

// TestEvaluatePublisherCarriesDescriptionsContainingSeparatorProneText keeps
// the recording frame honest. A description legitimately contains spaces,
// semicolons and parentheses, so the recorder frames argv with ASCII RS/US
// rather than anything a description could contain.
func TestEvaluatePublisherCarriesDescriptionsContainingSeparatorProneText(t *testing.T) {
	t.Parallel()

	const want = "A required gate was cancelled or never ran; re-run it (no gate failed)"
	run := "gh api -X POST \"repos/x/y/statuses/${HEAD_SHA}\" -f state=error -f description='" + want + "'\n"
	observed, err := EvaluatePublisher(run, 13)
	if err != nil {
		t.Fatalf("evaluate publisher: %v", err)
	}
	if len(observed.Publishes) != 1 || observed.Publishes[0].Description != want {
		t.Fatalf("observed %+v, want one publish described %q", observed.Publishes, want)
	}
}

// TestTerminalPublisherRunReportsAnAmbiguousWorkflow keeps step selection from
// guessing. Two publishers, or none, is a contract that moved, and evaluating
// a step nobody meant would prove something about the wrong shell.
func TestTerminalPublisherRunReportsAnAmbiguousWorkflow(t *testing.T) {
	t.Parallel()

	const twoTerminals = `name: Required Gates
jobs:
  aggregate:
    steps:
      - name: Publish pending
        run: gh api -X POST repos/x/y/statuses/${HEAD_SHA} -f state=pending
      - name: Publish terminal
        run: gh api -X POST repos/x/y/statuses/${HEAD_SHA} -f state=success
      - name: Publish terminal again
        run: gh api -X POST repos/x/y/statuses/${HEAD_SHA} -f state=error
`
	root := t.TempDir()
	path := writeWorkflowFile(t, root, twoTerminals)
	if _, err := TerminalPublisherRun(path); err == nil {
		t.Fatal("two terminal publishers must be reported, not silently resolved to one")
	}

	const noTerminal = `name: Required Gates
jobs:
  aggregate:
    steps:
      - name: Publish pending
        run: gh api -X POST repos/x/y/statuses/${HEAD_SHA} -f state=pending
`
	if _, err := TerminalPublisherRun(writeWorkflowFile(t, t.TempDir(), noTerminal)); err == nil {
		t.Fatal("a workflow with no terminal publisher must be reported")
	}
}

// TestTerminalPublisherRunFindsTheRealPublisher is the positive control for
// the selection above, run against the workflow this repository ships.
func TestTerminalPublisherRunFindsTheRealPublisher(t *testing.T) {
	t.Parallel()

	run, err := TerminalPublisherRun(repositoryRequiredGatesWorkflow(t))
	if err != nil {
		t.Fatalf("locate the repository's terminal publisher: %v", err)
	}
	if !strings.Contains(run, "AGGREGATE_CODE") {
		t.Fatalf("the located step does not branch on AGGREGATE_CODE; the wrong step was selected:\n%s", run)
	}
	if strings.Contains(run, "state=pending") {
		t.Fatalf("the located step is the pending publisher, not the terminal one:\n%s", run)
	}
}
