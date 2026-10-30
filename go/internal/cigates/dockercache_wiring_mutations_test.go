// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"strings"
	"testing"
)

// TestDockerCacheWritersOutsideTheBuildSteps proves a second cache writer is
// refused: a duplicate cache-to key, a docker/bake-action step, and a shell
// build that passes --cache-to (#9632).
func TestDockerCacheWritersOutsideTheBuildSteps(t *testing.T) {
	t.Parallel()
	dup := []byte(`jobs:
  build:
    runs-on: ubuntu-24.04
    steps:
      - name: Build
        uses: docker/build-push-action@v5
        with:
          cache-to: ${{ github.event_name == 'push' && 'type=registry,ref=` + dockerCacheRef + `runtime,mode=max' || '' }}
          cache-to: type=registry,ref=` + dockerCacheRef + `runtime,mode=max
`)
	if _, err := parseWorkflowJobs(dup); err == nil {
		t.Error("a duplicate cache-to key must be a parse error")
	}
	clean := loadWorkflowJobs(t, "docker-publish.yml")
	for _, extra := range []struct {
		name string
		step readinessStep
		want string
	}{
		{"bake action", readinessStep{Name: "Bake", Uses: "docker/bake-action@v5"}, "docker/bake-action"},
		{"shell build", readinessStep{Name: "Shell build", Run: "docker buildx build --cache-to type=registry,ref=x ."}, "--cache-to"},
	} {
		t.Run(extra.name, func(t *testing.T) {
			jobs := make(map[string]readinessJob, len(clean))
			for name, j := range clean {
				copyJobSteps(&j)
				jobs[name] = j
			}
			job := jobs["build-and-push-image"]
			job.Steps = append(job.Steps, extra.step)
			jobs["build-and-push-image"] = job
			if problem := checkDockerCache(jobs); !strings.Contains(problem, extra.want) {
				t.Fatalf("want a problem containing %q, got %q", extra.want, problem)
			}
		})
	}
}

// TestDockerBuildCacheWiringMutations proves each #9632 rule is load-bearing:
// a seeded break of it, applied to each docker build step in turn, fails the
// pin, while the unmutated workflow passes.
func TestDockerBuildCacheWiringMutations(t *testing.T) {
	t.Parallel()
	push := func(export string) string { return "${{ github.event_name == 'push' && '" + export + "' || '' }}" }
	setWith := func(key, value string) func(*readinessStep) {
		return func(s *readinessStep) { s.With[key] = value }
	}
	mutations := []struct {
		name   string
		mutate func(*readinessStep)
		want   string
	}{
		{"cache-to on every event", setWith("cache-to", "type=registry,ref="+dockerCacheRef+"runtime,mode=max"), "only on a push event"},
		{"cache-to on anything but pull_request", setWith("cache-to",
			"${{ github.event_name != 'pull_request' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"cache-to falls back to a write", setWith("cache-to",
			"${{ github.event_name == 'push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || 'type=gha,mode=max' }}"), "only on a push event"},
		{"push gate with a pull_request OR-branch", setWith("cache-to",
			"${{ github.event_name == 'push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || github.event_name == 'pull_request' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"push gate with an unconditional OR-branch", setWith("cache-to",
			"${{ github.event_name == 'push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"pull_request branch ahead of the push gate", setWith("cache-to",
			"${{ github.event_name == 'pull_request' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || github.event_name == 'push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"push gate with an extra && operand", setWith("cache-to",
			"${{ github.event_name == 'push' && github.ref != 'refs/heads/x' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"push gate with a case variant", setWith("cache-to",
			"${{ GitHub.Event_Name == 'push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"push gate without spaces", setWith("cache-to",
			"${{ github.event_name=='push' && 'type=registry,ref="+dockerCacheRef+"runtime,mode=max' || '' }}"), "only on a push event"},
		{"cache-to supplied by the matrix", setWith("cache-to", "${{ matrix.cache_to }}"), "only on a push event"},
		{"second unconditional export on a new line", func(s *readinessStep) {
			s.With["cache-to"] += "\ntype=registry,ref=" + dockerCacheRef + "runtime,mode=max"
		}, "only on a push event"},
		{"two expressions in one value", func(s *readinessStep) {
			s.With["cache-to"] += "${{ 'type=registry,ref=" + dockerCacheRef + "runtime,mode=max' }}"
		}, "only on a push event"},
		{"cache-to back to the Actions cache", setWith("cache-to", push("type=gha,mode=max")), "must not use the Actions cache store"},
		{"cache-to into the image package", setWith("cache-to", push("type=registry,ref=ghcr.io/eshu-hq/eshu:buildcache-runtime,mode=max")), "registry package"},
		{"cache-to mode=min", setWith("cache-to", push("type=registry,ref="+dockerCacheRef+"runtime,mode=min")), "mode=max"},
		{"cache-from back to the Actions cache", func(s *readinessStep) { s.With["cache-from"] += "\ntype=gha" }, "must not read the Actions cache store"},
		{"cache-from removed", func(s *readinessStep) { delete(s.With, "cache-from") }, "must read the registry build cache"},
		{"cache-from gated off PRs", setWith("cache-from",
			"type=registry,ref="+dockerCacheRef+"runtime${{ github.event_name == 'push' && '' || '' }}"), "must not be gated by event"},
		{"cache-to tag shared with another target", func(s *readinessStep) {
			tag := "scanner-image"
			if s.With["target"] != "" {
				tag = "runtime"
			}
			s.With["cache-to"] = push("type=registry,ref=" + dockerCacheRef + tag + ",mode=max")
		}, "one fixed tag per target"},
		{"cache-from loses its own tag", func(s *readinessStep) {
			tag := "runtime"
			if s.With["target"] == "" {
				tag = "scanner-image"
			}
			s.With["cache-from"] = "type=registry,ref=" + dockerCacheRef + tag
		}, "own cache tag"},
		{"secret passed to BuildKit", setWith("secrets", "token=${{ secrets.GITHUB_TOKEN }}"), "must not pass secrets"},
		{"secret build-arg", func(s *readinessStep) { s.With["build-args"] += "\nTOKEN=${{ secrets.GITHUB_TOKEN }}" }, "must not carry a secret"},
	}
	clean := loadWorkflowJobs(t, "docker-publish.yml")
	if problem := checkDockerCache(clean); problem != "" {
		t.Fatalf("unmutated workflow must pass, got %q", problem)
	}
	for jobName, job := range clean {
		for stepIndex, step := range job.Steps {
			if !isDockerBuild(step) {
				continue
			}
			for _, m := range mutations {
				t.Run(jobName+"/"+m.name, func(t *testing.T) {
					jobs := make(map[string]readinessJob, len(clean))
					for name, j := range clean {
						copyJobSteps(&j)
						jobs[name] = j
					}
					m.mutate(&jobs[jobName].Steps[stepIndex])
					problem := checkDockerCache(jobs)
					if !strings.Contains(problem, m.want) {
						t.Fatalf("mutation %q: want a problem containing %q, got %q", m.name, m.want, problem)
					}
				})
			}
		}
	}
}
