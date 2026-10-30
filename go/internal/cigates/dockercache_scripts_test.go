// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptRefRE finds repo-local script references in a run step: a path that
// starts at scripts/ (optionally behind ./, a quote, or a ${VAR}/$(cmd)/
// prefix) and ends in a script extension. It also matches a script passed as
// an argument; that only widens what is scanned.
var scriptRefRE = regexp.MustCompile(`(?:^|[\s"'=(;&|])(?:\./|\$\{?[A-Za-z_][A-Za-z_0-9]*\}?/)?(scripts/[A-Za-z0-9_./-]+\.(?:sh|bash|py))\b`)

// sourceRefRE finds a file a shell script sources: "source <path>" or
// ". <path>" with an optional quote and ${VAR}/ prefix.
var sourceRefRE = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+["']?(?:\$\{?[A-Za-z_][A-Za-z_0-9]*\}?/|\$\(.*?\)/)*([A-Za-z0-9_./-]+\.(?:sh|bash))["']?`)

// cacheWriterPatterns are the forms of a BuildKit cache export or a build
// that could carry one: the cache-to flag or key, and the bake front ends.
var cacheWriterPatterns = []*regexp.Regexp{
	regexp.MustCompile(`cache-to`),
	regexp.MustCompile(`\b(?:docker\s+)?buildx\s+bake\b`),
	regexp.MustCompile(`\bdocker\s+bake\b`),
	regexp.MustCompile(`docker/bake-action`),
}

// scriptCacheWriter reports the first cache-writer pattern a script body
// holds, or "".
func scriptCacheWriter(body string) string {
	for _, pattern := range cacheWriterPatterns {
		if found := pattern.FindString(body); found != "" {
			return found
		}
	}
	return ""
}

// resolveLocalFile returns the repo-local file a reference names, trying the
// repo root and the referring script's directory. It returns "" when no such
// file exists, which is what a variable-built or remote path looks like.
func resolveLocalFile(root, fromDir, ref string) string {
	for _, base := range []string{root, fromDir} {
		candidate := filepath.Join(base, ref)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// checkScriptBody scans one script and the files it sources, one level deep.
func checkScriptBody(root, path string, depth int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	body := string(raw)
	if found := scriptCacheWriter(body); found != "" {
		rel, _ := filepath.Rel(root, path)
		return rel + " contains " + found
	}
	if depth == 0 {
		return ""
	}
	for _, match := range sourceRefRE.FindAllStringSubmatch(body, -1) {
		if sourced := resolveLocalFile(root, filepath.Dir(path), match[1]); sourced != "" {
			if problem := checkScriptBody(root, sourced, depth-1); problem != "" {
				return problem
			}
		}
	}
	return ""
}

// checkRunScriptCacheWriters follows every repo-local script a run step
// references, and the files those scripts source one level deep, and refuses
// any body that exports a BuildKit cache (#9632). It does not cover a script
// fetched from elsewhere, an action pulled from another repository, a path
// built at run time, or a source chain deeper than one level; the inline
// checks in checkOtherCacheWriters and the build-step pins cover the rest.
func checkRunScriptCacheWriters(root string, jobs map[string]readinessJob) string {
	for jobName, job := range jobs {
		for _, step := range job.Steps {
			for _, match := range scriptRefRE.FindAllStringSubmatch(step.Run, -1) {
				script := resolveLocalFile(root, root, match[1])
				if script == "" {
					continue
				}
				if problem := checkScriptBody(root, script, 1); problem != "" {
					return jobName + " " + step.Name + ": run step reaches a build-cache export: " + problem + " (#9632)"
				}
			}
		}
	}
	return ""
}

// writeFixtureTree writes files (path -> body) under a fresh temp root.
func writeFixtureTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestRunScriptCacheWriters proves a run step cannot export a build cache
// through a local script, directly or through one sourced file, and that the
// real workflow's scripts pass (#9632).
func TestRunScriptCacheWriters(t *testing.T) {
	t.Parallel()
	const writer = "docker buildx build --cache-to type=registry,ref=x,mode=max .\n"
	files := map[string]string{
		"scripts/ci/clean.sh":       "#!/usr/bin/env bash\necho hello\n",
		"scripts/ci/writer.sh":      "#!/usr/bin/env bash\n" + writer,
		"scripts/ci/bake.sh":        "#!/usr/bin/env bash\ndocker buildx bake --push\n",
		"scripts/ci/sources.sh":     "#!/usr/bin/env bash\nsource \"${ROOT}/scripts/lib/inner.sh\"\n",
		"scripts/ci/sources-dot.sh": "#!/usr/bin/env bash\n. ../lib/inner.sh\n",
		"scripts/ci/deep.sh":        "#!/usr/bin/env bash\nsource scripts/lib/mid.sh\n",
		"scripts/lib/inner.sh":      "# helper\n" + writer,
		"scripts/lib/mid.sh":        "source scripts/lib/inner.sh\n",
		"scripts/ci/benign.sh":      "#!/usr/bin/env bash\nsource scripts/lib/benign-lib.sh\n",
		"scripts/lib/benign-lib.sh": "echo ok\n",
	}
	root := writeFixtureTree(t, files)
	check := func(run string) string {
		jobs := map[string]readinessJob{"j": {Steps: []readinessStep{{Name: "s", Run: run}}}}
		return checkRunScriptCacheWriters(root, jobs)
	}
	for _, run := range []string{
		"bash scripts/ci/clean.sh",
		"echo no scripts here",
		"bash scripts/ci/benign.sh",
		"bash scripts/ci/missing.sh",
	} {
		if problem := check(run); problem != "" {
			t.Errorf("run %q must pass, got %q", run, problem)
		}
	}
	for _, tc := range []struct{ name, run, want string }{
		{"bash", "bash scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"sh", "sh scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"dot slash", "./scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"source", "source scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"dot source", ". scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"bare path", "scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"variable prefix", "bash \"${GITHUB_WORKSPACE}/scripts/ci/writer.sh\"", "writer.sh contains cache-to"},
		{"after other commands", "set -e\ncd .\nbash scripts/ci/clean.sh && bash scripts/ci/writer.sh", "writer.sh contains cache-to"},
		{"bake", "bash scripts/ci/bake.sh", "bake.sh contains"},
		{"nested source with a variable prefix", "bash scripts/ci/sources.sh", "inner.sh contains cache-to"},
		{"nested dot source relative to the script", "bash scripts/ci/sources-dot.sh", "inner.sh contains cache-to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if problem := check(tc.run); !strings.Contains(problem, tc.want) {
				t.Fatalf("want a problem containing %q, got %q", tc.want, problem)
			}
		})
	}
	// Documented limit: a writer two sources deep is not followed.
	if problem := check("bash scripts/ci/deep.sh"); problem != "" {
		t.Errorf("a chain two sources deep is outside the documented cover, got %q", problem)
	}
}

// TestDockerPublishScriptsExportNoCache runs the script check over the real
// docker-publish.yml, so a script it calls (or sources) that starts to export
// a cache fails here.
func TestDockerPublishScriptsExportNoCache(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	if problem := checkRunScriptCacheWriters(root, loadWorkflowJobs(t, "docker-publish.yml")); problem != "" {
		t.Errorf("docker-publish.yml: %s", problem)
	}
}
