// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRepositoryStaticContractWorkflowSkipsDuplicateGateCommand(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	root, err := workflowRoot(raw)
	if err != nil {
		t.Fatalf("parse static contract workflow: %v", err)
	}
	jobs, err := mappingValue(root, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	gate, err := mappingValue(jobs, "gate")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := mappingValue(gate, "steps")
	if err != nil {
		t.Fatal(err)
	}

	gateConsumers := 0
	for _, step := range steps.Content {
		run, runErr := mappingValue(step, "run")
		if runErr != nil || run.Value != "${{ matrix.gate }}" {
			continue
		}
		gateConsumers++
		condition, conditionErr := mappingValue(step, "if")
		if conditionErr != nil {
			t.Fatalf("Run gate step must skip a byte-identical test mirror: %v", conditionErr)
		}
		if condition.Value != "${{ matrix.run_gate }}" {
			t.Fatalf("Run gate condition = %q, want exact duplicate suppression", condition.Value)
		}
	}
	if gateConsumers != 1 {
		t.Fatalf("steps that run matrix.gate = %d, want exactly 1", gateConsumers)
	}

	builder := staticContractMatrixBuilder(t, raw)
	if !strings.Contains(builder, `if [[ "${test}" == "${gate}" ]]; then`) {
		t.Fatal("matrix builder must compare quoted test and gate commands case-sensitively in Bash")
	}
	if !strings.Contains(builder, `\"run_gate\":${run_gate}`) {
		t.Fatal("matrix builder must carry its exact comparison into each matrix row")
	}
}

func TestRepositoryStaticContractCommandComparisonIsCaseSensitive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	builder := staticContractMatrixBuilder(t, raw)
	firstCall := strings.Index(builder, "\nappend_gate ")
	if firstCall < 0 {
		t.Fatal("matrix builder has no append_gate invocation")
	}
	probe := strings.ReplaceAll(builder[:firstCall], "${{ github.event_name }}", "pull_request")
	probe += `
append_gate "true" "equal" "Equal" "echo FOO" "echo FOO"
append_gate "true" "case-only" "Case only" "echo FOO" "echo foo"
printf 'matrix={"include":[%s]}\n' "${matrix_items}" >>"${GITHUB_OUTPUT}"
`
	outputPath := filepath.Join(t.TempDir(), "output")
	cmd := exec.Command("bash", "-c", probe)
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath)
	if output, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("run matrix builder probe: %v\n%s", runErr, output)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read matrix builder output: %v", err)
	}
	line := strings.TrimSpace(string(output))
	line = strings.TrimPrefix(line, "matrix=")
	var matrix struct {
		Include []struct {
			Key     string `json:"key"`
			RunGate bool   `json:"run_gate"`
		} `json:"include"`
	}
	if err := json.Unmarshal([]byte(line), &matrix); err != nil {
		t.Fatalf("decode matrix builder output %q: %v", line, err)
	}
	if len(matrix.Include) != 2 {
		t.Fatalf("matrix rows = %d, want 2", len(matrix.Include))
	}
	if matrix.Include[0].Key != "equal" || matrix.Include[0].RunGate {
		t.Fatalf("equal command row = %+v, want run_gate=false", matrix.Include[0])
	}
	if matrix.Include[1].Key != "case-only" || !matrix.Include[1].RunGate {
		t.Fatalf("case-only command row = %+v, want run_gate=true", matrix.Include[1])
	}
}

func staticContractMatrixBuilder(t *testing.T, raw []byte) string {
	t.Helper()
	root, err := workflowRoot(raw)
	if err != nil {
		t.Fatalf("parse static contract workflow: %v", err)
	}
	jobs, err := mappingValue(root, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := mappingValue(jobs, "changes")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := mappingValue(changes, "steps")
	if err != nil {
		t.Fatal(err)
	}

	var builder string
	matches := 0
	for _, step := range steps.Content {
		id, idErr := mappingValue(step, "id")
		if idErr != nil || id.Value != "gate-matrix" {
			continue
		}
		matches++
		run, runErr := mappingValue(step, "run")
		if runErr != nil {
			t.Fatalf("gate-matrix step has no run block: %v", runErr)
		}
		builder = run.Value
	}
	if matches != 1 {
		t.Fatalf("steps with id gate-matrix = %d, want exactly 1", matches)
	}
	return builder
}

func TestRepositorySkillRoundtripHasOneHostedOwner(t *testing.T) {
	t.Parallel()

	workflowPaths, err := filepath.Glob(filepath.Join(repositoryRoot(t), ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	for _, script := range []string{
		"scripts/test-verify-skill-roundtrip.sh",
		"scripts/verify-skill-roundtrip.sh",
	} {
		var hosts []string
		for _, path := range workflowPaths {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read workflow %s: %v", path, readErr)
			}
			if workflowRunsScript(string(raw), script) {
				hosts = append(hosts, filepath.Base(path))
			}
		}
		slices.Sort(hosts)
		want := []string{"static-contract-gates.yml"}
		if !slices.Equal(hosts, want) {
			t.Fatalf("%s hosted owners = %q, want %q", script, hosts, want)
		}
	}
}
