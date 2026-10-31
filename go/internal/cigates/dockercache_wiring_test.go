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

// cacheExportRE and cacheFromLineRE accept one export and one cache-from line
// each, whole: exactly one type=registry, exactly one ref= at the package, a
// tag with no comma or newline, and nothing after it but ",mode=max" for an
// export and nothing for a read. A second ref=, a second type=, a newline
// inside the literal, or any trailing attribute cannot match, so a target
// cannot also write or read another target's tag (#9632).
var (
	cacheExportRE   = regexp.MustCompile(`^type=registry,ref=` + regexp.QuoteMeta(dockerCacheRef) + `([^,\n]+),mode=max$`)
	cacheFromLineRE = regexp.MustCompile(`^type=registry,ref=` + regexp.QuoteMeta(dockerCacheRef) + `([^,\n]+)$`)
)

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
// may also read :runtime for the shared builder stage. Each export and each
// cache-from line must hold exactly one ref (cacheExportRE, cacheFromLineRE),
// so a prefix, a suffix, a shared static tag, another target's tag, or a
// second ref in the same value fails (#9632).
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
	if ref := cacheExportRE.FindStringSubmatch(export); ref == nil || ref[1] != own {
		return "cache-to must use one fixed tag per target, exactly one ref and exactly " + own + ", then only ,mode=max (#9632)"
	}
	owned := false
	for _, line := range strings.Split(step.With["cache-from"], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ref := cacheFromLineRE.FindStringSubmatch(line)
		switch {
		case ref == nil:
			return "cache-from lines must each hold exactly one ref and nothing after the tag; the target's own cache tag is " + own + " (#9632)"
		case ref[1] == own:
			owned = true
		case ref[1] == "runtime" && own != "runtime":
			// A scanner build also reads the runtime builder-stage cache.
		default:
			return "cache-from may read only the target's own cache tag " + own + " (and :runtime for a scanner), got " + ref[1] + " (#9632)"
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

// checkOtherCacheWriters rejects the forms of a second cache writer that show
// in one step: a local action (its steps are not read here), a bake action,
// and a run step whose own text holds cache-to, cache_to or a bake command.
// Files a run step references are scanned by checkRunScriptCacheWriters.
// Neither check is a proof that no step can export a cache (#9632).
func checkOtherCacheWriters(step readinessStep) string {
	if strings.HasPrefix(step.Uses, "./") {
		return "a local action (" + step.Uses + ") is refused: its steps are not read by this test (#9632)"
	}
	if strings.HasPrefix(step.Uses, "docker/bake-action@") {
		return "docker/bake-action can export a cache outside the pinned build steps (#9632)"
	}
	if found := scriptCacheWriter(step.Run); found != "" {
		return "a run step must not export a build cache (" + found + ") (#9632)"
	}
	return ""
}

// checkDockerCache applies the #9632 cache rules to every docker build step
// in a workflow's jobs and returns the first problem.
func checkDockerCache(jobs map[string]readinessJob) string {
	builds := 0
	for name, job := range jobs {
		if strings.HasPrefix(job.Uses, "./") {
			return name + ": a local reusable workflow (" + job.Uses + ") is refused: its steps are not read by this test (#9632)"
		}
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
