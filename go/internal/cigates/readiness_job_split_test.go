// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type readinessWorkflow struct {
	On map[string]struct {
		Paths []string `yaml:"paths"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Name            string               `yaml:"name"`
		If              string               `yaml:"if"`
		Needs           yaml.Node            `yaml:"needs"`
		ContinueOnError string               `yaml:"continue-on-error"`
		Services        map[string]yaml.Node `yaml:"services"`
		Steps           []readinessStep      `yaml:"steps"`
	} `yaml:"jobs"`
}

type readinessStep struct {
	Run             string `yaml:"run"`
	If              string `yaml:"if"`
	ContinueOnError string `yaml:"continue-on-error"`
}

func checkReadinessSplit(workflow []byte, gate Gate) string {
	var parsed readinessWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		return err.Error()
	}
	if gate.Blocking {
		return "staged gate became globally blocking"
	}
	if !slices.Equal(gate.CI.CheckNames, []string{
		"live-postgres-readiness hermetic",
		"live-postgres-readiness (postgres18)",
	}) {
		return "registry must name both independent checks"
	}
	if len(parsed.Jobs) != 2 {
		return "workflow must have exactly two independent jobs"
	}
	for _, trigger := range []string{"merge_group", "pull_request", "push"} {
		if _, ok := parsed.On[trigger]; !ok {
			return "missing workflow trigger " + trigger
		}
	}
	if len(parsed.On["pull_request"].Paths) == 0 ||
		!slices.Equal(parsed.On["pull_request"].Paths, parsed.On["push"].Paths) ||
		!slices.Contains(parsed.On["pull_request"].Paths, ".github/workflows/live-postgres-readiness.yml") ||
		!slices.Contains(parsed.On["pull_request"].Paths, "go/internal/cigates/readiness_job_split_test.go") {
		return "pull and push paths must select the workflow and its own regression"
	}
	hermetic, ok := parsed.Jobs["hermetic"]
	if !ok || hermetic.Name != gate.CI.CheckNames[0] {
		return "missing hermetic check"
	}
	live, ok := parsed.Jobs["live-postgres-readiness"]
	if !ok || live.Name != gate.CI.CheckNames[1] {
		return "missing live check"
	}
	for name, job := range parsed.Jobs {
		if job.If != "" || job.ContinueOnError != "" || job.Needs.Kind != 0 {
			return name + " must not skip, waive, or depend on another job"
		}
	}
	if len(hermetic.Services) != 0 || len(live.Services) != 2 {
		return "only the live job must start both disposable backends"
	}
	for _, service := range []string{"postgres", "neo4j"} {
		if _, ok := live.Services[service]; !ok {
			return "missing live service " + service
		}
	}
	counts := map[string]int{}
	for _, job := range parsed.Jobs {
		for _, step := range job.Steps {
			for _, script := range []string{
				"scripts/test-run-live-postgres-readiness-tests.sh",
				"scripts/run-live-postgres-readiness-tests.sh",
			} {
				if strings.Contains(step.Run, script) {
					if strings.TrimSpace(step.Run) != "bash "+script {
						return "proof runner must be a standalone command"
					}
					if step.If != "" || step.ContinueOnError != "" {
						return "proof runner step must not skip or waive failure"
					}
					counts[script]++
				}
			}
		}
	}
	if counts["scripts/test-run-live-postgres-readiness-tests.sh"] != 1 ||
		counts["scripts/run-live-postgres-readiness-tests.sh"] != 1 {
		return "each proof runner must execute exactly once"
	}
	if !jobRuns(hermetic.Steps, "scripts/test-run-live-postgres-readiness-tests.sh") ||
		!jobRuns(live.Steps, "scripts/run-live-postgres-readiness-tests.sh") {
		return "proof runners must be in their designated jobs"
	}
	return ""
}

func jobRuns(steps []readinessStep, command string) bool {
	for _, step := range steps {
		if strings.TrimSpace(step.Run) == "bash "+command {
			return true
		}
	}
	return false
}

func TestReadinessWorkflowSplit(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var gate *Gate
	for i := range registry.Gates {
		if registry.Gates[i].ID == "live-postgres-readiness" {
			gate = &registry.Gates[i]
			break
		}
	}
	if gate == nil {
		t.Fatal("live-postgres-readiness registry row missing")
	}
	if problem := checkReadinessSplit(workflow, *gate); problem != "" {
		t.Fatal(problem)
	}
	for _, mutation := range []struct {
		name string
		edit func(*Gate)
	}{
		{"drop hermetic check", func(g *Gate) { g.CI.CheckNames = g.CI.CheckNames[1:] }},
		{"drop live check", func(g *Gate) { g.CI.CheckNames = g.CI.CheckNames[:1] }},
		{"promote staged gate", func(g *Gate) { g.Blocking = true }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			changed := *gate
			mutation.edit(&changed)
			if problem := checkReadinessSplit(workflow, changed); problem == "" {
				t.Fatal("seeded registry violation passed")
			}
		})
	}
	for _, mutation := range []struct {
		name, old, replacement string
	}{
		{"missing hermetic", "run: bash scripts/test-run-live-postgres-readiness-tests.sh", "run: true"},
		{"missing live", "run: bash scripts/run-live-postgres-readiness-tests.sh", "run: true"},
		{"skip hermetic", "  hermetic:\n", "  hermetic:\n    if: false\n"},
		{"waive live", "  live-postgres-readiness:\n", "  live-postgres-readiness:\n    continue-on-error: true\n"},
		{"compound runner", "run: bash scripts/run-live-postgres-readiness-tests.sh", "run: bash scripts/run-live-postgres-readiness-tests.sh || true"},
		{"missing push path", "      - 'go/internal/cigates/readiness_job_split_test.go'\n", ""},
		{"skip hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        if: false\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
		{"waive hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        continue-on-error: true\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
		{"skip live proof step", "        run: bash scripts/run-live-postgres-readiness-tests.sh", "        if: false\n        run: bash scripts/run-live-postgres-readiness-tests.sh"},
		{"waive live proof step", "        run: bash scripts/run-live-postgres-readiness-tests.sh", "        continue-on-error: true\n        run: bash scripts/run-live-postgres-readiness-tests.sh"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(string(workflow), mutation.old) {
				t.Fatalf("mutation anchor missing: %s", mutation.old)
			}
			changed := strings.Replace(string(workflow), mutation.old, mutation.replacement, 1)
			if problem := checkReadinessSplit([]byte(changed), *gate); problem == "" {
				t.Fatal("seeded violation passed")
			}
		})
	}
}
