// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func assertGoCoreLinterWorkflow(t *testing.T, root string) {
	t.Helper()
	if problem := goCoreLinterProblem(readRepositoryFile(t, root, ".github/workflows/test.yml"), readRepositoryFile(t, root, ".github/actions/ci-go-core/action.yml")); problem != "" {
		t.Fatal(problem)
	}
}

func goCoreLinterProblem(workflowText, actionText string) string {
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string
				If   string
			}
		}
	}
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		return err.Error()
	}
	coreActionCalled := false
	for _, step := range workflow.Jobs["go-core"].Steps {
		if step.Uses != "./.github/actions/ci-go-core" {
			continue
		}
		// The cancellation guard is the only allowed step condition: the
		// lane job's own plan guard already selected the run, so a
		// selection guard here could only skip lint on a selected lane.
		coreActionCalled = step.If == "" || step.If == "${{ !cancelled() }}"
	}
	if !coreActionCalled {
		return "go-core must invoke the Go core composite action with at most a cancellation guard"
	}
	var action struct {
		Runs struct {
			Using string
			Steps []struct {
				Run              string
				If               string
				WorkingDirectory string `yaml:"working-directory"`
			}
		}
	}
	if err := yaml.Unmarshal([]byte(actionText), &action); err != nil {
		return err.Error()
	}
	lintRuns := false
	for _, step := range action.Runs.Steps {
		lintRuns = lintRuns || (step.Run == "golangci-lint run ./..." && step.WorkingDirectory == "go" &&
			step.If == "${{ !cancelled() && (inputs.mode != 'smoke' || contains(fromJSON(inputs.gates), 'go-lint')) }}")
	}
	if action.Runs.Using != "composite" || !lintRuns {
		return "Go core composite action must run golangci-lint"
	}
	return ""
}

func TestGoCoreLinterRejectsDisabledOrMisplacedStep(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readRepositoryFile(t, root, ".github/workflows/test.yml")
	action := readRepositoryFile(t, root, ".github/actions/ci-go-core/action.yml")
	condition := "${{ !cancelled() && (inputs.mode != 'smoke' || contains(fromJSON(inputs.gates), 'go-lint')) }}"
	for name, mutated := range map[string]string{
		"disabled":        strings.Replace(action, "if: "+condition, "if: ${{ false }}", 1),
		"wrong directory": strings.Replace(action, "working-directory: go\n      shell: bash\n      run: golangci-lint", "working-directory: .\n      shell: bash\n      run: golangci-lint", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if mutated == action {
				t.Fatal("seed did not alter the actual action")
			}
			if goCoreLinterProblem(workflow, mutated) == "" {
				t.Fatal("guard accepted a disabled or misplaced lint step")
			}
		})
	}
}

func TestGoCoreLinterRejectsDisabledCaller(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readRepositoryFile(t, root, ".github/workflows/test.yml")
	action := readRepositoryFile(t, root, ".github/actions/ci-go-core/action.yml")
	before := "name: Run selected Go core checks\n        if: ${{ !cancelled() }}"
	mutated := strings.Replace(workflow, before, "name: Run selected Go core checks\n        if: ${{ false }}", 1)
	if mutated == workflow {
		t.Fatal("seed did not alter the actual workflow caller")
	}
	if goCoreLinterProblem(mutated, action) == "" {
		t.Fatal("guard accepted a disabled composite caller")
	}
}
