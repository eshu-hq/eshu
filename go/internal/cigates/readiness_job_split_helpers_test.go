// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strconv"
	"strings"
)

func checkReadinessCleanup(job readinessJob) string {
	starts, stops := 0, 0
	for _, step := range job.Steps {
		switch strings.TrimSpace(step.Run) {
		case readinessFixtureStart:
			starts++
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "standby start must use the registry queue selection without waiving failure"
			}
		case readinessFixtureStop:
			stops++
			if step.If != readinessCleanupSelection || step.ContinueOnError != "" {
				return "standby cleanup must run on every shard-leg outcome"
			}
		default:
			if strings.Contains(step.Run, "/live-postgres-standby-fixture.sh ") {
				return "standby fixture must carry the shard suffix so parallel legs cannot collide"
			}
		}
	}
	if starts != 1 || stops != 1 {
		return "each shard leg must start and stop its disposable standby exactly once"
	}
	return ""
}

func checkReadinessAggregate(job readinessJob) string {
	downloaded, verified, summarized := false, false, false
	for _, step := range job.Steps {
		if step.Uses == "actions/download-artifact@v4" {
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "artifact download must use the registry queue selection without waiving failure"
			}
			if step.With["pattern"] != "readiness-shard-${{ github.run_id }}-${{ github.run_attempt }}-*" ||
				step.With["path"] != readinessArtifactPath ||
				step.With["merge-multiple"] != "" {
				return "aggregate must download exactly the current-attempt shard reports"
			}
			downloaded = true
			continue
		}
		if strings.Contains(step.Run, "live_postgres_readiness_shards.py verify") {
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "coverage verification must use the registry queue selection without waiving failure"
			}
			verified = true
		}
		if strings.Contains(step.Run, "live_postgres_readiness_results.py summary") {
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "suite summary must use the registry queue selection without waiving failure"
			}
			summarized = true
		}
	}
	if !downloaded {
		return "aggregate must download the shard reports before verifying coverage"
	}
	if !verified {
		return "aggregate must verify the four shard reports cover the full plan"
	}
	if !summarized {
		return "aggregate must print the suite summary only after coverage verifies"
	}
	return ""
}

func checkReadinessNoDockerHubLogin(job readinessJob) string {
	if _, ok := job.Env["DOCKERHUB_LOGIN_ENABLED"]; ok {
		return "live job must not carry a Docker Hub login flag: its services use public GHCR mirrors"
	}
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "docker/login-action@") {
			continue
		}
		if registry, ok := step.With["registry"]; ok && registry != "" &&
			!strings.Contains(registry, "docker.io") {
			continue
		}
		return "live job must not log in to Docker Hub: its services use public GHCR mirrors"
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

const (
	readinessSummaryScript = "scripts/ci/live-postgres-readiness-job-summary.sh"
	readinessSummaryTest   = "bash scripts/ci/test-live-postgres-readiness-job-summary.sh"
)

// checkReadinessHermeticSummary requires the hermetic job to record its start
// time first and to end with an always-run summary step that classifies the
// job by status and elapsed time against the job's own cap, so a run cut off
// by the cap reads differently from a failure (#8241).
func checkReadinessHermeticSummary(job readinessJob) string {
	steps := job.Steps
	if len(steps) == 0 || strings.TrimSpace(steps[0].Run) != "bash "+readinessSummaryScript+" start" ||
		steps[0].If != "" || steps[0].ContinueOnError != "" {
		return "hermetic job must record its start time in its first step"
	}
	last := steps[len(steps)-1]
	if strings.TrimSpace(last.Run) != "bash "+readinessSummaryScript+" finish" ||
		last.If != readinessAlways || last.ContinueOnError != "" {
		return "hermetic job must end with an always-run summary step"
	}
	if last.Env["JOB_STATUS"] != "${{ job.status }}" {
		return "hermetic summary step must read the job status"
	}
	if last.Env["CAP_MINUTES"] != strconv.Itoa(job.TimeoutMinutes) {
		return "hermetic summary CAP_MINUTES must equal the job timeout-minutes"
	}
	runs := 0
	for _, step := range steps {
		if strings.TrimSpace(step.Run) == readinessSummaryTest {
			runs++
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "summary classifier test must use the registry queue selection without waiving failure"
			}
		}
	}
	if runs != 1 {
		return "hermetic job must run the summary classifier test exactly once"
	}
	return ""
}
