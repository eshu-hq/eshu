// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	workflowPath = "../../../.github/workflows/test.yml"
	dirgateJob   = "dirgate"
)

// workflowFile is the slice of a GitHub Actions workflow the check reads.
type workflowFile struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs           any            `yaml:"needs"`
	If              string         `yaml:"if"`
	ContinueOnError any            `yaml:"continue-on-error"`
	Steps           []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name            string `yaml:"name"`
	If              string `yaml:"if"`
	Run             string `yaml:"run"`
	ContinueOnError any    `yaml:"continue-on-error"`
}

// swallowsFailure reports whether a continue-on-error value could let a failed
// job or step pass: anything but an absent key or a literal false.
func swallowsFailure(v any) bool {
	switch b := v.(type) {
	case nil:
		return false
	case bool:
		return b
	default:
		return true // an expression or any other value is not provably false
	}
}

// normalizeIf strips the expression wrapper, spaces and parentheses so two
// spellings of the same condition compare equal.
func normalizeIf(s string) string {
	s = strings.NewReplacer("${{", "", "}}", "", " ", "", "(", "", ")", "", "\"", "'").Replace(s)
	return strings.TrimSpace(s)
}

// queueOnlyIf reports whether a condition schedules the step only through the
// merge-queue selection: it always runs on a pull request, and on a merge group
// only when queue-select chose the job.
func queueOnlyIf(cond, job string) bool {
	n := normalizeIf(cond)
	event := "github.event_name!='merge_group'"
	selected := "containsfromJSONneeds.queue-selection.outputs.jobs||'[]','" + job + "'"
	return n == event+"||"+selected || n == selected+"||"+event
}

// checkDirgateJob returns the reasons the dirgate job would be skipped, or would
// not run the real gate, on some pull request. The job must schedule only through
// the merge-queue selection. A code-path guard like needs.changes.outputs.code
// skips a docs-only pull request, so a stray trailer there would never be
// checked and would inflate W for good.
func checkDirgateJob(content []byte) []string {
	var wf workflowFile
	if err := yaml.Unmarshal(content, &wf); err != nil {
		return []string{"workflow does not parse: " + err.Error()}
	}
	job, ok := wf.Jobs[dirgateJob]
	if !ok {
		return []string{"missing the " + dirgateJob + " job"}
	}
	var problems []string
	switch needs := job.Needs.(type) {
	case string:
		if needs != "queue-selection" {
			problems = append(problems, "needs "+needs+" instead of only queue-selection")
		}
	case []any:
		if len(needs) != 1 || needs[0] != "queue-selection" {
			problems = append(problems, "needs more than queue-selection")
		}
	default:
		problems = append(problems, "needs is not exactly queue-selection")
	}
	if job.If != "" {
		problems = append(problems, "job-level if skips pull requests: "+job.If)
	}
	if swallowsFailure(job.ContinueOnError) {
		problems = append(problems, "job-level continue-on-error lets a failed gate pass")
	}
	var ran, tested bool
	for _, step := range job.Steps {
		selector := step.Name == "Require queue selection to succeed"
		if step.If != "" && !queueOnlyIf(step.If, dirgateJob) &&
			(!selector || normalizeIf(step.If) != "always&&github.event_name=='merge_group'") {
			problems = append(problems, "step "+step.Name+" has a non-queue guard: "+step.If)
		}
		if swallowsFailure(step.ContinueOnError) {
			problems = append(problems, "step "+step.Name+" has continue-on-error, so a failure would pass")
		}
		if selector {
			continue
		}
		// The gate command must stand alone: a trailing `|| true` or `; true`
		// turns a RED gate into a green step.
		if strings.TrimSpace(step.Run) == "bash scripts/verify-dirgate.sh --ci" {
			ran = true
		}
		if strings.TrimSpace(step.Run) == "go test ./cmd/dirgate/ -count=1" {
			tested = true
		}
	}
	if !ran {
		problems = append(problems, "no step runs scripts/verify-dirgate.sh --ci")
	}
	if !tested {
		problems = append(problems, "no step runs go test ./cmd/dirgate/")
	}
	return problems
}

func liveWorkflow(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWorkflowDirgateJobRunsOnEveryPullRequest(t *testing.T) {
	if problems := checkDirgateJob([]byte(liveWorkflow(t))); len(problems) > 0 {
		t.Fatalf("live test.yml: %s", strings.Join(problems, "; "))
	}
}

// TestWorkflowDirgateJobMutationsAreRejected seeds each way the job could be
// skipped on a docs-only change, or lose the real gate, from the live text.
func TestWorkflowDirgateJobMutationsAreRejected(t *testing.T) {
	live := liveWorkflow(t)
	start := strings.Index(live, "\n  "+dirgateJob+":\n")
	if start < 0 {
		t.Fatal("live test.yml has no dirgate job to mutate")
	}
	end := strings.Index(live[start+1:], "\n  verify-contracts:")
	job := live[start+1 : start+1+end]
	mutate := func(old, repl string) string {
		if !strings.Contains(live, old) {
			t.Fatalf("mutation target %q not in the live workflow", old)
		}
		return strings.Replace(live, old, repl, 1)
	}
	cases := []struct{ name, text string }{
		{"code-path job guard (skips a docs-only PR)", mutate("  "+dirgateJob+":\n    needs: queue-selection\n", "  "+dirgateJob+":\n    needs: queue-selection\n    if: ${{ needs.changes.outputs.code == 'true' }}\n")},
		{"a second needs entry", mutate("  "+dirgateJob+":\n    needs: queue-selection\n", "  "+dirgateJob+":\n    needs: [queue-selection, changes]\n")},
		{"renamed merge-queue guard", strings.ReplaceAll(live, "'"+dirgateJob+"'", "'renamed'")},
		{"missing job", mutate("\n  "+dirgateJob+":\n", "\n  other-job:\n")},
		{"dropped gate step", strings.Replace(live, "run: bash scripts/verify-dirgate.sh --ci", "run: echo skipped", 1)},
		{"commented-out gate", strings.Replace(live, "run: bash scripts/verify-dirgate.sh --ci", "# run: bash scripts/verify-dirgate.sh --ci\n        run: true", 1)},
		{"dropped unit tests", strings.Replace(live, "go test ./cmd/dirgate/ -count=1", "true", 1)},
		{"gate result swallowed with || true", strings.Replace(live, "run: bash scripts/verify-dirgate.sh --ci", "run: bash scripts/verify-dirgate.sh --ci || true", 1)},
		{"gate result swallowed with a trailing command", strings.Replace(live, "run: bash scripts/verify-dirgate.sh --ci", "run: bash scripts/verify-dirgate.sh --ci; true", 1)},
		{"job-level continue-on-error", mutate("  "+dirgateJob+":\n    needs: queue-selection\n", "  "+dirgateJob+":\n    needs: queue-selection\n    continue-on-error: true\n")},
		{"step-level continue-on-error on the gate", strings.Replace(live, "      - name: Verify dirgate (derived ratchet)\n", "      - name: Verify dirgate (derived ratchet)\n        continue-on-error: true\n", 1)},
		{"step-level continue-on-error on the unit tests", strings.Replace(live, "      - name: Test dirgate\n", "      - name: Test dirgate\n        continue-on-error: true\n", 1)},
	}
	if job == "" {
		t.Fatal("empty dirgate job text")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if problems := checkDirgateJob([]byte(tc.text)); len(problems) == 0 {
				t.Fatal("mutation was accepted")
			}
		})
	}
}
