// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// setupGoUsesRE matches an actions/setup-go step, at any pinned ref (tag,
// SHA, or branch) after the @.
var setupGoUsesRE = regexp.MustCompile(`(^|/)actions/setup-go(@|$)`)

// goModDownloadRetryRE captures the optional module-dir argument of a
// scripts/ci/go-mod-download-retry.sh invocation. An empty capture means the
// bare form, which defaults to "go" (scripts/ci/go-mod-download-retry.sh's
// own contract).
var goModDownloadRetryRE = regexp.MustCompile(`scripts/ci/go-mod-download-retry\.sh(?:\s+(\S+))?`)

// goTouchRE matches a literal `go <verb>` invocation for a verb that touches
// the module graph. moduleTouchingToolRE matches the other module-graph-
// touching tools #6615's audit found in this repo's CI. Neither is load-
// bearing for correctness any more (see checkJobPrewarmOrdering's doc
// comment): they only make a violation message name what tripped it.
var goTouchRE = regexp.MustCompile(`(?:^|[^\w./-])go\s+(build|test|vet|run|install|list|mod|generate)\b`)

var moduleTouchingToolRE = regexp.MustCompile(`\b(golangci-lint|govulncheck|gosec|nancy)\b`)

// prewarmStep is the minimal shape this check reads from a workflow step:
// enough to recognize actions/setup-go and its `cache`/cache-dependency-path
// inputs, and to inspect a `run:` step's command text. A separate type from
// runStep (scriptworkflow.go) rather than extending it -- this check needs
// `uses` and `with`, which no other check in this package reads.
type prewarmStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
	Run  string            `yaml:"run"`
}

type prewarmJob struct {
	Steps []prewarmStep `yaml:"steps"`
}

type prewarmWorkflowFile struct {
	Jobs map[string]prewarmJob `yaml:"jobs"`
}

// prewarmModuleDirFromCacheDependencyPath derives the module directory a
// setup-go cache-dependency-path implies: the directory containing the
// go.sum/go.mod file it names. "go/go.sum" -> "go"; "sdk/go/collector/go.mod"
// -> "sdk/go/collector". A path with no directory component returns "",
// which the caller treats as "cannot derive, skip".
func prewarmModuleDirFromCacheDependencyPath(cacheDependencyPath string) string {
	p := strings.TrimSpace(cacheDependencyPath)
	if p == "" {
		return ""
	}
	dir := path.Dir(path.Clean(p))
	if dir == "." {
		return ""
	}
	return dir
}

// setupGoCachesModule reports whether a setup-go step, given its `with:`
// inputs, restores/saves a module cache at all. actions/setup-go's own
// action.yml (v5 and v6, both pinned refs this repo uses, fetched from
// raw.githubusercontent.com/actions/setup-go/<ref>/action.yml) declares
// `cache: { default: true }`, so caching is ON unless the step sets
// `cache: false` explicitly -- an absent `cache:` key is still cached.
func setupGoCachesModule(with map[string]string) bool {
	return !strings.EqualFold(strings.TrimSpace(with["cache"]), "false")
}

// checkSetupGoPrewarmOrdering enforces #6615's F2 invariant: every job that
// restores a setup-go module cache must warm it, via
// scripts/ci/go-mod-download-retry.sh for the matching module, before the
// job's post-step cache save -- because actions/setup-go's cache key has no
// restore-keys and saves only on a miss, so the FIRST job to finish on a key
// owns the save for every other job restoring it (confirmed: job
// 102442878753, 2026-09-09, saved cache 7497506231 at 14,716,033 bytes with
// zero `go: downloading` lines -- a fast job with no literal `go <verb>` in
// its own steps).
//
// This is a "must warm at all" rule, not only an ordering one: a job with no
// Go-shaped step whatsoever is exactly the failure case above and must still
// pre-warm. goTouchRE/moduleTouchingToolRE below only decide what a
// too-late pre-warm's error message names; a job that never pre-warms is a
// violation regardless of what else it runs.
//
// Fails closed: a workflow file this check cannot read or parse is reported,
// not skipped, since a broken file could otherwise hide an unenforced job.
func checkSetupGoPrewarmOrdering(repoRoot string) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return []error{fmt.Errorf("checkSetupGoPrewarmOrdering: cannot read %s: %w", wfDir, err)}
	}

	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
			continue
		}
		wfPath := filepath.Join(wfDir, name)
		raw, err := os.ReadFile(wfPath) // #nosec G304 -- wfDir-confined, entry name comes from os.ReadDir of that same directory
		if err != nil {
			errs = append(errs, fmt.Errorf("checkSetupGoPrewarmOrdering: read %s: %w", name, err))
			continue
		}
		var wf prewarmWorkflowFile
		if err := yaml.Unmarshal(raw, &wf); err != nil {
			errs = append(errs, fmt.Errorf("checkSetupGoPrewarmOrdering: parse %s: %w", name, err))
			continue
		}
		errs = append(errs, checkJobPrewarmOrdering(name, wf.Jobs)...)
	}
	return errs
}

// checkJobPrewarmOrdering walks jobKeys in sorted order (map iteration is
// randomized in Go; a stable order keeps this check's own error output
// deterministic, which matters for a RED/GREEN fixture diff).
func checkJobPrewarmOrdering(workflowFile string, jobs map[string]prewarmJob) []error {
	jobKeys := make([]string, 0, len(jobs))
	for k := range jobs {
		jobKeys = append(jobKeys, k)
	}
	sort.Strings(jobKeys)

	var errs []error
	for _, jobKey := range jobKeys {
		job := jobs[jobKey]
		for i, step := range job.Steps {
			if !setupGoUsesRE.MatchString(step.Uses) || !setupGoCachesModule(step.With) {
				continue
			}
			cacheDependencyPath := step.With["cache-dependency-path"]
			moduleDir := prewarmModuleDirFromCacheDependencyPath(cacheDependencyPath)
			if moduleDir == "" {
				continue
			}
			if violation := findPrewarmOrderingViolation(job.Steps[i+1:], moduleDir); violation != "" {
				errs = append(errs, fmt.Errorf(
					"drift: %s job %q: actions/setup-go step %d caches %q (module %q), but %s -- "+
						"the FIRST job to finish on this cache key saves it for everyone (setup-go "+
						"has no restore-keys), so an unwarmed job saves an empty entry for the rest",
					workflowFile, jobKey, i, cacheDependencyPath, moduleDir, violation,
				))
			}
		}
	}
	return errs
}

// findPrewarmOrderingViolation returns a non-empty description of why steps
// does not warm moduleDir before the job's cache save: either no matching
// pre-warm step exists at all, or one exists but only after a step that
// already touches the module graph. Returns "" when a matching pre-warm
// appears with no touch before it -- including when nothing after setup-go
// touches the module graph at all, which is still fine ONLY because a
// matching pre-warm was found; a job with neither a touch nor a pre-warm
// falls through to the "never warms" case below.
func findPrewarmOrderingViolation(steps []prewarmStep, moduleDir string) string {
	prewarmSeen := false
	var wrongModuleSeen string
	for _, step := range steps {
		if step.Run == "" {
			continue
		}
		if m := goModDownloadRetryRE.FindStringSubmatch(step.Run); m != nil {
			arg := m[1]
			if arg == "" {
				arg = "go"
			}
			if arg == moduleDir {
				prewarmSeen = true
			} else if wrongModuleSeen == "" {
				wrongModuleSeen = arg
			}
			continue
		}
		if !prewarmSeen {
			if goTouchRE.MatchString(step.Run) {
				return fmt.Sprintf("step %q runs a Go command before any scripts/ci/go-mod-download-retry.sh step warms that module",
					strings.TrimSpace(firstLine(step.Run)))
			}
			if moduleTouchingToolRE.MatchString(step.Run) {
				return fmt.Sprintf("step %q runs a module-graph-touching tool before any scripts/ci/go-mod-download-retry.sh step warms that module",
					strings.TrimSpace(firstLine(step.Run)))
			}
		}
	}
	if prewarmSeen {
		return ""
	}
	if wrongModuleSeen != "" {
		return fmt.Sprintf("no step warms that module -- found a pre-warm for %q instead", wrongModuleSeen)
	}
	return "no step warms that module"
}

// firstLine returns s up to its first newline, so a multi-line `run: |`
// block's error message names the command that actually matched instead of
// dumping the whole block.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
