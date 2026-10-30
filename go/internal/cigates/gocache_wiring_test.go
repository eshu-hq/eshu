// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// loadTestWorkflowJobs parses every job in test.yml, so the no-cache rule
// below covers jobs added later without a lane entry.
func loadTestWorkflowJobs(t *testing.T) map[string]readinessJob {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "workflows", "test.yml")
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
	if len(parsed.Jobs) == 0 {
		t.Fatal("test.yml: no jobs parsed")
	}
	return parsed.Jobs
}

// checkSetupGoCacheDisabled requires every actions/setup-go step in test.yml
// to set cache: false. setup-go's built-in cache stores go env GOCACHE, so
// every default-cache step writes a second build-cache copy, and each
// PR-scoped copy counts against the 10 GB store that evicted main's gobuild
// entries (#9462, #9598). Modules come from
// scripts/ci/go-mod-download-retry.sh instead. Sorted job order keeps the
// verdict deterministic; the first violation wins.
func checkSetupGoCacheDisabled(jobs map[string]readinessJob) string {
	keys := make([]string, 0, len(jobs))
	for k := range jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for i, step := range jobs[key].Steps {
			if setupGoUsesRE.MatchString(step.Uses) && setupGoCachesModule(step.With) {
				return fmt.Sprintf("test.yml job %q: setup-go step %d must set cache: false so its built-in cache does not store the build cache a second time (#9462)", key, i)
			}
		}
	}
	return ""
}

// TestSetupGoCacheDisabledInTestWorkflow pins the #9462 tail: no setup-go
// step in test.yml may use the built-in cache, including jobs on the default
// GOCACHE that never shared the persisted directory.
func TestSetupGoCacheDisabledInTestWorkflow(t *testing.T) {
	t.Parallel()
	if problem := checkSetupGoCacheDisabled(loadTestWorkflowJobs(t)); problem != "" {
		t.Error(problem)
	}
}

// setupGoCacheDisabledExcept names the only workflow files whose setup-go
// steps may keep the built-in cache, with the bounded reason each is
// excluded from the repo-wide cache: false rule (#9648). Every other
// workflow_call leaf runs on pull requests, so each default-cache step
// writes a PR-scoped ~700 MB build-cache copy that counts against the 10 GB
// store whose evictions empty main's gobuild entries.
var setupGoCacheDisabledExcept = map[string]string{
	// Nightly + workflow_dispatch only, never on pull_request, so it
	// writes no PR-scoped copies. Its key is OS-isolated (macOS arm64,
	// one 808 MB entry on main) and the 45-minute job envelope was
	// measured with a warm cache; forcing a cold macOS compile risks
	// that envelope for no store saving.
	"macos.yml": "nightly-only macOS leg with an OS-isolated single main entry",
}

// loadAllWorkflowJobs parses every workflow file's jobs, keyed by file
// name then job name, so the repo-wide no-cache rule covers workflows
// added later without a lane entry. It reuses the pre-warm check's minimal
// job shape: readinessJob's typed timeout-minutes cannot parse expression
// timeouts such as static-contract-gates.yml's matrix timeout.
func loadAllWorkflowJobs(t *testing.T) map[string]map[string]prewarmJob {
	t.Helper()
	dir := filepath.Join("..", "..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := make(map[string]map[string]prewarmJob)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- dir-confined, entry name comes from os.ReadDir of that same directory
		if err != nil {
			t.Fatal(err)
		}
		var parsed prewarmWorkflowFile
		if err := yaml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		all[name] = parsed.Jobs
	}
	if len(all) == 0 {
		t.Fatal("no workflow files parsed")
	}
	return all
}

// checkSetupGoCacheDisabledAllWorkflows requires every actions/setup-go
// step in every workflow to set cache: false, except allowlisted files.
// Sorted file and job order keeps the verdict deterministic; the first
// violation wins.
func checkSetupGoCacheDisabledAllWorkflows(all map[string]map[string]prewarmJob) string {
	files := make([]string, 0, len(all))
	for f := range all {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		if _, ok := setupGoCacheDisabledExcept[file]; ok {
			continue
		}
		jobs := all[file]
		keys := make([]string, 0, len(jobs))
		for k := range jobs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for i, step := range jobs[key].Steps {
				if setupGoUsesRE.MatchString(step.Uses) && setupGoCachesModule(step.With) {
					return fmt.Sprintf("%s job %q: setup-go step %d must set cache: false so its built-in cache does not store a PR-scoped build-cache copy in the 10 GB store (#9648)", file, key, i)
				}
			}
		}
	}
	return ""
}

// TestSetupGoCacheDisabledAllWorkflows pins the #9648 sweep: the #9462
// cache: false rule holds repo-wide, with only the documented exceptions.
func TestSetupGoCacheDisabledAllWorkflows(t *testing.T) {
	t.Parallel()
	if problem := checkSetupGoCacheDisabledAllWorkflows(loadAllWorkflowJobs(t)); problem != "" {
		t.Error(problem)
	}
}

// checkStaticContractCacheWiring pins the #9648 re-examination of
// static-contract-gates.yml's 724 MB Linux-X64-go- entry: no step may use
// the bare actions/cache action (which saves a PR-scoped copy on every
// workflow_call run into the 10 GB store), and the single save step must
// run only on a main push that missed the exact key, mirroring the #9598
// go-lane pattern.
func checkStaticContractCacheWiring(jobs map[string]prewarmJob) string {
	keys := make([]string, 0, len(jobs))
	for k := range jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	saves := 0
	for _, key := range keys {
		for i, step := range jobs[key].Steps {
			if step.Uses == "actions/cache@v4" {
				return fmt.Sprintf("static-contract-gates.yml job %q: step %d must use actions/cache/restore, not actions/cache: the bare action saves a PR-scoped 724 MB copy on every run (#9648)", key, i)
			}
			if strings.HasPrefix(step.Uses, "actions/cache/save@") {
				saves++
				if !strings.Contains(step.If, "github.event_name == 'push'") ||
					!strings.Contains(step.If, "refs/heads/main") ||
					!strings.Contains(step.If, "cache-hit != 'true'") {
					return fmt.Sprintf("static-contract-gates.yml job %q: save step %d must run only on a main push that missed the exact key (#9648)", key, i)
				}
			}
		}
	}
	if saves != 1 {
		return fmt.Sprintf("static-contract-gates.yml must save its cache exactly once on a main push, found %d save steps (#9648)", saves)
	}
	return ""
}

// TestStaticContractCacheWiring pins the converted static-contract cache:
// restore-only on pull requests, one guarded main-push save.
func TestStaticContractCacheWiring(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "static-contract-gates.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed prewarmWorkflowFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if problem := checkStaticContractCacheWiring(parsed.Jobs); problem != "" {
		t.Error(problem)
	}
}
