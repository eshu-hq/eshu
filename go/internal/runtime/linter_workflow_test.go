// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func assertGoCoreLinterWorkflow(t *testing.T, root string) {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct{ Uses string }
		}
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, root, ".github/workflows/test.yml")), &workflow); err != nil {
		t.Fatal(err)
	}
	coreActionCalled := false
	for _, step := range workflow.Jobs["go-core"].Steps {
		coreActionCalled = coreActionCalled || step.Uses == "./.github/actions/ci-go-core"
	}
	if !coreActionCalled {
		t.Fatal("go-core must invoke the Go core composite action")
	}
	var action struct {
		Runs struct {
			Using string
			Steps []struct{ Run string }
		}
	}
	if err := yaml.Unmarshal([]byte(readRepositoryFile(t, root, ".github/actions/ci-go-core/action.yml")), &action); err != nil {
		t.Fatal(err)
	}
	lintRuns := false
	for _, step := range action.Runs.Steps {
		lintRuns = lintRuns || step.Run == "golangci-lint run ./..."
	}
	if action.Runs.Using != "composite" || !lintRuns {
		t.Fatal("Go core composite action must run golangci-lint")
	}
}
