// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// dockerCacheRef is the dedicated GHCR package that holds the BuildKit cache
// (#9632). It is not an image package: the image packages hold untagged
// per-platform child manifests of every multi-arch image, so the 7-day prune
// of untagged versions is only safe in a package that holds cache manifests
// alone.
const dockerCacheRef = "ghcr.io/eshu-hq/eshu-buildcache:"

// dockerCacheToRE is the only shape a cache-to may take: a registry export
// written on a push event (main or tag) and nothing on any other event. A
// pull_request run (including test.yml's workflow_call, whose event is the
// caller's) and workflow_dispatch evaluate to an empty cache-to, so a PR,
// fork or not, never writes a cache that main reads (#9632).
var dockerCacheToRE = regexp.MustCompile(`^github\.event_name == 'push' && (.+) \|\| ''$`)

// dockerSecretInputs are the build-push-action inputs that hand a secret to
// BuildKit. mode=max exports every layer, so none may appear (#9632).
var dockerSecretInputs = []string{"secrets", "secret-files", "secret-envs", "ssh"}

// isDockerBuild reports whether a step runs docker/build-push-action.
func isDockerBuild(step readinessStep) bool {
	return strings.HasPrefix(step.Uses, "docker/build-push-action@")
}

// checkDockerCacheTo validates one build step's cache-to.
func checkDockerCacheTo(cacheTo string) string {
	match := dockerCacheToRE.FindStringSubmatch(stripExpression(cacheTo))
	if match == nil {
		return "cache-to must be written only on a push event: ${{ github.event_name == 'push' && '<export>' || '' }} (#9632)"
	}
	export := match[1]
	switch {
	case strings.Contains(export, "type=gha"):
		return "cache-to must not use the Actions cache store: its 10 GB evicts the Go build caches (#9632)"
	case !strings.Contains(export, "type=registry"), !strings.Contains(export, dockerCacheRef):
		return "cache-to must export to the " + dockerCacheRef + " registry package (#9632)"
	case !strings.Contains(export, "mode=max"):
		return "cache-to must keep mode=max so the builder stage stays cached (#9632)"
	}
	return ""
}

// checkDockerCacheTag requires one fixed tag per build target: the runtime
// image writes and reads :runtime, each scanner image writes :scanner-<name>
// and reads its own tag. Two targets sharing a tag would overwrite each
// other's cache on every main push (#9632).
func checkDockerCacheTag(step readinessStep) string {
	own := dockerCacheRef + "runtime"
	if step.With["target"] != "" {
		own = dockerCacheRef + "scanner-"
	}
	cacheTo := stripExpression(step.With["cache-to"])
	if !strings.Contains(cacheTo, own) || (own != dockerCacheRef+"runtime" && strings.Contains(cacheTo, dockerCacheRef+"runtime")) {
		return "cache-to must use one fixed tag per target (" + own + ") (#9632)"
	}
	if !strings.Contains(step.With["cache-from"], own) {
		return "cache-from must include the target's own cache tag (" + own + ") (#9632)"
	}
	return ""
}

// checkDockerCacheFrom validates one build step's cache-from lines.
func checkDockerCacheFrom(cacheFrom string) string {
	lines := 0
	for _, line := range strings.Split(cacheFrom, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines++
		if strings.Contains(line, "type=gha") {
			return "cache-from must not read the Actions cache store (#9632)"
		}
		if !strings.HasPrefix(line, "type=registry,ref="+dockerCacheRef) {
			return "cache-from must read the " + dockerCacheRef + " registry package (#9632)"
		}
		if strings.Contains(line, "github.event_name") {
			return "cache-from must not be gated by event: PRs read the cache (#9632)"
		}
	}
	if lines == 0 {
		return "docker build must read the registry build cache (#9632)"
	}
	return ""
}

// checkDockerBuildSecrets requires that no secret can enter an exported layer.
func checkDockerBuildSecrets(step readinessStep) string {
	for _, input := range dockerSecretInputs {
		if _, ok := step.With[input]; ok {
			return "docker build must not pass " + input + ": mode=max exports every layer to a public package (#9632)"
		}
	}
	if strings.Contains(step.With["build-args"], "secrets.") {
		return "docker build-args must not carry a secret: mode=max exports every layer to a public package (#9632)"
	}
	return ""
}

// checkDockerCache applies the #9632 cache rules to every docker build step
// in a workflow's jobs and returns the first problem.
func checkDockerCache(jobs map[string]readinessJob) string {
	builds := 0
	for name, job := range jobs {
		for _, step := range job.Steps {
			if !isDockerBuild(step) {
				continue
			}
			builds++
			for _, check := range []string{
				checkDockerCacheTo(step.With["cache-to"]),
				checkDockerCacheFrom(step.With["cache-from"]),
				checkDockerCacheTag(step),
				checkDockerBuildSecrets(step),
			} {
				if check != "" {
					return name + " " + step.Name + ": " + check
				}
			}
		}
	}
	if builds == 0 {
		return "no docker/build-push-action step found"
	}
	return ""
}

// loadWorkflowJobs parses every job of one workflow file.
func loadWorkflowJobs(t *testing.T, workflow string) map[string]readinessJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", workflow))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Jobs map[string]readinessJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Jobs
}

// TestDockerBuildCacheWiring pins docker-publish's BuildKit cache to the GHCR
// registry package, written on push only (#9632).
func TestDockerBuildCacheWiring(t *testing.T) {
	t.Parallel()
	if problem := checkDockerCache(loadWorkflowJobs(t, "docker-publish.yml")); problem != "" {
		t.Errorf("docker-publish.yml: %s", problem)
	}
}
