// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRequiredWorkflowSkipsClosedPRStatus runs the committed workflow's first
// step with the same closed-PR payload that reached #7206 after its merge.
// The recorder observes status API calls, rather than looking for a guard's
// wording in the YAML.
func TestRequiredWorkflowSkipsClosedPRStatus(t *testing.T) {
	steps := repositoryRequiredWorkflowSteps(t)
	pending := requiredStepNamed(t, steps, "Publish pending required status")
	terminal := requiredStepNamed(t, steps, "Publish terminal required status")
	const mergedOnly = `[{"number":7206,"state":"closed","merged_at":"2026-09-27T19:45:14Z","head":{"sha":"probe-head"}}]`

	got := runRequiredStepWithPRs(t, pending, "pull_request", mergedOnly, nil)
	if got.err != nil {
		t.Fatalf("closed PR pending step failed: %v\n%s", got.err, got.output)
	}
	if strings.Contains(got.calls, "-X POST") {
		t.Fatalf("closed PR head must not replace a verified status with pending; calls:\n%s", got.calls)
	}
	if !strings.Contains(got.outputs, "disposition=inactive") {
		t.Fatalf("closed PR head must be classified inactive, outputs: %q", got.outputs)
	}

	got = runRequiredStepWithPRs(t, terminal, "pull_request", mergedOnly, map[string]string{
		"PR_DISPOSITION": "inactive", "PENDING_OUTCOME": "success", "AGGREGATE_CODE": "0",
	})
	if got.err != nil {
		t.Fatalf("closed PR terminal step failed: %v\n%s", got.err, got.output)
	}
	if strings.Contains(got.calls, "-X POST") {
		t.Fatalf("closed PR head must not receive a terminal overwrite; calls:\n%s", got.calls)
	}
}

func TestRequiredWorkflowPendingPRDispositions(t *testing.T) {
	pending := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish pending required status")
	tests := []struct {
		name            string
		event           string
		pulls           string
		wantDisposition string
		wantNumber      string
		wantPost        bool
		wantLookup      bool
		wantError       bool
	}{
		{"one open", "pull_request", `[{"number":7206,"state":"open","head":{"sha":"probe-head"}}]`, "active", "7206", true, true, false},
		{"two open", "pull_request", `[{"number":7206,"state":"open","head":{"sha":"probe-head"}},{"number":7207,"state":"open","head":{"sha":"probe-head"}}]`, "ambiguous", "", true, true, false},
		{"closed and open", "pull_request", `[{"number":7206,"state":"closed","head":{"sha":"probe-head"}},{"number":7207,"state":"open","head":{"sha":"probe-head"}}]`, "active", "7207", true, true, false},
		{"no association", "pull_request", `[]`, "inactive", "", false, true, false},
		{"malformed response", "pull_request", `{`, "", "", false, true, true},
		{"lookup failure", "pull_request", `API_ERROR`, "", "", false, true, true},
		{"merge group", "merge_group", `[]`, "active", "", true, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runRequiredStepWithPRs(t, pending, tc.event, tc.pulls, nil)
			if (got.err != nil) != tc.wantError {
				t.Fatalf("step error = %v, want error %t; output: %s", got.err, tc.wantError, got.output)
			}
			if post := strings.Contains(got.calls, "-X POST"); post != tc.wantPost {
				t.Fatalf("status POST = %t, want %t; calls:\n%s", post, tc.wantPost, got.calls)
			}
			if tc.wantPost {
				assertRequiredStatusCall(t, got.calls, "pending")
			}
			if lookup := strings.Contains(got.calls, "/pulls"); lookup != tc.wantLookup {
				t.Fatalf("PR lookup = %t, want %t; calls:\n%s", lookup, tc.wantLookup, got.calls)
			}
			if tc.wantDisposition != "" && !strings.Contains(got.outputs, "disposition="+tc.wantDisposition) {
				t.Fatalf("missing disposition %q in outputs %q", tc.wantDisposition, got.outputs)
			}
			if tc.wantNumber != "" && !strings.Contains(got.outputs, "number="+tc.wantNumber) {
				t.Fatalf("missing PR number %q in outputs %q", tc.wantNumber, got.outputs)
			}
		})
	}
}

func TestRequiredWorkflowTerminalRejectsAmbiguousPR(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	const twoOpen = `[{"number":7206,"state":"open","head":{"sha":"probe-head"}},{"number":7207,"state":"open","head":{"sha":"probe-head"}}]`
	got := runRequiredStepWithPRs(t, terminal, "pull_request", twoOpen, map[string]string{
		"PR_DISPOSITION": "ambiguous", "PENDING_OUTCOME": "success", "AGGREGATE_CODE": "",
	})
	if got.err == nil {
		t.Fatal("ambiguous active heads must fail closed")
	}
	if !strings.Contains(got.calls, "state=error") || strings.Contains(got.calls, "state=success") {
		t.Fatalf("ambiguous head must publish only error; calls:\n%s", got.calls)
	}
	assertRequiredStatusCall(t, got.calls, "error")
}

func TestRequiredWorkflowTerminalActivePR(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	const oneOpen = `[{"number":7206,"state":"open","head":{"sha":"probe-head"}}]`
	got := runRequiredStepWithPRs(t, terminal, "pull_request", oneOpen, map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7206", "AGGREGATE_CODE": "0",
	})
	if got.err != nil {
		t.Fatalf("active PR terminal step failed: %v\n%s", got.err, got.output)
	}
	assertRequiredStatusCall(t, got.calls, "success")
}

func TestRequiredWorkflowTerminalRejectsChangedPROwner(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	const otherOpen = `[{"number":7207,"state":"open","head":{"sha":"probe-head"}}]`
	got := runRequiredStepWithPRs(t, terminal, "pull_request", otherOpen, map[string]string{
		"PR_DISPOSITION": "active", "PR_NUMBER": "7206", "AGGREGATE_CODE": "0",
	})
	if got.err == nil {
		t.Fatal("ownership change must fail closed")
	}
	assertRequiredStatusCall(t, got.calls, "error")
}

func TestRequiredWorkflowTerminalSkipsPRClosedAfterPending(t *testing.T) {
	terminal := requiredStepNamed(t, repositoryRequiredWorkflowSteps(t), "Publish terminal required status")
	const mergedOnly = `[{"number":7206,"state":"closed","merged_at":"2026-09-27T19:45:14Z","head":{"sha":"probe-head"}}]`
	got := runRequiredStepWithPRs(t, terminal, "pull_request", mergedOnly, map[string]string{
		"PR_DISPOSITION": "active", "PENDING_OUTCOME": "success", "AGGREGATE_CODE": "10",
	})
	if strings.Contains(got.calls, "-X POST") {
		t.Fatalf("late close must not replace the merged head with a new verdict; calls:\n%s", got.calls)
	}
	if got.err != nil {
		t.Fatalf("late close should stop terminal publication cleanly: %v\n%s", got.err, got.output)
	}
}

type requiredStepRun struct {
	output  string
	outputs string
	calls   string
	err     error
}

func repositoryRequiredWorkflowSteps(t *testing.T) []requiredWorkflowStep {
	t.Helper()
	raw, err := os.ReadFile(repositoryRequiredGatesWorkflow(t))
	if err != nil {
		t.Fatal(err)
	}
	var workflow requiredWorkflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs["aggregate"].Steps
}

func requiredStepNamed(t *testing.T, steps []requiredWorkflowStep, name string) requiredWorkflowStep {
	t.Helper()
	for _, step := range steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("required workflow has no %q step", name)
	return requiredWorkflowStep{}
}

func assertRequiredStatusCall(t *testing.T, calls, state string) {
	t.Helper()
	var posts []string
	for _, call := range strings.Split(strings.TrimSpace(calls), "\n") {
		if strings.Contains(call, "api -X POST ") {
			posts = append(posts, call)
		}
	}
	if len(posts) != 1 {
		t.Fatalf("want exactly one status POST, got %d; calls:\n%s", len(posts), calls)
	}
	stateArgs := 0
	for _, field := range strings.Fields(posts[0]) {
		if strings.HasPrefix(field, "state=") {
			stateArgs++
			if field != "state="+state {
				t.Fatalf("unexpected status state %q; calls:\n%s", field, calls)
			}
		}
	}
	if stateArgs != 1 {
		t.Fatalf("want exactly one state argument, got %d; calls:\n%s", stateArgs, calls)
	}
	for _, fragment := range []string{
		"api -X POST repos/eshu-hq/required-gates-probe/statuses/probe-head",
		"context=required-gates-complete",
		"target_url=https://example.invalid/actions/runs/7206",
	} {
		if !strings.Contains(posts[0], fragment) {
			t.Fatalf("required status call lacks %q; calls:\n%s", fragment, calls)
		}
	}
}

func runRequiredStepWithPRs(t *testing.T, step requiredWorkflowStep, event, pulls string, extra map[string]string) requiredStepRun {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "pulls.json")
	if err := os.WriteFile(fixture, []byte(pulls), 0o600); err != nil {
		t.Fatal(err)
	}
	ghCalls := filepath.Join(dir, "gh-calls")
	outputs := filepath.Join(dir, "outputs")
	stub := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$ESHU_GH_CALLS"
if [[ "$1" == api && "$2" == repos/*/commits/*/pulls ]]; then
  if [[ "$(cat "$ESHU_PULLS_JSON")" == API_ERROR ]]; then
    exit 70
  fi
  cat "$ESHU_PULLS_JSON"
  exit 0
fi
if [[ "$1" == api && "$2" == -X && "$3" == POST ]]; then
  exit 0
fi
exit 79
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-e", "-c", step.Run) // #nosec G204 -- committed workflow shell is the test subject; gh is intercepted.
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + bin + ":/usr/bin:/bin",
		"HOME=" + dir,
		"GH_TOKEN=",
		"GITHUB_REPOSITORY=eshu-hq/required-gates-probe",
		"HEAD_SHA=probe-head",
		"TARGET_URL=https://example.invalid/actions/runs/7206",
		"RUN_EVENT=" + event,
		"GITHUB_OUTPUT=" + outputs,
		"ESHU_GH_CALLS=" + ghCalls,
		"ESHU_PULLS_JSON=" + fixture,
		"PENDING_OUTCOME=success",
		"AGGREGATE_CODE=0",
	}
	for key, value := range extra {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	combined, runErr := cmd.CombinedOutput()
	callBytes, err := os.ReadFile(ghCalls)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	outputBytes, err := os.ReadFile(outputs)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return requiredStepRun{output: string(combined), outputs: string(outputBytes), calls: string(callBytes), err: runErr}
}
