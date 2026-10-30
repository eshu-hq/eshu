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
// written on a push event (main or tag) and nothing on any other event. The
// expression is exactly one push guard, one quoted literal or one
// format(<literal>, matrix.component) call, and an empty fallback. No other
// "||" branch, "&&" operand, event name or second expression can match, so a
// pull_request run (including test.yml's workflow_call, whose event is the
// caller's) and workflow_dispatch evaluate to an empty cache-to and a PR,
// fork or not, never writes a cache that main reads (#9632).
var dockerCacheToRE = regexp.MustCompile(
	`^github\.event_name == 'push' && ('[^']*'|format\('[^']*', matrix\.component\)) \|\| ''$`)

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
		return "cache-to must be written only on a push event, in exactly this shape: ${{ github.event_name == 'push' && '<export>' || '' }} (#9632)"
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

// dockerCacheToExportRE pulls the export literal, and the optional format()
// argument, out of a cache-to expression that already matched dockerCacheToRE.
var dockerCacheToExportRE = regexp.MustCompile(`'([^']*)'(?:, (matrix\.component))?\)?\s*\|\|\s*''$`)

// cacheRefTag returns the tag of the first ref=<dockerCacheRef><tag> in an
// export or cache-from line: the text after the package name up to the next
// comma. It returns ok=false when the line holds no such ref.
func cacheRefTag(line string) (string, bool) {
	_, after, found := strings.Cut(line, "ref="+dockerCacheRef)
	if !found {
		return "", false
	}
	tag, _, _ := strings.Cut(after, ",")
	return strings.TrimSpace(tag), true
}

// ownCacheTag is the one tag a build step may write and must read: the
// runtime image uses runtime; a step with a target uses the target itself
// (scanner-${{ matrix.component }}), so every component has its own tag.
func ownCacheTag(step readinessStep) string {
	if target := step.With["target"]; target != "" {
		return target
	}
	return "runtime"
}

// checkDockerCacheTag requires one fixed tag per build target, compared for
// exact equality: the runtime image writes and reads :runtime, each scanner
// image writes :scanner-<component> and reads that tag, and a scanner build
// may also read :runtime for the shared builder stage. A prefix, a suffix, a
// shared static tag or another target's tag would let two targets overwrite
// each other's cache on every main push (#9632).
func checkDockerCacheTag(step readinessStep) string {
	own := ownCacheTag(step)
	match := dockerCacheToExportRE.FindStringSubmatch(stripExpression(step.With["cache-to"]))
	if match == nil {
		return "cache-to must use one fixed tag per target (" + own + ") (#9632)"
	}
	export := match[1]
	if match[2] != "" {
		export = strings.ReplaceAll(export, "{0}", "${{ "+match[2]+" }}")
	}
	if tag, ok := cacheRefTag(export); !ok || tag != own {
		return "cache-to must use one fixed tag per target, exactly " + own + " (#9632)"
	}
	owned := false
	for _, line := range strings.Split(step.With["cache-from"], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tag, ok := cacheRefTag(line)
		switch {
		case !ok:
			return "cache-from must include the target's own cache tag, exactly " + own + " (#9632)"
		case tag == own:
			owned = true
		case tag == "runtime" && own != "runtime":
			// A scanner build also reads the runtime builder-stage cache.
		default:
			return "cache-from may read only the target's own cache tag " + own + " (and :runtime for a scanner), got " + tag + " (#9632)"
		}
	}
	if !owned {
		return "cache-from must include the target's own cache tag, exactly " + own + " (#9632)"
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
			return "docker build must not pass " + input + ": mode=max exports every layer to a package that anonymous PR readers must be able to pull, so it is treated as public (#9632)"
		}
	}
	if strings.Contains(step.With["build-args"], "secrets.") {
		return "docker build-args must not carry a secret: mode=max exports every layer to a package that anonymous PR readers must be able to pull, so it is treated as public (#9632)"
	}
	return ""
}

// checkOtherCacheWriters rejects any step outside the two pinned build steps
// that could export a BuildKit cache: a bake action or a shell build with
// --cache-to would bypass the push-only gate (#9632).
func checkOtherCacheWriters(step readinessStep) string {
	if strings.HasPrefix(step.Uses, "docker/bake-action@") {
		return "docker/bake-action can export a cache outside the pinned build steps (#9632)"
	}
	if strings.Contains(step.Run, "cache-to") {
		return "a run step must not export a build cache with --cache-to (#9632)"
	}
	return ""
}

// checkDockerCache applies the #9632 cache rules to every docker build step
// in a workflow's jobs and returns the first problem.
func checkDockerCache(jobs map[string]readinessJob) string {
	builds := 0
	for name, job := range jobs {
		for _, step := range job.Steps {
			if problem := checkOtherCacheWriters(step); problem != "" {
				return name + " " + step.Name + ": " + problem
			}
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

// parseWorkflowJobs parses every job of one workflow file. A duplicate YAML
// key (a second cache-to on one step) is a parse error, not a silent
// last-one-wins.
func parseWorkflowJobs(raw []byte) (map[string]readinessJob, error) {
	var parsed struct {
		Jobs map[string]readinessJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Jobs, nil
}

// loadWorkflowJobs parses every job of one workflow file in .github/workflows.
func loadWorkflowJobs(t *testing.T, workflow string) map[string]readinessJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", workflow))
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := parseWorkflowJobs(raw)
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

// TestDockerBuildCacheWiring pins docker-publish's BuildKit cache to the GHCR
// registry package, written on push only (#9632).
func TestDockerBuildCacheWiring(t *testing.T) {
	t.Parallel()
	if problem := checkDockerCache(loadWorkflowJobs(t, "docker-publish.yml")); problem != "" {
		t.Errorf("docker-publish.yml: %s", problem)
	}
}
