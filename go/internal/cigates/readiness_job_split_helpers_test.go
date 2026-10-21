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
	downloaded, selected, verified, summarized := false, false, false, false
	for _, step := range job.Steps {
		if step.Uses == "actions/download-artifact@v4" {
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "artifact download must use the registry queue selection without waiving failure"
			}
			if step.With["pattern"] != "readiness-shard-${{ github.run_id }}-*" ||
				step.With["path"] != readinessArtifactPath ||
				step.With["merge-multiple"] != "" {
				return "aggregate must download the run-scoped shard reports so reruns reunite (#8724)"
			}
			downloaded = true
			continue
		}
		if strings.Contains(step.Run, "live_postgres_readiness_shards.py select") {
			if step.If != readinessQueueSelection || step.ContinueOnError != "" {
				return "newest-report selection must use the registry queue selection without waiving failure"
			}
			selected = true
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
	if !selected {
		return "aggregate must select the newest valid report per shard before verifying coverage (#8724)"
	}
	if !verified {
		return "aggregate must verify the four shard reports cover the full plan"
	}
	if !summarized {
		return "aggregate must print the suite summary only after coverage verifies"
	}
	return ""
}

// backendQueueSelection is the live-backend aggregate guard: same shape as
// the readiness one, without the extra contains() parentheses the readiness
// conditions carry. Copy it exactly from live-backend-tests.yml.
const (
	backendQueueSelection  = "${{ github.event_name != 'merge_group' || contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'live-backend (nornicdb and neo4j)') }}"
	backendAggregateJob    = "live-backend"
	backendArtifactPath    = "live-backend-reports"
	backendArtifactPattern = "live-backend-${{ github.run_id }}-*"
)

// checkBackendAggregate pins the live-backend aggregate wiring the same way
// checkReadinessAggregate pins the readiness one: run-scoped download so a
// failed-job rerun reunites every attempt's reports, then newest-valid select
// before the exact-four verify (#8724). The backend hunk is small enough to
// revert silently, so this guard must fail on a planted revert.
func checkBackendAggregate(job readinessJob) string {
	downloaded, selected, verified := false, false, false
	for _, step := range job.Steps {
		if step.Uses == "actions/download-artifact@v4" {
			if step.If != backendQueueSelection || step.ContinueOnError != "" {
				return "artifact download must use the registry queue selection without waiving failure"
			}
			if step.With["pattern"] != backendArtifactPattern ||
				step.With["path"] != backendArtifactPath ||
				step.With["merge-multiple"] != "" {
				return "aggregate must download the run-scoped coverage reports so reruns reunite (#8724)"
			}
			downloaded = true
			continue
		}
		if strings.Contains(step.Run, "live_backend_shards.py select") {
			if step.If != backendQueueSelection || step.ContinueOnError != "" {
				return "newest-report selection must use the registry queue selection without waiving failure"
			}
			selected = true
		}
		if strings.Contains(step.Run, "live_backend_shards.py verify") {
			if step.If != backendQueueSelection || step.ContinueOnError != "" {
				return "coverage verification must use the registry queue selection without waiving failure"
			}
			verified = true
		}
	}
	if !downloaded {
		return "aggregate must download the coverage reports before verifying coverage"
	}
	if !selected {
		return "aggregate must select the newest valid report per shard before verifying coverage (#8724)"
	}
	if !verified {
		return "aggregate must verify the shard reports cover the full plan"
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
	// readinessStartRecord is the whole first step: it needs no checkout, so
	// it must not name a repo file. NAME=value is the $GITHUB_ENV format.
	readinessStartRecord = `echo "READINESS_JOB_STARTED_EPOCH=$(date +%s)" >> "$GITHUB_ENV"`
)

// checkReadinessHermeticSummary requires the hermetic job to record its start
// time inline in its first step (before checkout, so no repo file is needed)
// and to end with an always-run summary step that classifies the job by
// status and elapsed time against the job's own cap, so a run cut off by the
// cap reads differently from a failure (#8241). The summary step must also
// tolerate an absent script (checkout skipped or not yet run) and a failing
// classifier, so it can never fail a healthy job. No other step before the
// checkout may run the repo script.
func checkReadinessHermeticSummary(job readinessJob) string {
	steps := job.Steps
	if len(steps) == 0 || strings.TrimSpace(steps[0].Run) != readinessStartRecord ||
		steps[0].If != "" || steps[0].ContinueOnError != "" {
		return "hermetic job must record its start time inline in its first step, with no repo file"
	}
	checkedOut := false
	for i, step := range steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			checkedOut = true
		}
		if !checkedOut && i != len(steps)-1 && strings.Contains(step.Run, readinessSummaryScript) {
			return "hermetic step runs a repo script before checkout"
		}
	}
	last := steps[len(steps)-1]
	if last.If != readinessAlways || last.ContinueOnError != "" ||
		!strings.Contains(last.Run, `bash "$script" finish`) || !strings.Contains(last.Run, "script="+readinessSummaryScript) {
		return "hermetic job must end with an always-run summary step"
	}
	if !strings.Contains(last.Run, `if [ ! -f "$script" ]; then`) || !strings.Contains(last.Run, "exit 0\n") {
		return "hermetic summary step must skip, not fail, when the script is absent"
	}
	if !strings.Contains(last.Run, `bash "$script" finish || echo`) {
		return "hermetic summary step must not fail the job when the classifier fails"
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
