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
	// Pull requests may skip dirgate only when the trusted plan omits it.
	dirgatePlanIf = "${{ github.event_name == 'merge_group' || (github.event_name != 'pull_request' || (needs.pr-plan.result == 'success' && contains(fromJSON(needs.pr-plan.outputs.jobs || '{}')['test.yml'], 'dirgate'))) }}"
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

// checkDirgateJob returns the reasons the dirgate job could skip a selected
// pull request or avoid running the real gate. The trusted plan is the only
// allowed pull-request selector; code-path conditions cannot bypass it.
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
	case []any:
		validNeeds := len(needs) == 2 && ((needs[0] == "queue-selection" && needs[1] == "pr-plan") || (needs[0] == "pr-plan" && needs[1] == "queue-selection"))
		if !validNeeds {
			problems = append(problems, "needs must contain exactly queue-selection and pr-plan")
		}
	default:
		problems = append(problems, "needs must contain exactly queue-selection and pr-plan")
	}
	if strings.TrimSpace(job.If) != dirgatePlanIf {
		problems = append(problems, "job-level if does not require the trusted pr-plan selection: "+job.If)
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

func TestWorkflowDirgateJobRunsWhenTrustedPlanSelectsIt(t *testing.T) {
	if problems := checkDirgateJob([]byte(liveWorkflow(t))); len(problems) > 0 {
		t.Fatalf("live test.yml: %s", strings.Join(problems, "; "))
	}
}

// TestWorkflowDirgateJobMutationsAreRejected seeds ways the selected job could
// be skipped or lose the real gate, starting from the live workflow.
func TestWorkflowDirgateJobMutationsAreRejected(t *testing.T) {
	live := liveWorkflow(t)
	start := strings.Index(live, "\n  "+dirgateJob+":\n")
	if start < 0 {
		t.Fatal("live test.yml has no dirgate job to mutate")
	}
	end := strings.Index(live[start+1:], "\n  verify-contracts:")
	job := live[start+1 : start+1+end]
	mutate := func(old, repl string) string {
		if !strings.Contains(job, old) {
			t.Fatalf("mutation target %q not in the live workflow", old)
		}
		return strings.Replace(live, job, strings.Replace(job, old, repl, 1), 1)
	}
	cases := []struct{ name, text string }{
		{"code-path job guard", mutate("if: "+dirgatePlanIf, "if: ${{ needs.changes.outputs.code == 'true' }}")},
		{"missing planner success", mutate("needs.pr-plan.result == 'success' && ", "")},
		{"missing planner selection", mutate("contains(fromJSON(needs.pr-plan.outputs.jobs || '{}')['test.yml'], 'dirgate')", "true")},
		{"renamed planner selection", mutate("['test.yml'], 'dirgate'", "['test.yml'], 'renamed'")},
		{"arbitrary pull-request skip", mutate("if: "+dirgatePlanIf, "if: ${{ github.event_name == 'merge_group' }}")},
		{"missing planner dependency", mutate("needs: [queue-selection, pr-plan]", "needs: queue-selection")},
		{"missing queue dependency", mutate("needs: [queue-selection, pr-plan]", "needs: pr-plan")},
		{"an extra dependency", mutate("needs: [queue-selection, pr-plan]", "needs: [queue-selection, pr-plan, changes]")},
		{"renamed merge-queue guard", mutate("contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'dirgate')", "contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'renamed')")},
		{"missing job", mutate("  "+dirgateJob+":\n", "  other-job:\n")},
		{"dropped gate step", mutate("run: bash scripts/verify-dirgate.sh --ci", "run: echo skipped")},
		{"commented-out gate", mutate("run: bash scripts/verify-dirgate.sh --ci", "# run: bash scripts/verify-dirgate.sh --ci\n        run: true")},
		{"dropped unit tests", mutate("go test ./cmd/dirgate/ -count=1", "true")},
		{"gate result swallowed with || true", mutate("run: bash scripts/verify-dirgate.sh --ci", "run: bash scripts/verify-dirgate.sh --ci || true")},
		{"gate result swallowed with a trailing command", mutate("run: bash scripts/verify-dirgate.sh --ci", "run: bash scripts/verify-dirgate.sh --ci; true")},
		{"job-level continue-on-error", mutate("    needs: [queue-selection, pr-plan]\n", "    needs: [queue-selection, pr-plan]\n    continue-on-error: true\n")},
		{"step-level continue-on-error on the gate", mutate("      - name: Verify dirgate (derived ratchet)\n", "      - name: Verify dirgate (derived ratchet)\n        continue-on-error: true\n")},
		{"step-level continue-on-error on the unit tests", mutate("      - name: Test dirgate\n", "      - name: Test dirgate\n        continue-on-error: true\n")},
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
