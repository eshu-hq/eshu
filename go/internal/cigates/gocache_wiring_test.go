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

// checkGoBuildCache requires a Go lane to restore the persisted compiler
// cache before any go command runs (#8735). Fresh runners otherwise recompile
// the world on every leg (measured ~2:1 compile over test time on go-race).
// The restore runs unconditionally inside its lane: the lane job's own plan
// guard is the only selector, so a step-level guard could only skip a cache
// the lane then rebuilds from scratch.
func checkGoBuildCache(job readinessJob) string {
	if job.Env["GOCACHE"] != goBuildCacheDir {
		return "go lane must set GOCACHE to the persisted cache dir (#8735)"
	}
	restored := false
	for _, step := range job.Steps {
		if step.Uses == "actions/cache@v4" &&
			step.With["path"] == goBuildCacheDir {
			if step.If != "" || step.ContinueOnError != "" {
				return "build-cache restore must run unconditionally without waiving failure"
			}
			if !strings.Contains(step.With["key"], "hashFiles") {
				return "build-cache key must track dependencies (#8735)"
			}
			restored = true
			continue
		}
		if restored {
			continue
		}
		if strings.Contains(step.Run, "go ") ||
			strings.Contains(step.Run, "golangci-lint") {
			return "build-cache restore must run before the first go command (#8735)"
		}
	}
	if !restored {
		return "go lane must restore the persisted build cache (#8735)"
	}
	return ""
}

// TestGoBuildCacheWiring pins GOCACHE persistence on the slow Go lanes.
func TestGoBuildCacheWiring(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Jobs map[string]readinessJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, jobName := range []string{"go-core", "go-race"} {
		job, ok := parsed.Jobs[jobName]
		if !ok {
			t.Fatalf("job %q missing", jobName)
		}
		if problem := checkGoBuildCache(job); problem != "" {
			t.Fatalf("%s: %s", jobName, problem)
		}
	}
}
