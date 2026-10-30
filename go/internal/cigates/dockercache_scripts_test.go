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

// maxScriptHops bounds how far the check follows scripts: files a run step
// names are level 1, files those name are level 2, and so on to this level.
// A visited set ends cycles. A writer deeper than this is outside the cover.
const maxScriptHops = 4

// The reference patterns find repo-local files a run step or a script names.
// All three over-include on purpose: a token that is not an existing file is
// dropped when it fails to resolve, and an extra file only widens the scan.
var (
	// fileTokenRE: any path-like token with a script or YAML extension.
	fileTokenRE = regexp.MustCompile(`[A-Za-z0-9_./-]+\.(?:sh|bash|py|ya?ml)\b`)
	// interpreterRefRE: the path after bash, sh, zsh, source, exec or a lone
	// ".", with options, quotes and a ${VAR}/ or $(cmd)/ prefix skipped. It
	// is how an extensionless script is recognised.
	interpreterRefRE = regexp.MustCompile(`(?:^|[\s;&|(])(?:bash|sh|zsh|source|exec|\.)[ \t]+(?:-[A-Za-z]+[ \t]+)*["']?(?:\$\{?[A-Za-z_][A-Za-z_0-9]*\}?/|\$\([^)]*\)/)*(\.{0,2}/?[A-Za-z0-9_./-]+)`)
	// dotSlashRE: a command run as ./path.
	dotSlashRE = regexp.MustCompile(`(?:^|[\s;&|("'])(\./[A-Za-z0-9_./-]+)`)
)

// cacheWriterPatterns are the forms of a BuildKit cache export, or of a build
// front end that carries one: the cache-to flag or the compose cache_to key,
// and the bake commands.
var cacheWriterPatterns = []*regexp.Regexp{
	regexp.MustCompile(`cache[-_]to`),
	regexp.MustCompile(`\b(?:docker\s+)?buildx\s+bake\b`),
	regexp.MustCompile(`\bdocker\s+bake\b`),
	regexp.MustCompile(`docker/bake-action`),
}

// scriptCacheWriter reports the first cache-writer pattern a body holds, or "".
func scriptCacheWriter(body string) string {
	for _, pattern := range cacheWriterPatterns {
		if found := pattern.FindString(body); found != "" {
			return found
		}
	}
	return ""
}

// referencedFiles returns the repo-local files a body names, resolved against
// the repo root and the referring file's directory. Workflow files are never
// followed: they hold the pinned build steps and legitimately say cache-to.
func referencedFiles(root, fromDir, body string) []string {
	var refs []string
	for _, re := range []*regexp.Regexp{fileTokenRE, interpreterRefRE, dotSlashRE} {
		for _, match := range re.FindAllStringSubmatch(body, -1) {
			ref := match[len(match)-1]
			if re == fileTokenRE {
				ref = match[0]
			}
			refs = append(refs, ref)
		}
	}
	var files []string
	for _, ref := range refs {
		ref = strings.TrimLeft(strings.TrimPrefix(ref, "./"), "/")
		for _, base := range []string{root, fromDir} {
			candidate := filepath.Join(base, ref)
			rel, err := filepath.Rel(root, candidate)
			if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, ".github/workflows/") {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Size() < 2<<20 {
				files = append(files, candidate)
				break
			}
		}
	}
	return files
}

// scanFileTree scans one file and then the files it names, down to
// maxScriptHops levels, ending cycles with the visited set.
func scanFileTree(root, path string, level int, visited map[string]bool) string {
	if visited[path] {
		return ""
	}
	visited[path] = true
	raw, err := os.ReadFile(path)
	if err != nil || strings.IndexByte(string(raw), 0) >= 0 {
		return ""
	}
	body := string(raw)
	if found := scriptCacheWriter(body); found != "" {
		rel, _ := filepath.Rel(root, path)
		return rel + " contains " + found
	}
	if level >= maxScriptHops {
		return ""
	}
	for _, next := range referencedFiles(root, filepath.Dir(path), body) {
		if problem := scanFileTree(root, next, level+1, visited); problem != "" {
			return problem
		}
	}
	return ""
}

// checkRunScriptCacheWriters follows the repo-local files each run step names
// (scripts and compose-style YAML, however they are invoked: bash, sh, ./,
// source, ., exec, a bare path, or a quoted token), and the files those name,
// to maxScriptHops levels, and refuses any body that holds cache-to or
// cache_to or a bake command (#9632). It is a text scan of files that exist
// in the repository. It does not cover eval or bash -c built commands, make
// targets, variable-built or split flags, scripts fetched at run time, a
// path built at run time, an action from another repository, a chain deeper
// than maxScriptHops, or any workflow file.
func checkRunScriptCacheWriters(root string, jobs map[string]readinessJob) string {
	for jobName, job := range jobs {
		for _, step := range job.Steps {
			visited := map[string]bool{}
			for _, script := range referencedFiles(root, root, step.Run) {
				if problem := scanFileTree(root, script, 1, visited); problem != "" {
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
// through a repo-local script reached by any of the supported forms, directly
// or through scripts it sources or runs, and that clean and out-of-reach
// inputs pass (#9632).
func TestRunScriptCacheWriters(t *testing.T) {
	t.Parallel()
	const writer = "docker buildx build --cache-to type=registry,ref=x,mode=max .\n"
	files := map[string]string{
		"scripts/ci/clean.sh":         "#!/usr/bin/env bash\necho hello\n",
		"scripts/ci/writer.sh":        "#!/usr/bin/env bash\n" + writer,
		"scripts/ci/w.sh":             "#!/usr/bin/env bash\n" + writer,
		"scripts/ci/noext":            "#!/usr/bin/env bash\n" + writer,
		"scripts/ci/under.sh":         "#!/usr/bin/env bash\ncache_to: [type=registry]\n",
		"scripts/ci/bake.sh":          "#!/usr/bin/env bash\ndocker buildx bake --push\n",
		"scripts/ci/sources.sh":       "#!/usr/bin/env bash\nsource \"${ROOT}/scripts/lib/inner.sh\"\n",
		"scripts/ci/sources-dot.sh":   "#!/usr/bin/env bash\n. ../lib/inner.sh\n",
		"scripts/ci/calls.sh":         "#!/usr/bin/env bash\nbash scripts/ci/w.sh\n",
		"scripts/ci/calls-rel.sh":     "#!/usr/bin/env bash\n\"$(dirname \"$0\")/w.sh\"\n",
		"scripts/ci/calls-noext.sh":   "#!/usr/bin/env bash\nsh scripts/ci/noext\n",
		"scripts/ci/execs.sh":         "#!/usr/bin/env bash\nexec scripts/ci/w.sh\n",
		"scripts/ci/evals.sh":         "#!/usr/bin/env bash\neval \"$(cat scripts/ci/w.sh)\"\n",
		"scripts/ci/compose-call.sh":  "#!/usr/bin/env bash\ndocker compose -f deploy/compose.yml build\n",
		"scripts/ci/l1.sh":            "bash scripts/ci/l2.sh\n",
		"scripts/ci/l2.sh":            "bash scripts/ci/l3.sh\n",
		"scripts/ci/l3.sh":            "bash scripts/ci/w4.sh\n",
		"scripts/ci/w4.sh":            writer,
		"scripts/ci/m1.sh":            "bash scripts/ci/m2.sh\n",
		"scripts/ci/m2.sh":            "bash scripts/ci/m3.sh\n",
		"scripts/ci/m3.sh":            "bash scripts/ci/m4.sh\n",
		"scripts/ci/m4.sh":            "bash scripts/ci/w5.sh\n",
		"scripts/ci/w5.sh":            writer,
		"scripts/ci/cyc-a.sh":         "bash scripts/ci/cyc-b.sh\n",
		"scripts/ci/cyc-b.sh":         "bash scripts/ci/cyc-a.sh\n",
		"scripts/lib/inner.sh":        "# helper\n" + writer,
		"scripts/ci/benign.sh":        "#!/usr/bin/env bash\nsource scripts/lib/benign-lib.sh\n",
		"scripts/lib/benign-lib.sh":   "echo ok\n",
		".github/scripts/w.sh":        writer,
		"tools/w.sh":                  writer,
		"deploy/compose.yml":          "services:\n  app:\n    build:\n      cache_to:\n        - type=registry,ref=x\n",
		"deploy/clean-compose.yml":    "services:\n  app:\n    build:\n      cache_from:\n        - type=registry,ref=x\n",
		".github/workflows/other.yml": "# a workflow file is not a compose file: cache-to here is pinned elsewhere\n" + writer,
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
		"bash scripts/ci/cyc-a.sh",
		"docker compose -f deploy/clean-compose.yml build",
		"yq . .github/workflows/other.yml",
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
		{"script run by a followed script", "bash scripts/ci/calls.sh", "w.sh contains cache-to"},
		{"script run through dirname", "bash scripts/ci/calls-rel.sh", "w.sh contains cache-to"},
		{"exec of a script", "bash scripts/ci/execs.sh", "w.sh contains cache-to"},
		{"eval of a script body", "bash scripts/ci/evals.sh", "w.sh contains cache-to"},
		{"extensionless script", "bash scripts/ci/noext", "noext contains cache-to"},
		{"extensionless script run by a followed script", "bash scripts/ci/calls-noext.sh", "noext contains cache-to"},
		{"script outside scripts/", "bash tools/w.sh", "w.sh contains cache-to"},
		{"script under .github/scripts", "bash .github/scripts/w.sh", "w.sh contains cache-to"},
		{"underscore compose key", "bash scripts/ci/under.sh", "under.sh contains cache_to"},
		{"compose file named by the step", "docker compose -f deploy/compose.yml build", "compose.yml contains cache_to"},
		{"compose file named by a followed script", "bash scripts/ci/compose-call.sh", "compose.yml contains cache_to"},
		{"writer four scripts deep", "bash scripts/ci/l1.sh", "w4.sh contains cache-to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if problem := check(tc.run); !strings.Contains(problem, tc.want) {
				t.Fatalf("want a problem containing %q, got %q", tc.want, problem)
			}
		})
	}
	// Documented limits: a writer five scripts deep, a path built at run
	// time, and a flag split across a quote are outside the cover.
	for _, run := range []string{
		"bash scripts/ci/m1.sh",
		"D=scripts/ci; bash \"$D/clean-dir/w.sh\"",
	} {
		if problem := check(run); problem != "" {
			t.Errorf("run %q is outside the documented cover, got %q", run, problem)
		}
	}
}

// TestLocalActionsAreRefused proves a local composite action or reusable
// workflow, whose steps this test does not read, is refused in the covered
// workflow (#9632).
func TestLocalActionsAreRefused(t *testing.T) {
	t.Parallel()
	clean := loadWorkflowJobs(t, "docker-publish.yml")
	for _, tc := range []struct {
		name   string
		mutate func(map[string]readinessJob)
		want   string
	}{
		{"local composite action step", func(jobs map[string]readinessJob) {
			job := jobs["build-and-push-image"]
			job.Steps = append(job.Steps, readinessStep{Name: "Local", Uses: "./.github/actions/build"})
			jobs["build-and-push-image"] = job
		}, "local action"},
		{"local reusable workflow job", func(jobs map[string]readinessJob) {
			jobs["extra"] = readinessJob{Uses: "./.github/workflows/build.yml"}
		}, "local reusable workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := make(map[string]readinessJob, len(clean))
			for name, j := range clean {
				copyJobSteps(&j)
				jobs[name] = j
			}
			tc.mutate(jobs)
			if problem := checkDockerCache(jobs); !strings.Contains(problem, tc.want) {
				t.Fatalf("want a problem containing %q, got %q", tc.want, problem)
			}
		})
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
