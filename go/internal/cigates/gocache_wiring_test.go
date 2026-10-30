// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// goBuildCacheDir is the single GOCACHE location every persisted Go lane
// restores, mirroring static-contract-gates.yml.
const goBuildCacheDir = "/tmp/eshu-ci/go-build"

// goBuildRestoreStep is the restore step name every persisted lane uses. It
// keeps each job log's cache outcome at a stable landmark: the "Cache
// restored from key:" or "Cache not found for input keys" line sits inside
// this step's log group, which ends where the next step's group begins. So
// the step keeps this name and setup-go follows it directly; any other cache
// step placed between them would add its own outcome lines to that span.
// In scripts/lib/live_postgres_readiness_refresh_v2_pool.py (#9604),
// cache_era finds this step by name (a shard without it counts as cold) and
// restore_outcome reads the shard's cache outcome from exactly that span.
const goBuildRestoreStep = "Restore Go build cache"

// goBuildSaveCondition is the only condition a build-cache save may run on
// (#9598). The repository cache store holds 10 GB and evicts by last access,
// so PR-scoped copies (which only that PR's re-runs can restore) evict main's
// entry. Main push alone saves; PR runs restore main's entry, which the
// default-branch fallback of actions/cache allows. An exact-key hit saves
// nothing, as the combined actions/cache step did before the split.
func goBuildSaveCondition(restoreID string) string {
	return "github.event_name == 'push' && github.ref == 'refs/heads/main' && steps." +
		restoreID + ".outputs.cache-hit != 'true'"
}

// goBuildLane names one job that persists GOCACHE and whether it owns the save.
type goBuildLane struct {
	workflow string
	job      string
	saves    bool
}

// goBuildLanes lists every job that sets GOCACHE to goBuildCacheDir and runs
// actions/setup-go. go-core and go-race save their keys on main push; the
// readiness shards restore go-core's entry and never save (#9460).
var goBuildLanes = []goBuildLane{
	{workflow: "test.yml", job: "go-core", saves: true},
	{workflow: "test.yml", job: "go-race", saves: true},
	{workflow: "live-postgres-readiness.yml", job: "live-postgres-readiness-shards", saves: false},
}

// stripExpression removes one ${{ }} wrapper and surrounding space.
func stripExpression(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "${{") && strings.HasSuffix(value, "}}") {
		value = strings.TrimSpace(value[3 : len(value)-2])
	}
	return value
}

// checkGoBuildCache requires a Go lane to restore the persisted compiler
// cache before any go command runs (#8735), and pins who may write it
// (#9598). Fresh runners otherwise recompile the world on every leg
// (measured ~2:1 compile over test time on go-race). The restore runs
// unconditionally inside its lane: the lane job's own plan guard is the only
// selector, so a step-level guard could only skip a cache the lane then
// rebuilds from scratch.
func checkGoBuildCache(job readinessJob, saves bool) string {
	if job.Env["GOCACHE"] != goBuildCacheDir {
		return "go lane must set GOCACHE to the persisted cache dir (#8735)"
	}
	restoreIndex, saveIndex, saveCount := -1, -1, 0
	for i, step := range job.Steps {
		if step.With["path"] == goBuildCacheDir {
			switch {
			case step.Uses == "actions/cache@v4":
				return "build-cache step must be actions/cache/restore: actions/cache saves on every PR run (#9598)"
			case step.Uses == "actions/cache/restore@v4":
				if restoreIndex >= 0 {
					return "go lane must restore the build cache once"
				}
				restoreIndex = i
			case strings.HasPrefix(step.Uses, "actions/cache/save@"):
				saveIndex = i
				saveCount++
			}
		}
		if setupGoUsesRE.MatchString(step.Uses) && setupGoCachesModule(step.With) {
			return "setup-go must set cache: false in a persisted-GOCACHE lane: its own cache stores the build cache a second time (#9598)"
		}
		if restoreIndex < 0 && (strings.Contains(step.Run, "go ") ||
			strings.Contains(step.Run, "golangci-lint")) {
			return "build-cache restore must run before the first go command (#8735)"
		}
	}
	if restoreIndex < 0 {
		return "go lane must restore the persisted build cache (#8735)"
	}
	restore := job.Steps[restoreIndex]
	if restore.If != "" || restore.ContinueOnError != "" {
		return "build-cache restore must run unconditionally without waiving failure"
	}
	if !strings.Contains(restore.With["key"], "hashFiles") {
		return "build-cache key must track dependencies (#8735)"
	}
	if restore.Name != goBuildRestoreStep {
		return "build-cache restore must keep the step name " + goBuildRestoreStep + " (the log landmark for the cache outcome)"
	}
	if restoreIndex+1 >= len(job.Steps) || !setupGoUsesRE.MatchString(job.Steps[restoreIndex+1].Uses) {
		return "setup-go must directly follow the build-cache restore (the restore's log group must hold only its own cache outcome)"
	}
	if !saves {
		if saveCount != 0 {
			return "restore-only lane must not save the build cache (#9598)"
		}
		return ""
	}
	if saveCount != 1 {
		return "go lane must save the build cache exactly once, on main push (#9598)"
	}
	if saveIndex != len(job.Steps)-1 {
		return "build-cache save must be the last step, after every go command (#9598)"
	}
	save := job.Steps[saveIndex]
	if restore.ID == "" {
		return "build-cache restore needs an id for the save to read its key (#9598)"
	}
	if stripExpression(save.If) != goBuildSaveCondition(restore.ID) {
		return "build-cache save must run only on a main push that missed the exact key (#9598)"
	}
	if stripExpression(save.With["key"]) != "steps."+restore.ID+".outputs.cache-primary-key" {
		return "build-cache save key must be the restore step's primary key (#9598)"
	}
	if save.ContinueOnError != "" {
		return "build-cache save must not waive failure"
	}
	return ""
}

// loadGoBuildLane parses one lane's job from its workflow file.
func loadGoBuildLane(t *testing.T, lane goBuildLane) readinessJob {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "workflows", lane.workflow)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Jobs map[string]readinessJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	job, ok := parsed.Jobs[lane.job]
	if !ok {
		t.Fatalf("%s: job %q missing", lane.workflow, lane.job)
	}
	return job
}

// TestGoBuildCacheWiring pins GOCACHE persistence on the slow Go lanes.
func TestGoBuildCacheWiring(t *testing.T) {
	t.Parallel()
	for _, lane := range goBuildLanes {
		job := loadGoBuildLane(t, lane)
		if problem := checkGoBuildCache(job, lane.saves); problem != "" {
			t.Errorf("%s %s: %s", lane.workflow, lane.job, problem)
		}
	}
}
