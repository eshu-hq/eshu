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
// the module graph (compiles, tests, or resolves it) -- the same verb set
// #6615's F1 audit used. It does not match `go version`, `go env`, or other
// verbs that never download a module.
var goTouchRE = regexp.MustCompile(`(?:^|[^\w./-])go\s+(build|test|vet|run|install|list|mod|generate)\b`)

// moduleTouchingToolRE matches the other module-graph-touching invocations
// #6615's audit found in this repo's CI: golangci-lint (which type-checks
// the whole module before linting) and the three security scanners that
// resolve the dependency graph (govulncheck -scan package, gosec's SSA
// loader, nancy's `go list -json -deps` pipe).
var moduleTouchingToolRE = regexp.MustCompile(`\b(golangci-lint|govulncheck|gosec|nancy)\b`)

// prewarmStep is the minimal shape this check reads from a workflow step:
// enough to recognize actions/setup-go and its cache-dependency-path input,
// and to inspect a `run:` step's command text. A separate type from runStep
// (scriptworkflow.go) rather than extending it -- this check needs `uses`
// and `with`, which no other check in this package reads, and keeping the
// YAML shape scoped to this file's one purpose is cheaper than auditing
// every other checkXxx that unmarshals a runStep for a `with:` regression.
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
// -> "sdk/go/collector". A path with no directory component (bare "go.sum")
// returns "", which the caller treats as "cannot derive, skip" -- no
// workflow in this repo does that today, and guessing "." would be wrong for
// the one case (a bare go.sum at the repo root) this repo does not have.
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

// checkSetupGoPrewarmOrdering is #6615's closing item: every job that
// restores a setup-go module cache must warm GOMODCACHE, via
// scripts/ci/go-mod-download-retry.sh, before the first step that actually
// touches the module graph -- not merely SOMEWHERE in the job, which is all
// the #6615 branch's first review pass verified (a count, not an order).
//
// Why this is a real invariant and not a style preference: actions/setup-go
// (the version pinned in this repo) builds one cache key from
// cache-dependency-path with NO restore-keys, and saves only on a cache
// miss -- so after any go.sum/go.mod change, the FIRST job to finish on that
// key owns the save, with whatever GOMODCACHE it happened to populate. If
// that job never warmed the cache for real (a docs-only gate that merely
// shares the go-core job template, for example), it saves a near-empty
// entry and every other job restoring that key re-downloads from the proxy
// until a warmed job finishes first and re-saves it. Measured on this repo:
// job 102442878753 (2026-09-09, "Verify factschema-diff test mirror") saved
// cache 7497506231 at 14,716,033 bytes with zero `go: downloading` lines --
// the empty-cache case, caught in the act. A pre-warm step BEFORE the first
// module-graph touch is what keeps every job's own cache-save populated,
// regardless of which job happens to finish first.
//
// Scope: every actions/setup-go step, in every job, whose
// cache-dependency-path names a go.sum or go.mod. This is deliberately wider
// than "only go/go.sum": the same no-restore-keys race applies to every
// distinct cache-dependency-path this repo uses (sdk/go/collector/go.mod,
// sdk/go/factschema/go.sum, examples/collector-extensions/scorecard/go.sum),
// each its own independent cache key with the identical "first job to
// finish owns the save" exposure -- there is no reason the go/go.sum key
// would be special. tools/golangci-lint-filelength and
// tools/golangci-lint-dirgate do NOT get their own rule here: neither has
// its own actions/setup-go step (both build inside go-core's single go/
// setup-go job), so there is no separate cache key or setup-go step for
// this check to anchor on -- go-core's own go/go.sum pre-warm is what this
// check requires there, and the two tool-module pre-warm steps #6615 added
// alongside it are a network-reliability improvement this check does not
// itself mandate.
//
// The required pre-warm form is derived from the cache-dependency-path, not
// hardcoded to "go": a job whose setup-go step caches
// sdk/go/collector/go.mod must run
// "scripts/ci/go-mod-download-retry.sh sdk/go/collector", not the bare
// (go/-defaulting) form -- the bare form would warm the WRONG module's cache
// and this check would otherwise pass it as if it warmed the one setup-go
// actually restored.
//
// Narrowing, stated plainly rather than left as a silent gap (#5762 review
// convention this package holds itself to): this check recognizes a
// module-graph touch only as a literal `go <verb>` (build/test/vet/run/
// install/list/mod/generate) or one of golangci-lint/govulncheck/gosec/nancy
// appearing directly in a step's own `run:` text. It does NOT trace into a
// scripts/*.sh a step invokes, at any depth -- unlike
// checkCIScriptTriggerCoverage's script-sourcing walk, this check is about
// STEP ORDER within one job, and every module-graph-touching step in this
// repo's workflows names its go/golangci-lint/scanner invocation directly in
// its own `run:` line (confirmed by the #6615 F1 audit's deeper script
// trace, which found its two real gaps script-side, not workflow-step-side).
// A future job that buries its only go-touch inside a called script, with no
// literal verb in the workflow step itself, is invisible to this check the
// same way it was invisible to F1's first pass; F1's own scratch tool -- not
// committed, not this check -- is the deeper trace for that shape.
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
			continue
		}
		var wf prewarmWorkflowFile
		if yaml.Unmarshal(raw, &wf) != nil {
			continue
		}
		errs = append(errs, checkJobPrewarmOrdering(name, wf.Jobs)...)
	}
	return errs
}

// checkJobPrewarmOrdering walks jobKeys in sorted order (map iteration is
// randomized in Go; a stable order keeps this check's own error output
// deterministic across runs, which matters for a RED/GREEN fixture diff).
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
			if !setupGoUsesRE.MatchString(step.Uses) {
				continue
			}
			cacheDependencyPath := step.With["cache-dependency-path"]
			moduleDir := prewarmModuleDirFromCacheDependencyPath(cacheDependencyPath)
			if moduleDir == "" {
				continue
			}
			if violation := findPrewarmOrderingViolation(job.Steps[i+1:], moduleDir); violation != "" {
				errs = append(errs, fmt.Errorf(
					"drift: %s job %q: actions/setup-go step %d caches %q (module %q), but %s "+
						"before any scripts/ci/go-mod-download-retry.sh step warms that module -- "+
						"the FIRST job to finish on this cache key saves it for everyone (setup-go "+
						"has no restore-keys), so an unwarmed job saves an empty entry for the rest",
					workflowFile, jobKey, i, cacheDependencyPath, moduleDir, violation,
				))
			}
		}
	}
	return errs
}

// findPrewarmOrderingViolation returns a non-empty description of the first
// module-graph touch in steps that occurs before a matching pre-warm step,
// or "" if a matching pre-warm step appears first (or no touch exists at
// all -- a setup-go step with no Go-invoking step after it has nothing to
// order against, and is not itself a violation of this rule).
func findPrewarmOrderingViolation(steps []prewarmStep, moduleDir string) string {
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
				return "" // matching pre-warm found before any touch below
			}
			// A pre-warm for a DIFFERENT module does not satisfy this
			// setup-go step's own module and does not count as "seen" --
			// keep scanning; a later step may still warm the right one.
			continue
		}
		if goTouchRE.MatchString(step.Run) {
			return fmt.Sprintf("step %q runs a Go command", strings.TrimSpace(firstLine(step.Run)))
		}
		if moduleTouchingToolRE.MatchString(step.Run) {
			return fmt.Sprintf("step %q runs a module-graph-touching tool", strings.TrimSpace(firstLine(step.Run)))
		}
	}
	return ""
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
