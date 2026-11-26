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
	On   map[string]any          `yaml:"on"`
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
	Options     string    `yaml:"options"`
}

const (
	readinessCleanupSelection = "always()"
	readinessAlways           = "${{ always() }}"

	readinessShardJob      = "live-postgres-readiness-shards"
	readinessShardName     = "live-postgres-readiness (postgres18) shard (${{ matrix.shard }}/${{ matrix.total }})"
	readinessShardTimeout  = 30
	readinessShardRunner   = `bash scripts/run-live-postgres-readiness-tests.sh --shard "${{ matrix.shard }}/${{ matrix.total }}" --report readiness-shard-report.json`
	readinessShardContract = "python3 scripts/test-live-postgres-readiness-shards.py"
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

func checkReadinessSplit(workflow []byte) string {
	var parsed readinessWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		return err.Error()
	}
	if len(parsed.Jobs) != 4 {
		return "workflow must have plan, hermetic, shards, and aggregate jobs"
	}
	for _, trigger := range []string{"schedule", "workflow_dispatch"} {
		if _, ok := parsed.On[trigger]; !ok {
			return "missing scheduled workflow trigger " + trigger
		}
	}
	for _, forbidden := range []string{"pull_request", "workflow_call", "push", "merge_group"} {
		if _, ok := parsed.On[forbidden]; ok {
			return "scheduled-only readiness workflow must not declare trigger " + forbidden
		}
	}
	hermetic, ok := parsed.Jobs["hermetic"]
	if !ok || hermetic.Name != "live-postgres-readiness hermetic" {
		return "missing hermetic check"
	}
	shards, ok := parsed.Jobs[readinessShardJob]
	if !ok || shards.Name != readinessShardName {
		return "missing sharded live legs"
	}
	aggregate, ok := parsed.Jobs[readinessAggregateJob]
	if !ok || aggregate.Name != "live-postgres-readiness (postgres18)" {
		return "missing aggregate live check"
	}
	if hermetic.TimeoutMinutes != 30 || shards.TimeoutMinutes != readinessShardTimeout ||
		aggregate.TimeoutMinutes != 10 {
		return "hermetic, shard, and aggregate jobs must retain independent 30/30/10-minute limits"
	}
	if !slices.Equal(readinessNeedsList(shards), []string{"readiness-plan"}) ||
		shards.ContinueOnError != "" {
		return "shards must wait for the plan without waiving failure"
	}
	if !slices.Equal(readinessNeedsList(aggregate), []string{"readiness-plan", readinessShardJob}) ||
		aggregate.If != readinessAlways || aggregate.ContinueOnError != "" {
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
	if !strings.Contains(shards.Services["postgres"].Options, "--tmpfs /var/lib/postgresql:") {
		return "postgres shard service must mount /var/lib/postgresql on tmpfs"
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
	checkoutCount, tunedPG, uploaded := 0, false, false
	for _, step := range shards.Steps {
		if strings.TrimSpace(step.Run) == "bash scripts/run-live-postgres-readiness-tests.sh" {
			return "a shard leg must not run the unsharded suite"
		}
		if step.Uses == "actions/checkout@v5" {
			checkoutCount++
			if step.With["persist-credentials"] != "false" || step.With["ref"] != "${{ github.sha }}" {
				return "shard checkout must use this run's commit without persisted credentials"
			}
		}
		if strings.Contains(step.Run, "ALTER SYSTEM SET fsync = off") &&
			strings.Contains(step.Run, "pg_reload_conf()") {
			tunedPG = true
		}
		if step.Uses == "actions/upload-artifact@v4" {
			return "shard legs must route uploads through retry-upload-artifact"
		}
		if step.Uses != "./.github/actions/retry-upload-artifact" {
			continue
		}
		if step.With["path"] == ".readiness-debug/" &&
			strings.Contains(step.With["name"], "readiness-debug-") &&
			strings.Contains(step.If, "!cancelled()") {
			continue
		}
		if step.With["path"] != "readiness-shard-report.json" ||
			step.With["if-no-files-found"] != "error" ||
			!strings.Contains(step.With["name"], "${{ matrix.shard }}") {
			return "shard legs must upload the current-attempt report or fail"
		}
		uploaded = true
	}
	if checkoutCount != 1 {
		return "shard leg must check out the repository once"
	}
	if !tunedPG {
		return "shard leg must tune ephemeral PostgreSQL (fsync=off + pg_reload_conf)"
	}
	if !uploaded {
		return "each shard leg must upload its coverage report for the aggregate"
	}
	if problem := checkReadinessAggregate(aggregate); problem != "" {
		return problem
	}
	if problem := checkReadinessJobSummary(hermetic, "hermetic", "", true); problem != "" {
		return problem
	}
	if problem := checkReadinessJobSummary(shards, "shard", readinessShardLabel, false); problem != "" {
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
	if problem := checkReadinessSplit(workflow); problem != "" {
		t.Fatal(problem)
	}
}
