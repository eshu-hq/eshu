// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// goBuildMutation breaks one rule of checkGoBuildCache on a copy of a lane.
type goBuildMutation struct {
	name string
	// lanes limits the mutation to save lanes or restore-only lanes.
	saveLanes, restoreOnlyLanes bool
	mutate                      func(t *testing.T, job *readinessJob)
	want                        string
}

// copyJobSteps gives the job its own steps and with-maps, so a mutation never
// leaks into another subtest.
func copyJobSteps(job *readinessJob) {
	steps := make([]readinessStep, len(job.Steps))
	for i, step := range job.Steps {
		step.With = maps.Clone(step.With)
		steps[i] = step
	}
	job.Steps = steps
}

// stepIndex returns the index of the first step matching match, or fails.
func stepIndex(t *testing.T, job *readinessJob, match func(readinessStep) bool) int {
	t.Helper()
	for i, step := range job.Steps {
		if match(step) {
			return i
		}
	}
	t.Fatal("mutation anchor step missing")
	return -1
}

func isSetupGo(step readinessStep) bool { return setupGoUsesRE.MatchString(step.Uses) }

func isBuildRestore(step readinessStep) bool {
	return step.Uses == "actions/cache/restore@v4" && step.With["path"] == goBuildCacheDir
}

func isBuildSave(step readinessStep) bool {
	return strings.HasPrefix(step.Uses, "actions/cache/save@") && step.With["path"] == goBuildCacheDir
}

// TestGoBuildCacheWiringMutations proves each #9598 rule is load-bearing: a
// seeded break of it fails the pin on every lane it applies to.
func TestGoBuildCacheWiringMutations(t *testing.T) {
	t.Parallel()
	at := func(match func(readinessStep) bool, edit func(*readinessJob, int)) func(*testing.T, *readinessJob) {
		return func(t *testing.T, job *readinessJob) { edit(job, stepIndex(t, job, match)) }
	}
	mutations := []goBuildMutation{
		{
			name: "setup-go cache re-enabled by default", saveLanes: true, restoreOnlyLanes: true,
			mutate: at(isSetupGo, func(j *readinessJob, i int) { delete(j.Steps[i].With, "cache") }),
			want:   "setup-go must set cache: false",
		},
		{
			name: "setup-go cache true", saveLanes: true, restoreOnlyLanes: true,
			mutate: at(isSetupGo, func(j *readinessJob, i int) { j.Steps[i].With["cache"] = "true" }),
			want:   "setup-go must set cache: false",
		},
		{
			name: "restore back to actions/cache", saveLanes: true, restoreOnlyLanes: true,
			mutate: at(isBuildRestore, func(j *readinessJob, i int) { j.Steps[i].Uses = "actions/cache@v4" }),
			want:   "must be actions/cache/restore",
		},
		{
			name: "restore renamed", saveLanes: true, restoreOnlyLanes: true,
			mutate: at(isBuildRestore, func(j *readinessJob, i int) { j.Steps[i].Name = "Restore Go cache" }),
			want:   "must keep the step name",
		},
		{
			name: "step between restore and setup-go", saveLanes: true, restoreOnlyLanes: true,
			mutate: at(isBuildRestore, func(j *readinessJob, i int) {
				extra := readinessStep{
					Name: "Restore Go module cache", Uses: "actions/cache/restore@v4",
					With: map[string]string{"path": "~/go/pkg/mod", "key": "gomod-${{ hashFiles('go/go.sum') }}"},
				}
				j.Steps = append(j.Steps[:i+1], append([]readinessStep{extra}, j.Steps[i+1:]...)...)
			}),
			want: "setup-go must directly follow",
		},
		{
			name: "restore-only lane saves", restoreOnlyLanes: true,
			mutate: func(_ *testing.T, j *readinessJob) {
				j.Steps = append(j.Steps, readinessStep{
					Name: "Save Go build cache", Uses: "actions/cache/save@v4",
					With: map[string]string{"path": goBuildCacheDir, "key": "k"},
				})
			},
			want: "restore-only lane must not save",
		},
		{
			name: "PR run saves (no event guard)", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) {
				j.Steps[i].If = strings.Replace(j.Steps[i].If, "github.event_name == 'push' && ", "", 1)
			}),
			want: "only on a main push",
		},
		{
			name: "PR run saves (always)", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) { j.Steps[i].If = "${{ always() }}" }),
			want:   "only on a main push",
		},
		{
			name: "PR run saves (or)", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) {
				j.Steps[i].If = strings.Replace(j.Steps[i].If, "&& github.ref", "|| github.ref", 1)
			}),
			want: "only on a main push",
		},
		{
			name: "save on exact hit", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) {
				j.Steps[i].If = "${{ github.event_name == 'push' && github.ref == 'refs/heads/main' }}"
			}),
			want: "only on a main push",
		},
		{
			name: "save key mismatch", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) {
				j.Steps[i].With["key"] = "${{ runner.os }}-${{ runner.arch }}-gobuild-${{ hashFiles('go/go.sum') }}"
			}),
			want: "save key must be the restore step's primary key",
		},
		{
			name: "save missing", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) { j.Steps = append(j.Steps[:i], j.Steps[i+1:]...) }),
			want:   "save the build cache exactly once",
		},
		{
			name: "save not last", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) {
				j.Steps[i], j.Steps[i-1] = j.Steps[i-1], j.Steps[i]
			}),
			want: "must be the last step",
		},
		{
			name: "save waives failure", saveLanes: true,
			mutate: at(isBuildSave, func(j *readinessJob, i int) { j.Steps[i].ContinueOnError = "true" }),
			want:   "must not waive failure",
		},
	}
	for _, lane := range goBuildLanes {
		base := loadGoBuildLane(t, lane)
		for _, mutation := range mutations {
			if (lane.saves && !mutation.saveLanes) || (!lane.saves && !mutation.restoreOnlyLanes) {
				continue
			}
			t.Run(lane.job+"/"+mutation.name, func(t *testing.T) {
				job := base
				copyJobSteps(&job)
				mutation.mutate(t, &job)
				problem := checkGoBuildCache(job, lane.saves)
				if !strings.Contains(problem, mutation.want) {
					t.Fatalf("seeded break passed or failed for another reason: got %q, want %q", problem, mutation.want)
				}
			})
		}
	}
}

// TestSetupGoCacheDisabledAllWorkflowsMutations proves the repo-wide
// no-cache rule is load-bearing and its exception list is honest:
// re-enabling the built-in cache on a covered workflow fails the pin with
// that workflow's name, and every allowlisted file still holds a caching
// setup-go step, so a stale exception cannot linger once the file goes
// cache: false.
func TestSetupGoCacheDisabledAllWorkflowsMutations(t *testing.T) {
	t.Parallel()
	base := loadAllWorkflowJobs(t)
	t.Run("seeded re-enable fails", func(t *testing.T) {
		t.Parallel()
		all := make(map[string]map[string]prewarmJob, len(base))
		for f, jobs := range base {
			all[f] = jobs
		}
		jobs := make(map[string]prewarmJob, len(all["bench.yml"]))
		for k, v := range all["bench.yml"] {
			steps := make([]prewarmStep, len(v.Steps))
			for i, step := range v.Steps {
				step.With = maps.Clone(step.With)
				steps[i] = step
			}
			v.Steps = steps
			jobs[k] = v
		}
		all["bench.yml"] = jobs
		seeded := false
		for key, job := range jobs {
			for i, step := range job.Steps {
				if setupGoUsesRE.MatchString(step.Uses) {
					delete(job.Steps[i].With, "cache")
					seeded = true
					break
				}
			}
			if seeded {
				jobs[key] = job
				break
			}
		}
		if !seeded {
			t.Fatal("mutation anchor step missing: bench.yml has no setup-go step")
		}
		problem := checkSetupGoCacheDisabledAllWorkflows(all)
		if !strings.Contains(problem, "bench.yml") || !strings.Contains(problem, "must set cache: false") {
			t.Fatalf("seeded break passed or failed for another reason: got %q", problem)
		}
	})
	t.Run("exceptions still cache", func(t *testing.T) {
		t.Parallel()
		for file := range setupGoCacheDisabledExcept {
			caching := false
			for _, job := range base[file] {
				for _, step := range job.Steps {
					if setupGoUsesRE.MatchString(step.Uses) && setupGoCachesModule(step.With) {
						caching = true
					}
				}
			}
			if !caching {
				t.Errorf("allowlisted %s holds no caching setup-go step: remove the stale exception", file)
			}
		}
	})
}

// loadStaticContractJobs parses the gate job of static-contract-gates.yml
// for the cache-wiring mutation seeds below.
func loadStaticContractJobs(t *testing.T) map[string]prewarmJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "static-contract-gates.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed prewarmWorkflowFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Jobs
}

// TestStaticContractCacheWiringMutations proves the static-contract cache
// rule is load-bearing: restoring the bare save-on-every-run action or
// loosening the save guard fails the pin.
func TestStaticContractCacheWiringMutations(t *testing.T) {
	t.Parallel()
	base := loadStaticContractJobs(t)
	t.Run("bare cache action fails", func(t *testing.T) {
		t.Parallel()
		jobs := make(map[string]prewarmJob, len(base))
		for k, v := range base {
			jobs[k] = v
		}
		job := jobs["gate"]
		steps := make([]prewarmStep, len(job.Steps))
		copy(steps, job.Steps)
		seeded := false
		for i, step := range steps {
			if step.Uses == "actions/cache/restore@v4" && step.With["path"] == "/tmp/eshu-ci" {
				steps[i].Uses = "actions/cache@v4"
				seeded = true
				break
			}
		}
		if !seeded {
			t.Fatal("mutation anchor step missing: gate has no /tmp/eshu-ci restore")
		}
		job.Steps = steps
		jobs["gate"] = job
		if problem := checkStaticContractCacheWiring(jobs); !strings.Contains(problem, "must use actions/cache/restore") {
			t.Fatalf("seeded break passed or failed for another reason: got %q", problem)
		}
	})
	t.Run("unguarded save fails", func(t *testing.T) {
		t.Parallel()
		jobs := make(map[string]prewarmJob, len(base))
		for k, v := range base {
			jobs[k] = v
		}
		job := jobs["gate"]
		steps := make([]prewarmStep, len(job.Steps))
		copy(steps, job.Steps)
		seeded := false
		for i, step := range steps {
			if strings.HasPrefix(step.Uses, "actions/cache/save@") {
				steps[i].If = "${{ always() }}"
				seeded = true
				break
			}
		}
		if !seeded {
			t.Fatal("mutation anchor step missing: gate has no save step")
		}
		job.Steps = steps
		jobs["gate"] = job
		if problem := checkStaticContractCacheWiring(jobs); !strings.Contains(problem, "only on a main push") {
			t.Fatalf("seeded break passed or failed for another reason: got %q", problem)
		}
	})
}

// TestSetupGoCacheDisabledMutations proves the test.yml no-cache rule is
// load-bearing: re-enabling the built-in cache on any covered job fails the
// pin with its own message.
func TestSetupGoCacheDisabledMutations(t *testing.T) {
	t.Parallel()
	base := loadTestWorkflowJobs(t)
	mutations := []struct {
		name string
		job  string
		edit func(with map[string]string)
	}{
		{name: "cache key removed", job: "merge-group-slim", edit: func(with map[string]string) { delete(with, "cache") }},
		{name: "cache true", job: "verify-contracts", edit: func(with map[string]string) { with["cache"] = "true" }},
		{name: "cache true on pr-plan", job: "pr-plan", edit: func(with map[string]string) { with["cache"] = "true" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.job+"/"+mutation.name, func(t *testing.T) {
			jobs := make(map[string]readinessJob, len(base))
			for k, v := range base {
				jobs[k] = v
			}
			job := jobs[mutation.job]
			copyJobSteps(&job)
			i := stepIndex(t, &job, isSetupGo)
			mutation.edit(job.Steps[i].With)
			jobs[mutation.job] = job
			problem := checkSetupGoCacheDisabled(jobs)
			if !strings.Contains(problem, "must set cache: false") {
				t.Fatalf("seeded break passed or failed for another reason: got %q", problem)
			}
		})
	}
}
