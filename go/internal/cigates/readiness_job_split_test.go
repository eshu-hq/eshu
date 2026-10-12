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
	Jobs map[string]readinessJob `yaml:"jobs"`
}

type readinessJob struct {
	Name            string                      `yaml:"name"`
	If              string                      `yaml:"if"`
	Needs           string                      `yaml:"needs"`
	Uses            string                      `yaml:"uses"`
	With            map[string]string           `yaml:"with"`
	ContinueOnError string                      `yaml:"continue-on-error"`
	TimeoutMinutes  int                         `yaml:"timeout-minutes"`
	Env             map[string]string           `yaml:"env"`
	Services        map[string]readinessService `yaml:"services"`
	Steps           []readinessStep             `yaml:"steps"`
}

type readinessService struct {
	Image       string    `yaml:"image"`
	Credentials yaml.Node `yaml:"credentials"`
}

const (
	readinessQueueSelection     = "${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'live-postgres-readiness (postgres18)')) }}"
	readinessUnemittedSelection = "${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'live-postgres-readiness hermetic')) }}"
	readinessCleanupSelection   = "${{ (github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'live-postgres-readiness (postgres18)'))) && (always()) }}"
)

type readinessStep struct {
	Name            string            `yaml:"name"`
	Run             string            `yaml:"run"`
	Uses            string            `yaml:"uses"`
	If              string            `yaml:"if"`
	ContinueOnError string            `yaml:"continue-on-error"`
	With            map[string]string `yaml:"with"`
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
	if len(parsed.Jobs) != 3 {
		return "workflow must have queue selection and two proof jobs"
	}
	selector := parsed.Jobs["queue-selection"]
	if selector.Uses != "./.github/workflows/queue-selection.yml" ||
		selector.With["workflow"] != "live-postgres-readiness.yml" {
		return "workflow must use the shared queue selector"
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
	if hermetic.TimeoutMinutes != 30 || live.TimeoutMinutes != 60 {
		return "hermetic and live jobs must retain independent 30/60-minute limits"
	}
	for name, job := range map[string]readinessJob{"hermetic": hermetic, "live": live} {
		if job.If != "" || job.ContinueOnError != "" || job.Needs != "queue-selection" {
			return name + " must wait for queue selection without skipping or waiving"
		}
	}
	if len(hermetic.Services) != 0 || len(live.Services) != 2 {
		return "only the live job must start both disposable backends"
	}
	for service, imagePrefix := range map[string]string{
		"postgres": "ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:",
		"neo4j":    "ghcr.io/eshu-hq/ci-neo4j-community@sha256:",
	} {
		configuration, ok := live.Services[service]
		if !ok {
			return "missing live service " + service
		}
		if !strings.HasPrefix(configuration.Image, imagePrefix) || configuration.Credentials.Kind != 0 {
			return service + " must use the current public GHCR mirror without Docker Hub credentials"
		}
	}
	if problem := checkReadinessNoDockerHubLogin(live); problem != "" {
		return problem
	}
	if problem := checkReadinessCleanup(live); problem != "" {
		return problem
	}
	counts := map[string]int{}
	for _, job := range map[string]readinessJob{"hermetic": hermetic, "live": live} {
		for _, step := range job.Steps {
			for _, script := range []string{
				"scripts/test-run-live-postgres-readiness-tests.sh",
				"scripts/run-live-postgres-readiness-tests.sh",
			} {
				if strings.Contains(step.Run, script) {
					if strings.TrimSpace(step.Run) != "bash "+script {
						return "proof runner must be a standalone command"
					}
					if step.If != readinessQueueSelection || step.ContinueOnError != "" {
						return "proof runner must use the registry queue selection without waiving failure"
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

func checkReadinessCleanup(job readinessJob) string {
	const stop = "bash scripts/ci/live-postgres-standby-fixture.sh stop '${{ github.run_id }}-${{ github.run_attempt }}'"
	count := 0
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, "live-postgres-standby-fixture.sh stop") {
			continue
		}
		count++
		if strings.TrimSpace(step.Run) != stop || step.If != readinessCleanupSelection || step.ContinueOnError != "" {
			return "standby cleanup must run on every live-job outcome"
		}
	}
	if count != 1 {
		return "live job must stop its disposable standby exactly once"
	}
	return ""
}

func checkReadinessNoDockerHubLogin(job readinessJob) string {
	if _, ok := job.Env["DOCKERHUB_LOGIN_ENABLED"]; ok {
		return "live job must not carry a Docker Hub login flag: its services use public GHCR mirrors"
	}
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "docker/login-action@") {
			return "live job must not log in to Docker Hub: its services use public GHCR mirrors"
		}
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
	t.Run("merge queue selects both proof jobs via the shared ci.job token", func(t *testing.T) {
		selection, err := SelectQueueWorkflow(
			[]*Registry{registry},
			[]string{"go/internal/cigates/readiness_job_split_test.go"},
			false,
			"live-postgres-readiness.yml",
			"",
		)
		if err != nil {
			t.Fatal(err)
		}
		if !selection.Selected || !slices.Equal(selection.Jobs, []string{gate.CI.Job}) {
			t.Fatalf("unexpected queue jobs: %+v", selection)
		}
		var parsed readinessWorkflow
		if err := yaml.Unmarshal(workflow, &parsed); err != nil {
			t.Fatal(err)
		}
		const conditionPrefix = "${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), '"
		const conditionSuffix = "')) }}"
		for _, jobName := range []string{"hermetic", "live-postgres-readiness"} {
			job := parsed.Jobs[jobName]
			for _, step := range job.Steps {
				if step.Uses != "actions/checkout@v5" &&
					!strings.Contains(step.Run, "scripts/test-run-live-postgres-readiness-tests.sh") &&
					!strings.Contains(step.Run, "scripts/run-live-postgres-readiness-tests.sh") {
					continue
				}
				queueJob, ok := strings.CutPrefix(step.If, conditionPrefix)
				if !ok {
					t.Errorf("%s step %q has an unsupported queue condition", jobName, step.Name)
					continue
				}
				queueJob, ok = strings.CutSuffix(queueJob, conditionSuffix)
				if !ok {
					t.Errorf("%s step %q has an unsupported queue condition", jobName, step.Name)
					continue
				}
				if !slices.Contains(selection.Jobs, queueJob) {
					t.Errorf("%s step %q skips selected queue job %q", jobName, step.Name, gate.CI.Job)
				}
			}
		}
	})
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
		{"missing queue selector", "    uses: ./.github/workflows/queue-selection.yml", "    uses: actions/checkout@v5"},
		{"wrong hermetic selection", "        if: " + readinessQueueSelection + "\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        if: " + readinessUnemittedSelection + "\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
		{"wrong live selection", "      - name: Run live readiness proofs\n        if: " + readinessQueueSelection, "      - name: Run live readiness proofs\n        if: " + readinessUnemittedSelection},
		{"skip hermetic", "  hermetic:\n", "  hermetic:\n    if: false\n"},
		{"waive live", "  live-postgres-readiness:\n", "  live-postgres-readiness:\n    continue-on-error: true\n"},
		{"compound runner", "run: bash scripts/run-live-postgres-readiness-tests.sh", "run: bash scripts/run-live-postgres-readiness-tests.sh || true"},
		{"missing push path", "      - 'go/internal/cigates/readiness_job_split_test.go'\n", ""},
		{"skip hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        if: false\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
		{"waive hermetic proof step", "        run: bash scripts/test-run-live-postgres-readiness-tests.sh", "        continue-on-error: true\n        run: bash scripts/test-run-live-postgres-readiness-tests.sh"},
		{"skip live proof step", "        run: bash scripts/run-live-postgres-readiness-tests.sh", "        if: false\n        run: bash scripts/run-live-postgres-readiness-tests.sh"},
		{"waive live proof step", "        run: bash scripts/run-live-postgres-readiness-tests.sh", "        continue-on-error: true\n        run: bash scripts/run-live-postgres-readiness-tests.sh"},
		{"added login flag", "    timeout-minutes: 60\n    services:", "    timeout-minutes: 60\n    env:\n      DOCKERHUB_LOGIN_ENABLED: ${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}\n    services:"},
		{"added login step", "        with:\n          persist-credentials: false\n\n      - name: Set up Go", "        with:\n          persist-credentials: false\n      - name: Log in to Docker Hub\n        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n        uses: docker/login-action@v3\n        with:\n          username: ${{ secrets.DOCKERHUB_USERNAME }}\n          password: ${{ secrets.DOCKERHUB_TOKEN }}\n      - name: Set up Go"},
		{"short hermetic timeout", "timeout-minutes: 30", "timeout-minutes: 20"},
		{"changed live timeout", "timeout-minutes: 60", "timeout-minutes: 61"},
		{"missing standby cleanup", "run: bash scripts/ci/live-postgres-standby-fixture.sh stop", "run: true # bash scripts/ci/live-postgres-standby-fixture.sh stop"},
		{"skipped standby cleanup", "        if: " + readinessCleanupSelection, "        if: false"},
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
	for _, mutation := range []struct {
		name, old, replacement string
	}{
		{"postgres mirror changed", "image: ghcr.io/eshu-hq/ci-postgres-bookworm@", "image: docker.io/library/postgres@"},
		{"neo4j mirror changed", "image: ghcr.io/eshu-hq/ci-neo4j-community@", "image: docker.io/library/neo4j@"},
		{"postgres credentials added", "      postgres:\n        image:", "      postgres:\n        credentials: ${{ secrets.DOCKERHUB_TOKEN }}\n        image:"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(string(workflow), mutation.old) {
				t.Fatalf("mutation anchor missing: %s", mutation.old)
			}
			changed := strings.Replace(string(workflow), mutation.old, mutation.replacement, 1)
			if problem := checkReadinessSplit([]byte(changed), *gate); problem == "" {
				t.Fatal("seeded service image violation passed")
			}
		})
	}
	t.Run("added login", func(t *testing.T) {
		const loginStep = "      - name: Log in to Docker Hub\n" +
			"        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n" +
			"        uses: docker/login-action@v3\n" +
			"        with:\n" +
			"          username: ${{ secrets.DOCKERHUB_USERNAME }}\n" +
			"          password: ${{ secrets.DOCKERHUB_TOKEN }}\n"
		const checkoutStep = "        uses: actions/checkout@v5\n" +
			"        with:\n" +
			"          persist-credentials: false\n" +
			"\n" +
			"      - name: Set up Go\n"
		if !strings.Contains(string(workflow), checkoutStep) {
			t.Fatal("mutation anchor missing")
		}
		if strings.Contains(string(workflow), loginStep) {
			t.Fatal("Docker Hub login still present")
		}
		changed := strings.Replace(string(workflow), checkoutStep, "        uses: actions/checkout@v5\n"+
			"        with:\n"+
			"          persist-credentials: false\n"+
			loginStep+
			"      - name: Set up Go\n", 1)
		if problem := checkReadinessSplit([]byte(changed), *gate); problem == "" {
			t.Fatal("added Docker Hub login passed")
		}
	})
}
