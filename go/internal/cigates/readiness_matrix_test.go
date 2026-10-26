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

	"gopkg.in/yaml.v3"
)

func TestReadinessDynamicMatrixContract(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow readinessWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	plan, ok := workflow.Jobs["readiness-plan"]
	if !ok {
		t.Fatal("readiness-plan job missing")
	}
	if plan.Name != "live-postgres-readiness plan" ||
		!slices.Equal(readinessNeedsList(plan), []string{"queue-selection"}) ||
		!strings.Contains(plan.If, "contains(fromJSON(inputs.plan_jobs || '[]'), 'readiness-plan')") ||
		readinessSelectedJob(plan.If) != "live-postgres-readiness (postgres18)" ||
		plan.ContinueOnError != "" || len(plan.Services) != 0 || plan.TimeoutMinutes != 10 {
		t.Fatal("readiness plan must wait for selection and fail closed without services")
	}
	if plan.Outputs["matrix"] != "${{ steps.matrix.outputs.matrix }}" {
		t.Fatalf("matrix output not wired to producer: %q", plan.Outputs["matrix"])
	}
	checkout, setup, producer := 0, 0, 0
	var producerRun string
	for _, step := range plan.Steps {
		if step.Uses == "actions/checkout@v5" {
			checkout++
			if step.With["persist-credentials"] != "false" || step.With["ref"] != "${{ github.sha }}" {
				t.Fatal("plan checkout must use this run's commit without persisted credentials")
			}
		}
		if step.Uses == "actions/setup-python@v5" {
			setup++
			if step.With["python-version"] != "3.12" {
				t.Fatal("plan Python version is not pinned")
			}
		}
		if step.Name == "Produce readiness matrix" {
			producer++
			producerRun = step.Run
			if step.ID != "matrix" || step.ContinueOnError != "" ||
				!strings.Contains(step.Run, "matrix=$(python3 scripts/lib/live_postgres_readiness_shards.py matrix)") ||
				!strings.Contains(step.Run, "printf 'matrix=%s\\n' \"$matrix\" >> \"$GITHUB_OUTPUT\"") ||
				!strings.Contains(step.Run, "printf '%s' \"$matrix\" | python3 -m json.tool > /dev/null") ||
				strings.Contains(step.Run, "echo \"$(python") {
				t.Fatalf("matrix producer can hide loader failures: %q", step.Run)
			}
		}
	}
	if checkout != 1 || setup != 1 || producer != 1 {
		t.Fatalf("plan checkout=%d setup=%d producer=%d", checkout, setup, producer)
	}
	checkReadinessMatrixScript(t, root, producerRun)
	shards := workflow.Jobs[readinessShardJob]
	if !slices.Equal(readinessNeedsList(shards), []string{"queue-selection", "readiness-plan"}) ||
		shards.Name != "live-postgres-readiness (postgres18) shard (${{ matrix.shard }}/${{ matrix.total }})" ||
		shards.Strategy.Matrix.Value != "${{ fromJSON(needs.readiness-plan.outputs.matrix) }}" {
		t.Fatal("shard matrix must consume the plan count and preserve dynamic check names")
	}
	shardCheckout := false
	for _, step := range shards.Steps {
		if step.Uses == "actions/checkout@v5" {
			shardCheckout = step.With["ref"] == "${{ github.sha }}" && step.With["persist-credentials"] == "false"
		}
	}
	if !shardCheckout {
		t.Fatal("shard checkout must use the same commit as plan")
	}
	if !jobRuns(shards.Steps, `scripts/run-live-postgres-readiness-tests.sh --shard "${{ matrix.shard }}/${{ matrix.total }}" --report readiness-shard-report.json`) {
		t.Fatal("runner does not use the dynamic shard total")
	}
	aggregate := workflow.Jobs[readinessAggregateJob]
	if !slices.Equal(readinessNeedsList(aggregate), []string{"queue-selection", "readiness-plan", readinessShardJob}) {
		t.Fatal("aggregate must depend on plan and all shards")
	}
	guard := aggregate.Steps[0]
	if guard.Env["PLAN_RESULT"] != "${{ needs.readiness-plan.result }}" ||
		!strings.Contains(guard.Run, `test "$PLAN_RESULT" = success`) {
		t.Fatal("selected aggregate must fail when plan fails or is skipped")
	}
}

// checkReadinessMatrixScript runs the actual workflow command under Bash's
// failure mode, including malformed and empty loader output controls.
func checkReadinessMatrixScript(t *testing.T, root, script string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, loader string
		wantSuccess  bool
	}{
		{"default model", "", true},
		{"loader failure", "exit 7", false},
		{"empty matrix", "exit 0", false},
		{"invalid JSON", "printf '{bad'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			outputPath := filepath.Join(directory, "output")
			path := os.Getenv("PATH")
			if tc.loader != "" {
				fake := "#!/bin/sh\nif [ \"$1\" = -m ]; then exec \"$REAL_PYTHON\" \"$@\"; fi\n" + tc.loader + "\n"
				if err := os.WriteFile(filepath.Join(directory, "python3"), []byte(fake), 0o755); err != nil {
					t.Fatal(err)
				}
				path = directory + string(os.PathListSeparator) + path
			}
			command := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+path, "REAL_PYTHON="+python, "GITHUB_OUTPUT="+outputPath)
			combined, err := command.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("script err=%v output=%s", err, combined)
			}
			content, readErr := os.ReadFile(outputPath)
			if !tc.wantSuccess {
				if readErr == nil && len(content) != 0 {
					t.Fatalf("failed matrix wrote output: %q", content)
				}
				return
			}
			if readErr != nil || !strings.HasPrefix(string(content), "matrix=") {
				t.Fatalf("missing matrix output: %s %v", content, readErr)
			}
			var matrix struct {
				Include []struct {
					Shard int `json:"shard"`
					Total int `json:"total"`
				} `json:"include"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(string(content)), "matrix=")), &matrix); err != nil {
				t.Fatal(err)
			}
			count := len(matrix.Include)
			if count != 4 && count != 5 {
				t.Fatalf("matrix has %d shards", count)
			}
			for index, shard := range matrix.Include {
				if shard.Shard != index+1 || shard.Total != count {
					t.Fatalf("inconsistent matrix: %+v", matrix.Include)
				}
			}
		})
	}
}
