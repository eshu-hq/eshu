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

// setupGoUsesRE matches an actions/setup-go step, at any pinned ref (this
// repo uses tags only: @v6 x44, @v5 x1) after the @.
var setupGoUsesRE = regexp.MustCompile(`(^|/)actions/setup-go(@|$)`)

// goModDownloadRetryRE captures the optional module-dir argument of a
// scripts/ci/go-mod-download-retry.sh invocation. An empty capture means the
// bare form, which defaults to "go". The capture class deliberately excludes
// shell operators (|, ;, &): a bare `\S+` would swallow "||" out of
// `... .sh || true` as if it were a module argument, masking the exit-status
// suppression prewarmSuppressedRE exists to catch.
var goModDownloadRetryRE = regexp.MustCompile(`scripts/ci/go-mod-download-retry\.sh(?:\s+([\w./"'-]+))?`)

// goTouchRE matches a literal `go <verb>` invocation for a verb that touches
// the module graph. moduleTouchingToolRE matches the other module-graph-
// touching tools this repo's CI runs. Neither is load-bearing for
// correctness (see checkJobPrewarmOrdering's doc comment): they only make a
// violation message name what tripped it.
var goTouchRE = regexp.MustCompile(`(?:^|[^\w./-])go\s+(build|test|vet|run|install|list|mod|generate)\b`)

var moduleTouchingToolRE = regexp.MustCompile(`\b(golangci-lint|govulncheck|gosec|nancy)\b`)

// ghExpressionRE matches a GitHub Actions expression token, e.g.
// "${{ matrix.sum }}" -- this check has no workflow-evaluation engine and
// cannot resolve one statically.
var ghExpressionRE = regexp.MustCompile(`\$\{\{.*\}\}`)

// prewarmSuppressedRE matches a shell suffix, immediately after a
// go-mod-download-retry.sh call on the same line, that throws away its exit
// status: "; true", "|| true", or "|| :". The command still runs -- this
// only means a genuine failure (proxy down, all retries exhausted) can no
// longer fail the job, defeating the "fail HERE, loud" purpose the script
// exists for.
var prewarmSuppressedRE = regexp.MustCompile(`^\s*(;\s*true\b|\|\|\s*true\b|\|\|\s*:(\s|$))`)

// prewarmStep is the minimal shape this check reads from a workflow step.
// A separate type from runStep (scriptworkflow.go) -- this check needs
// `uses`/`with`/`if`/`continue-on-error`, which no other check reads.
type prewarmStep struct {
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	With            map[string]string `yaml:"with"`
	Run             string            `yaml:"run"`
	If              string            `yaml:"if"`
	ContinueOnError string            `yaml:"continue-on-error"`
}

type prewarmJob struct {
	Steps []prewarmStep `yaml:"steps"`
}

type prewarmWorkflowFile struct {
	Jobs map[string]prewarmJob `yaml:"jobs"`
}

// prewarmModuleDirFromCacheDependencyPath derives the module directory a
// setup-go cache-dependency-path implies. Returns ok=false when it cannot:
// the key is absent, or its value contains an unresolved ${{ }} expression
// (e.g. a matrix variable). Absent is NOT silently skipped: when unset,
// actions/setup-go v6 defaults cache-dependency-path to a repo-root go.mod
// and v5 to a repo-root go.sum (per each ref's README), and this repo has
// neither at the root, so that default cannot be mapped to a real module. "go.sum" (no
// directory component) resolves to the real, valid module ".".
func prewarmModuleDirFromCacheDependencyPath(cacheDependencyPath string) (dir string, ok bool) {
	p := strings.TrimSpace(cacheDependencyPath)
	if p == "" || ghExpressionRE.MatchString(p) {
		return "", false
	}
	return path.Dir(path.Clean(p)), true
}

// setupGoCachesModule reports whether a setup-go step, given its `with:`
// inputs, restores/saves a module cache at all. actions/setup-go's own
// action.yml (v5 and v6) declares `cache: { default: true }`, so caching is
// ON unless the step sets `cache: false` explicitly.
func setupGoCachesModule(with map[string]string) bool {
	return !strings.EqualFold(strings.TrimSpace(with["cache"]), "false")
}

// unquoteArg strips one layer of matching surrounding quotes from a
// pre-warm module argument, so `go-mod-download-retry.sh "sdk/go/collector"`
// still matches the unquoted module directory this check derives from
// cache-dependency-path.
func unquoteArg(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// prewarmStepIneffective reports why a step that DOES name the right module
// still cannot protect the job, or "" if it can. matchEnd is the byte
// offset immediately after the go-mod-download-retry.sh match in step.Run,
// used to inspect only the rest of that same line for a suppressing suffix.
//
// Limits, deliberately not handled: an arbitrary `if:` expression is not
// evaluated -- only a literal `false`/`${{ false }}` is recognized, since
// evaluating a real expression needs the workflow's runtime context this
// static check does not have. The pre-warm is recognized by the script path
// appearing in the step's run text, not by parsing shell, so a mention that
// never executes it (`echo scripts/ci/go-mod-download-retry.sh`, a `#`
// comment) or a form that swallows its exit status other than the `|| true`,
// `; true` and `|| :` suffixes (`|| echo`, `|| exit 0`, `if ...; then`, a
// trailing `&`) is counted as warming; so is `continue-on-error: ${{ true }}`.
// A composite action (`uses: ./.github/actions/...`) that wraps
// actions/setup-go internally is invisible to this check. None of these
// shapes exist in this repo today.
func prewarmStepIneffective(step prewarmStep, matchEnd int) string {
	if strings.EqualFold(strings.TrimSpace(step.ContinueOnError), "true") {
		return "continue-on-error: true"
	}
	switch strings.ReplaceAll(strings.TrimSpace(step.If), " ", "") {
	case "false", "${{false}}":
		return "if: " + strings.TrimSpace(step.If)
	}
	rest := step.Run[matchEnd:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	if prewarmSuppressedRE.MatchString(rest) {
		return "its exit status is suppressed (" + strings.TrimSpace(rest) + ")"
	}
	return ""
}

// checkSetupGoPrewarmOrdering enforces #6615's F2 invariant: every job that
// restores a setup-go module cache must warm it, via
// scripts/ci/go-mod-download-retry.sh for the matching module, before the
// job's post-step cache save -- because actions/setup-go's cache key has no
// restore-keys and saves only on a miss, so the FIRST job to finish on a key
// owns the save for every other job restoring it (confirmed: job
// 102442878753, 2026-09-09, saved a 14,716,033-byte cache with zero
// `go: downloading` lines -- a fast job with no literal `go <verb>` in its
// own steps).
//
// "Must warm at all", not only an ordering rule: a job with no Go-shaped
// step whatsoever is exactly the failure case above and must still
// pre-warm. goTouchRE/moduleTouchingToolRE only decide what a too-late
// pre-warm's error message names.
//
// Fails closed: a workflow file this check cannot read or parse, or a
// cache-dependency-path it cannot resolve to a module, is reported, not
// skipped.
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
			moduleDir, ok := prewarmModuleDirFromCacheDependencyPath(cacheDependencyPath)
			if !ok {
				errs = append(errs, fmt.Errorf(
					"drift: %s job %q: actions/setup-go step %d caches Go modules but its cache-dependency-path %q cannot be resolved to a module directory -- "+
						"set it to a literal go.sum/go.mod path so this check (and scripts/ci/go-mod-download-retry.sh) can warm the right module",
					workflowFile, jobKey, i, cacheDependencyPath,
				))
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
// does not warm moduleDir before the job's cache save, or "" when a real
// (module-matching, effective) pre-warm appears before any touch.
func findPrewarmOrderingViolation(steps []prewarmStep, moduleDir string) string {
	prewarmSeen := false
	var wrongModuleSeen string
	for _, step := range steps {
		if step.Run == "" {
			continue
		}
		loc := goModDownloadRetryRE.FindStringSubmatchIndex(step.Run)
		if loc == nil {
			if !prewarmSeen {
				if v := touchDescription(step.Run); v != "" {
					return v
				}
			}
			continue
		}
		// A pre-warm call can share its line (or an earlier line of the
		// same multi-line `run: |` block) with a touch that ran first --
		// finding the call anywhere in step.Run is not enough; only the
		// text strictly BEFORE the match can have already run.
		if !prewarmSeen {
			if v := touchDescription(step.Run[:loc[0]]); v != "" {
				return v
			}
		}
		arg := "go"
		if loc[2] >= 0 {
			arg = unquoteArg(step.Run[loc[2]:loc[3]])
		}
		if arg != moduleDir {
			if wrongModuleSeen == "" {
				wrongModuleSeen = arg
			}
			continue
		}
		// Report an ineffective same-module pre-warm immediately, rather
		// than recording it and continuing to scan: it IS the root finding
		// -- a later touch this leaves unprotected is a symptom, and
		// reporting that instead would bury the actual fix (remove the
		// continue-on-error/if:false/suppression) behind an ordering
		// message that does not name it.
		if reason := prewarmStepIneffective(step, loc[1]); reason != "" {
			return fmt.Sprintf("found a pre-warm for %q, but %s", moduleDir, reason)
		}
		prewarmSeen = true
	}
	if prewarmSeen {
		return ""
	}
	if wrongModuleSeen != "" {
		return fmt.Sprintf("no step warms that module -- found a pre-warm for %q instead", wrongModuleSeen)
	}
	return "no step warms that module"
}

// touchDescription reports the first module-graph touch in text, naming the
// exact line it matched on, or "" if none.
func touchDescription(text string) string {
	if loc := goTouchRE.FindStringIndex(text); loc != nil {
		return fmt.Sprintf("step %q runs a Go command before any scripts/ci/go-mod-download-retry.sh step warms that module",
			strings.TrimSpace(lineAt(text, loc[0])))
	}
	if loc := moduleTouchingToolRE.FindStringIndex(text); loc != nil {
		return fmt.Sprintf("step %q runs a module-graph-touching tool before any scripts/ci/go-mod-download-retry.sh step warms that module",
			strings.TrimSpace(lineAt(text, loc[0])))
	}
	return ""
}

// lineAt returns the line of text containing byte offset pos.
func lineAt(text string, pos int) string {
	start := strings.LastIndexByte(text[:pos], '\n') + 1 // -1+1 == 0 when absent
	if end := strings.IndexByte(text[pos:], '\n'); end >= 0 {
		return text[start : pos+end]
	}
	return text[start:]
}
