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
	Needs           yaml.Node                   `yaml:"needs"`
	ContinueOnError string                      `yaml:"continue-on-error"`
	TimeoutMinutes  int                         `yaml:"timeout-minutes"`
	Env             map[string]string           `yaml:"env"`
	Services        map[string]readinessService `yaml:"services"`
	Steps           []readinessStep             `yaml:"steps"`
}

type readinessService struct {
	Credentials yaml.Node `yaml:"credentials"`
}

const readinessServiceCredentials = "        credentials:\n" +
	"          username: ${{ secrets.DOCKERHUB_USERNAME }}\n" +
	"          password: ${{ secrets.DOCKERHUB_TOKEN }}\n"

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
	if hermetic.TimeoutMinutes != 30 || live.TimeoutMinutes != 60 {
		return "hermetic and live jobs must retain independent 30/60-minute limits"
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
		configuration, ok := live.Services[service]
		if !ok {
			return "missing live service " + service
		}
		var credentials map[string]string
		if configuration.Credentials.Kind != yaml.MappingNode ||
			configuration.Credentials.Decode(&credentials) != nil || len(credentials) != 2 ||
			credentials["username"] != "${{ secrets.DOCKERHUB_USERNAME }}" ||
			credentials["password"] != "${{ secrets.DOCKERHUB_TOKEN }}" {
			return service + " must use username/password job-init credentials"
		}
	}
	if problem := checkReadinessDockerHubLogin(live); problem != "" {
		return problem
	}
	if problem := checkReadinessCleanup(live); problem != "" {
		return problem
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

func checkReadinessCleanup(job readinessJob) string {
	const stop = "bash scripts/ci/live-postgres-standby-fixture.sh stop '${{ github.run_id }}-${{ github.run_attempt }}'"
	count := 0
	for _, step := range job.Steps {
		if !strings.Contains(step.Run, "live-postgres-standby-fixture.sh stop") {
			continue
		}
		count++
		if strings.TrimSpace(step.Run) != stop || step.If != "always()" || step.ContinueOnError != "" {
			return "standby cleanup must run on every live-job outcome"
		}
	}
	if count != 1 {
		return "live job must stop its disposable standby exactly once"
	}
	return ""
}

func checkReadinessDockerHubLogin(job readinessJob) string {
	const enabled = "${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}"
	if job.Env["DOCKERHUB_LOGIN_ENABLED"] != enabled {
		return "Docker Hub login must require both secrets in the live job"
	}
	login, checkout, standby := -1, -1, -1
	for i, step := range job.Steps {
		if step.Uses == "actions/checkout@v5" {
			checkout = i
		}
		if strings.Contains(step.Run, "live-postgres-standby-fixture.sh start") {
			standby = i
		}
		if strings.HasPrefix(step.Uses, "docker/login-action@") {
			if login != -1 {
				return "Docker Hub login must run exactly once"
			}
			login = i
			if step.Name != "Log in to Docker Hub" ||
				step.Uses != "docker/login-action@v3" ||
				step.If != "env.DOCKERHUB_LOGIN_ENABLED == 'true'" ||
				step.ContinueOnError != "" || step.Run != "" ||
				step.With["username"] != "${{ secrets.DOCKERHUB_USERNAME }}" ||
				step.With["password"] != "${{ secrets.DOCKERHUB_TOKEN }}" {
				return "Docker Hub login must be gated, fail closed, and use both secrets"
			}
		}
	}
	if checkout < 0 || standby < 0 || login <= checkout || login >= standby {
		return "Docker Hub login must follow checkout and precede the standby pull"
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
		{"missing login flag", "      DOCKERHUB_LOGIN_ENABLED: ${{ secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != '' }}\n", ""},
		{"username-only login flag", "secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != ''", "secrets.DOCKERHUB_USERNAME != ''"},
		{"token-only login flag", "secrets.DOCKERHUB_USERNAME != '' && secrets.DOCKERHUB_TOKEN != ''", "secrets.DOCKERHUB_TOKEN != ''"},
		{"missing login", "uses: docker/login-action@v3", "uses: actions/cache@v4"},
		{"renamed login", "name: Log in to Docker Hub\n", "name: Unrelated login\n"},
		{"skip login", "if: env.DOCKERHUB_LOGIN_ENABLED == 'true'", "if: false"},
		{"waive login failure", "        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'", "        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n        continue-on-error: true"},
		{"short hermetic timeout", "timeout-minutes: 30", "timeout-minutes: 20"},
		{"changed live timeout", "timeout-minutes: 60", "timeout-minutes: 61"},
		{"missing standby cleanup", "run: bash scripts/ci/live-postgres-standby-fixture.sh stop", "run: true # bash scripts/ci/live-postgres-standby-fixture.sh stop"},
		{"skipped standby cleanup", "        if: always()", "        if: false"},
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
	workflowText := string(workflow)
	if strings.Count(workflowText, readinessServiceCredentials) != 2 {
		t.Fatal("expected one credential block for each live service")
	}
	for _, mutation := range []struct {
		name, replacement string
		last              bool
	}{
		{"missing postgres credentials", "", false},
		{"missing neo4j credentials", "", true},
		{"partial postgres credentials", "        credentials:\n          username: ${{ secrets.DOCKERHUB_USERNAME }}\n", false},
		{"partial neo4j credentials", "        credentials:\n          password: ${{ secrets.DOCKERHUB_TOKEN }}\n", true},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			index := strings.Index(workflowText, readinessServiceCredentials)
			if mutation.last {
				index = strings.LastIndex(workflowText, readinessServiceCredentials)
			}
			changed := workflowText[:index] + mutation.replacement + workflowText[index+len(readinessServiceCredentials):]
			if problem := checkReadinessSplit([]byte(changed), *gate); problem == "" {
				t.Fatal("seeded service credential violation passed")
			}
		})
	}
	t.Run("late login", func(t *testing.T) {
		const loginStep = "      - name: Log in to Docker Hub\n" +
			"        if: env.DOCKERHUB_LOGIN_ENABLED == 'true'\n" +
			"        uses: docker/login-action@v3\n" +
			"        with:\n" +
			"          username: ${{ secrets.DOCKERHUB_USERNAME }}\n" +
			"          password: ${{ secrets.DOCKERHUB_TOKEN }}\n\n"
		const standbyStep = "        run: bash scripts/ci/live-postgres-standby-fixture.sh start '${{ job.services.postgres.id }}' '${{ github.run_id }}-${{ github.run_attempt }}'\n"
		if !strings.Contains(string(workflow), loginStep) || !strings.Contains(string(workflow), standbyStep) {
			t.Fatal("mutation anchor missing")
		}
		changed := strings.Replace(string(workflow), loginStep, "", 1)
		changed = strings.Replace(changed, standbyStep, standbyStep+"\n"+loginStep, 1)
		if problem := checkReadinessSplit([]byte(changed), *gate); problem == "" {
			t.Fatal("late Docker Hub login passed")
		}
	})
}

func TestReadinessServiceCredentialsUseMainMapping(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed readinessWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"postgres", "neo4j"} {
		credentials := parsed.Jobs["live-postgres-readiness"].Services[service].Credentials
		var got map[string]string
		if credentials.Kind != yaml.MappingNode || credentials.Decode(&got) != nil ||
			got["username"] != "${{ secrets.DOCKERHUB_USERNAME }}" ||
			got["password"] != "${{ secrets.DOCKERHUB_TOKEN }}" || len(got) != 2 {
			t.Fatalf("%s service credentials must use main's username/password mapping", service)
		}
	}
}
