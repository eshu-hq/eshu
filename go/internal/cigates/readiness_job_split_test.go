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
	Needs           any                         `yaml:"needs"`
	Uses            string                      `yaml:"uses"`
	With            map[string]string           `yaml:"with"`
	Outputs         map[string]string           `yaml:"outputs"`
	ContinueOnError string                      `yaml:"continue-on-error"`
	TimeoutMinutes  int                         `yaml:"timeout-minutes"`
	Env             map[string]string           `yaml:"env"`
	Services        map[string]readinessService `yaml:"services"`
	Strategy        readinessStrategy           `yaml:"strategy"`
	Steps           []readinessStep             `yaml:"steps"`
}

type readinessStrategy struct {
	FailFast *bool     `yaml:"fail-fast"`
	Matrix   yaml.Node `yaml:"matrix"`
}

func readinessNeedsList(job readinessJob) []string {
	switch needs := job.Needs.(type) {
	case string:
		return []string{needs}
	case []any:
		var out []string
		for _, item := range needs {
			text, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

type readinessService struct {
	Image       string    `yaml:"image"`
	Credentials yaml.Node `yaml:"credentials"`
}

const (
	readinessCleanupSelection = "always()"
	readinessHermeticPlan     = "${{ github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'hermetic') }}"
	readinessShardPlan        = "${{ github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'live-postgres-readiness-shards') }}"
	readinessAlways           = "${{ always() }}"
	readinessAggregatePlan    = "${{ (github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'live-postgres-readiness')) && (always()) }}"

	readinessShardJob  = "live-postgres-readiness-shards"
	readinessShardName = "live-postgres-readiness (postgres18) shard (${{ matrix.shard }}/${{ matrix.total }})"
	// Interim 30-minute leg budget (#7864): test walls alone predicted 12.6m,
	// but real sharded runs showed the cold `go test` build of the giant
	// storage package costs ~9m on slow runners (leg 2/4: 821s suite for
	// 249s of tests). The heavy leg needs ~22m decomposed and timed out at
	// 17m, while a fast run finished every leg in 8-15m. 30m covers the slow
	// need with margin at half the old 60m cap; five green sharded runs plus
	// a build-cache investigation tighten it in a follow-up.
	readinessShardTimeout  = 30
	readinessShardRunner   = `bash scripts/run-live-postgres-readiness-tests.sh --shard "${{ matrix.shard }}/${{ matrix.total }}" --report readiness-shard-report.json`
	readinessShardContract = "python3 scripts/test-live-postgres-readiness-shards.py"
	// readinessSelfTestSeeds plants verifier and runner defects and requires
	// the hermetic self-test to catch each one (#8241).
	readinessSelfTestSeeds = "bash scripts/test-run-live-postgres-readiness-selftest-seeds.sh"
	readinessFixtureStart  = "bash scripts/ci/live-postgres-standby-fixture.sh start '${{ job.services.postgres.id }}' '${{ github.run_id }}-${{ github.run_attempt }}-${{ matrix.shard }}'"
	readinessFixtureStop   = "bash scripts/ci/live-postgres-standby-fixture.sh stop '${{ github.run_id }}-${{ github.run_attempt }}-${{ matrix.shard }}'"
	readinessAggregateJob  = "live-postgres-readiness"
	readinessArtifactPath  = "readiness-shard-reports"
)

type readinessStep struct {
	Name            string            `yaml:"name"`
	ID              string            `yaml:"id"`
	Run             string            `yaml:"run"`
	Uses            string            `yaml:"uses"`
	If              string            `yaml:"if"`
	ContinueOnError string            `yaml:"continue-on-error"`
	With            map[string]string `yaml:"with"`
	Env             map[string]string `yaml:"env"`
}

func checkReadinessSplit(workflow []byte, gate Gate, prPaths []string) string {
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
	if len(parsed.Jobs) != 4 {
		return "workflow must have plan, hermetic, shards, and aggregate jobs"
	}
	if _, ok := parsed.Jobs["queue-selection"]; ok {
		return "removed queue selector must stay out of the workflow"
	}
	for _, trigger := range []string{"workflow_call", "push"} {
		if _, ok := parsed.On[trigger]; !ok {
			return "missing workflow trigger " + trigger
		}
	}
	if _, ok := parsed.On["merge_group"]; ok {
		return "removed merge_group trigger must stay out of the workflow"
	}
	pushPaths := parsed.On["push"].Paths
	if _, directPR := parsed.On["pull_request"]; directPR ||
		len(prPaths) != len(pushPaths)+1 ||
		!slices.Equal(prPaths[:len(pushPaths)], pushPaths) ||
		prPaths[len(pushPaths)] != "specs/ci-pr-workflows.d/live-postgres-readiness.yml" ||
		!slices.Contains(prPaths, ".github/workflows/live-postgres-readiness.yml") ||
		!slices.Contains(prPaths, "go/internal/cigates/readiness_job_split_test.go") ||
		!slices.Contains(prPaths, "go/internal/cigates/readiness_matrix_test.go") ||
		!slices.Contains(prPaths, "scripts/lib/live_postgres_readiness_shards.py") ||
		!slices.Contains(prPaths, "scripts/test-live-postgres-readiness-shards.py") ||
		!slices.Contains(prPaths, "scripts/lib/live_postgres_readiness_events.py") {
		return "PR inventory and push paths must select the workflow, its regression, and the shard contract"
	}
	hermetic, ok := parsed.Jobs["hermetic"]
	if !ok || hermetic.Name != gate.CI.CheckNames[0] {
		return "missing hermetic check"
	}
	shards, ok := parsed.Jobs[readinessShardJob]
	if !ok || shards.Name != readinessShardName {
		return "missing sharded live legs"
	}
	aggregate, ok := parsed.Jobs[readinessAggregateJob]
	if !ok || aggregate.Name != gate.CI.CheckNames[1] {
		return "missing aggregate live check"
	}
	if hermetic.TimeoutMinutes != 30 || shards.TimeoutMinutes != readinessShardTimeout ||
		aggregate.TimeoutMinutes != 10 {
		return "hermetic, shard, and aggregate jobs must retain independent 30/30/10-minute limits"
	}
	if len(readinessNeedsList(hermetic)) != 0 ||
		hermetic.If != readinessHermeticPlan || hermetic.ContinueOnError != "" {
		return "hermetic must run on the trusted plan without skipping or waiving"
	}
	if !slices.Equal(readinessNeedsList(shards), []string{"readiness-plan"}) ||
		shards.If != readinessShardPlan || shards.ContinueOnError != "" {
		return "shards must wait for the plan without waiving failure"
	}
	if !slices.Equal(readinessNeedsList(aggregate),
		[]string{"readiness-plan", readinessShardJob}) ||
		aggregate.If != readinessAggregatePlan || aggregate.ContinueOnError != "" {
		return "aggregate must wait for the plan and every shard without waiving failure"
	}
	if shards.Strategy.FailFast == nil || *shards.Strategy.FailFast {
		return "a slow shard must not cancel its siblings before they attest"
	}
	if shards.Strategy.Matrix.Value != "${{ fromJSON(needs.readiness-plan.outputs.matrix) }}" {
		return "shards must use the plan's validated dynamic matrix"
	}
	if len(hermetic.Services) != 0 || len(shards.Services) != 2 || len(aggregate.Services) != 0 {
		return "only the shard legs must start both disposable backends"
	}
	for service, imagePrefix := range map[string]string{
		"postgres": "ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:",
		"neo4j":    "ghcr.io/eshu-hq/ci-neo4j-community@sha256:",
	} {
		configuration, ok := shards.Services[service]
		if !ok {
			return "missing shard service " + service
		}
		if !strings.HasPrefix(configuration.Image, imagePrefix) || configuration.Credentials.Kind != 0 {
			return service + " must use the current public GHCR mirror without Docker Hub credentials"
		}
	}
	if problem := checkReadinessNoDockerHubLogin(shards); problem != "" {
		return problem
	}
	if problem := checkReadinessNoDockerHubLogin(aggregate); problem != "" {
		return problem
	}
	if problem := checkReadinessCleanup(shards); problem != "" {
		return problem
	}
	counts := map[string]int{}
	for _, job := range map[string]readinessJob{"hermetic": hermetic, "shards": shards} {
		for _, step := range job.Steps {
			for _, script := range []string{
				"scripts/test-run-live-postgres-readiness-tests.sh",
				readinessSelfTestSeeds[len("bash "):],
				readinessShardContract,
				readinessShardRunner,
			} {
				if strings.Contains(step.Run, script) {
					if strings.TrimSpace(step.Run) != script &&
						strings.TrimSpace(step.Run) != "bash "+script {
						return "proof runner must be a standalone command"
					}
					if step.If != "" || step.ContinueOnError != "" {
						return "proof runner must run unconditionally without waiving failure"
					}
					counts[script]++
				}
			}
		}
	}
	if counts["scripts/test-run-live-postgres-readiness-tests.sh"] != 1 ||
		counts[readinessSelfTestSeeds[len("bash "):]] != 1 ||
		counts[readinessShardContract] != 1 || counts[readinessShardRunner] != 1 {
		return "hermetic and shard proof runners must each execute exactly once"
	}
	if !jobRuns(hermetic.Steps, "scripts/test-run-live-postgres-readiness-tests.sh") ||
		!jobRuns(hermetic.Steps, readinessSelfTestSeeds[len("bash "):]) ||
		!jobRuns(shards.Steps, readinessShardRunner[len("bash "):]) {
		return "proof runners must be in their designated jobs"
	}
	for _, step := range shards.Steps {
		if strings.TrimSpace(step.Run) == "bash scripts/run-live-postgres-readiness-tests.sh" {
			return "a shard leg must not run the unsharded suite"
		}
	}
	uploaded := false
	for _, step := range shards.Steps {
		if step.Uses != "actions/upload-artifact@v4" {
			continue
		}
		// Failure-only death-point evidence uploads (#8662) ride alongside
		// the report attestation without weakening it: the report step
		// below still fails the leg when the report is absent. The
		// carve-out names the exact diagnostic shape (path, name, gate)
		// so a future failure-gated report attestation cannot silently
		// escape the shape check.
		if step.With["path"] == ".readiness-debug/" &&
			strings.Contains(step.With["name"], "readiness-debug-") &&
			strings.Contains(step.If, "failure()") {
			continue
		}
		if step.With["path"] != "readiness-shard-report.json" ||
			step.With["if-no-files-found"] != "error" ||
			!strings.Contains(step.With["name"], "${{ matrix.shard }}") {
			return "shard legs must upload the current-attempt report or fail"
		}
		uploaded = true
	}
	if !uploaded {
		return "each shard leg must upload its coverage report for the aggregate"
	}
	if problem := checkReadinessAggregate(aggregate); problem != "" {
		return problem
	}
	if problem := checkReadinessHermeticSummary(hermetic); problem != "" {
		return problem
	}
	return ""
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
	fragment, err := os.ReadFile(filepath.Join(root, "specs", "ci-pr-workflows.d", "live-postgres-readiness.yml"))
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := ParsePRPlanFragment(fragment)
	if err != nil || len(definitions) != 1 {
		t.Fatalf("invalid readiness PR inventory: %v", err)
	}
	prPaths := definitions[0].Paths
	if problem := checkReadinessSplit(workflow, *gate, prPaths); problem != "" {
		t.Fatal(problem)
	}
	for _, mutation := range readinessRegistryMutations {
		t.Run(mutation.name, func(t *testing.T) {
			changed := *gate
			mutation.edit(&changed)
			if problem := checkReadinessSplit(workflow, changed, prPaths); problem == "" {
				t.Fatal("seeded registry violation passed")
			}
		})
	}
	for _, mutation := range readinessWorkflowMutations {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(string(workflow), mutation.old) {
				t.Fatalf("mutation anchor missing: %s", mutation.old)
			}
			changed := strings.Replace(string(workflow), mutation.old, mutation.replacement, 1)
			if problem := checkReadinessSplit([]byte(changed), *gate, prPaths); problem == "" {
				t.Fatal("seeded violation passed")
			}
		})
	}
	for _, mutation := range readinessServiceMutations {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(string(workflow), mutation.old) {
				t.Fatalf("mutation anchor missing: %s", mutation.old)
			}
			changed := strings.Replace(string(workflow), mutation.old, mutation.replacement, 1)
			if problem := checkReadinessSplit([]byte(changed), *gate, prPaths); problem == "" {
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
			"          ref: ${{ github.sha }}\n" +
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
			"          ref: ${{ github.sha }}\n"+
			loginStep+
			"      - name: Set up Go\n", 1)
		if problem := checkReadinessSplit([]byte(changed), *gate, prPaths); problem == "" {
			t.Fatal("added Docker Hub login passed")
		}
	})
	t.Run("added ghcr login passes", func(t *testing.T) {
		const ghcrLoginStep = "      - name: Log in to GHCR\n" +
			"        uses: docker/login-action@v3\n" +
			"        with:\n" +
			"          registry: ghcr.io\n" +
			"          username: ${{ github.actor }}\n" +
			"          password: ${{ secrets.GITHUB_TOKEN }}\n"
		const checkoutStep = "        uses: actions/checkout@v5\n" +
			"        with:\n" +
			"          persist-credentials: false\n" +
			"          ref: ${{ github.sha }}\n" +
			"\n" +
			"      - name: Set up Go\n"
		if !strings.Contains(string(workflow), checkoutStep) {
			t.Fatal("mutation anchor missing")
		}
		changed := strings.Replace(string(workflow), checkoutStep, "        uses: actions/checkout@v5\n"+
			"        with:\n"+
			"          persist-credentials: false\n"+
			"          ref: ${{ github.sha }}\n"+
			ghcrLoginStep+
			"      - name: Set up Go\n", 1)
		if problem := checkReadinessSplit([]byte(changed), *gate, prPaths); problem != "" {
			t.Fatalf("added GHCR login failed: %s", problem)
		}
	})
}
