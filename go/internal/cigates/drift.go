// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// appendGateDisplayRE captures the display argument (3rd positional) of an
// append_gate call in a matrix-dispatch workflow such as
// static-contract-gates.yml: append_gate "<selected>" "<key>" "<display>" ...
// The display is the concrete GitHub check name for a matrix job whose own
// name is a ${{ matrix.display }} expression.
var appendGateDisplayRE = regexp.MustCompile(`append_gate\s+"[^"]*"\s+"[^"]*"\s+"([^"]*)"`)

var matrixVariableRE = regexp.MustCompile(`\$\{\{\s*matrix\.([A-Za-z0-9_]+)\s*\}\}`)

// DriftCheck validates that .pre-commit-config.yaml and .github/workflows/ are
// consistent with the gate registry. It accumulates all errors rather than
// stopping at the first; a nil or empty slice means the tree is drift-free.
//
// Scope: this reconciles the two surfaces with discrete, enumerable entries —
// pre-commit hooks and workflow files. Reconciling scripts/dev/pre-pr.sh's step
// set against the registry is #4214, which replaces pre-pr.sh's hard-coded steps
// with the registry-driven gate selector; until then pre-pr.sh is only a trigger
// for re-running this check, not a surface it parses.
//
// Checks performed (#4220 AC):
//
//  1. Hook → registry/hygiene: every local repo hook id in
//     .pre-commit-config.yaml must be either (a) the hook_id of some gate or
//     (b) listed in hygiene_hooks. Anything else is an unregistered hook error.
//
//  2. Gate hook_id → present + stage match: for every gate whose hook_id is
//     non-empty, the hook must exist in .pre-commit-config.yaml and its stages
//     must be consistent with the gate tier (pre-commit gate → hook stage
//     includes "pre-commit" or "default"; pre-push gate → includes "pre-push").
//
//  3. Workflow ↔ registry completeness: each .github/workflows/*.yml file must
//     be EITHER referenced by ≥1 gate ci.workflow OR listed in
//     non_gate_workflows. A file in neither is an error. A non_gate_workflows
//     entry whose file is missing on disk is a stale-entry error. A workflow
//     file present in both a gate ci.workflow AND non_gate_workflows is an error.
//
//  4. ci.job → workflow check-name resolution (#5010): every gate's ci.job must
//     name a real check in its ci.workflow — a job `name:`, a job key, or an
//     append_gate display — not the workflow title. See checkJobNamesResolve for
//     the membership-vs-correspondence and matrix-job caveats.
//
//  5. Registry trigger → CI path-filter coverage (#5855): for a gate whose
//     ci.workflow uses a dorny/paths-filter matrix dispatch and whose ci.job
//     resolves to a dorny filter key via an append_gate call, every LITERAL
//     (non-glob) registry trigger must be matched by that key's filter glob
//     under dorny's own gitignore-style semantics. See checkPathFilterCoverage
//     for the skip conditions (glob-form triggers, non-matrix workflows, and
//     gates whose ci.job does not resolve to a filter key are all skipped
//     rather than false-flagged).
//
//  6. Verify-script → ci.workflow correspondence (#5748): when a gate's
//     scripts/verify-*.sh is executed by exactly one workflow, the gate must
//     declare that workflow. Only executable `run:` blocks count as executing
//     it — a dorny/paths-filter entry watches a path rather than invoking it,
//     and counting such a mention gives the script a phantom second owner that
//     silently skips the gate. See checkVerifyScriptWorkflowMatch for why this
//     is deliberately narrower than "the declared job runs the gate's local
//     command": local and CI entrypoints legitimately differ, and a one-time
//     #5748 measurement of that broader (never-implemented) rule found it
//     flagged a double digit number of gates, nearly all of them legitimately
//     wired rather than broken. A script no workflow runs, or several run,
//     carries no correspondence signal and is skipped.
//
//  7. Trivy skip-dirs wiring: scripts/lib/trivy-skip-dirs.sh (the single
//     shared skip-dirs derivation), scripts/dev/trivy-fs-local.sh, and
//     security-scan.yml's trivy-fs job must all be provably wired to
//     specs/trivy-skip-dirs.txt, the single authoritative skip-dirs list —
//     the helper reading it, and the other two invoking the helper rather
//     than deriving or hard-coding their own value. This proves wiring, not
//     value flow: see checkTrivySkipDirsParity for the four assertions this
//     makes and the boundary it deliberately stops at.
//
//  8. Gate script → own trigger coverage (#5762): every scripts/ token in a
//     gate's local.command and local.test_command — and every scripts/ file
//     those source, at any depth — must be matched by one of that gate's own
//     triggers, so a PR editing only the verifier, a chained second script, or
//     a case file sourced two levels deep still selects the gate locally
//     instead of first failing in CI. See checkScriptTriggerCoverage.
//
//  9. Required-status manifest and aggregator contract: the manifest and its
//     trusted aggregator workflow must satisfy the source, trigger,
//     permission, checkout, secret, and publisher-command rules. See
//     checkRequiredStatusWorkflows.
//
//  10. Gate Go package → own trigger coverage (#5873): check 8's rule for the
//     gates implemented as a Go package rather than a scripts/ file. Every
//     package a gate's local.command or local.test_command builds, runs,
//     generates, or tests must be matched by one of that gate's own triggers,
//     and by a single trigger that spans the whole package directory. Check 8
//     is structurally blind to these — it only derives scripts/-prefixed
//     tokens. See checkGoPackageTriggerCoverage.
//
//  11. Gate CI-job script → own trigger coverage (#6149 follow-up): check 8's
//     rule extended to the script CI actually executes, not only the local
//     static mirror check 8 walks. Some gates' local.command runs a static
//     mirror of a separate "live driver" script that CI invokes directly (for
//     example ifa-determinism's local.command runs
//     scripts/test-verify-ifa-determinism.sh while its CI job runs
//     scripts/verify-ifa-determinism.sh) — check 8's walk never reaches that
//     live driver or anything it sources, at any depth. Every scripts/ token
//     in a gate's resolved CI job's `run:` steps — and every scripts/ file
//     those source, at any depth — must be matched by one of that gate's own
//     triggers, the same rule and the same narrowings check 8 applies, just
//     rooted at the CI job instead of local.command. Skips any (ci.workflow,
//     ci.job) pair shared by more than one gate: a shared job (multiple
//     gates in the committed registry point at test.yml's verify-contracts
//     backstop job alone -- see CIScriptTriggerCoverageSummary for the live
//     count rather than a number restated here) carries no per-gate
//     attribution over which step belongs to which gate, so guessing
//     produced 352 false positives, measured once against the committed
//     registry at the time this narrowing was added, before this narrowing
//     existed. See checkCIScriptTriggerCoverage.
func DriftCheck(repoRoot string, reg *Registry) []error {
	var errs []error

	hooks, hookErrs := parsePreCommitHooks(repoRoot)
	errs = append(errs, hookErrs...)
	if len(hookErrs) > 0 {
		// Cannot continue hook checks if the file could not be parsed.
		return errs
	}

	errs = append(errs, checkHookRegistration(hooks, reg)...)
	errs = append(errs, checkGateHookIDs(hooks, reg)...)
	errs = append(errs, checkWorkflowCompleteness(repoRoot, reg)...)
	errs = append(errs, checkJobNamesResolve(repoRoot, reg)...)
	errs = append(errs, checkPathFilterCoverage(repoRoot, reg)...)
	errs = append(errs, checkVerifyScriptWorkflowMatch(repoRoot, reg)...)
	errs = append(errs, checkTrivySkipDirsParity(repoRoot)...)
	errs = append(errs, checkScriptTriggerCoverage(repoRoot, reg)...)
	errs = append(errs, checkRequiredStatusWorkflows(repoRoot, reg)...)
	errs = append(errs, checkGoPackageTriggerCoverage(reg)...)
	errs = append(errs, checkCIScriptTriggerCoverage(repoRoot, reg)...)

	return errs
}

// checkJobNamesResolve validates that every gate's ci.job names a real check in
// its ci.workflow — either a job's `name:` (or job key) or an append_gate
// display in a matrix-dispatch workflow. It closes the gap that let a gate name
// the workflow TITLE instead of the check name (issue #5010): the completeness
// check only verifies the workflow FILE exists, so a title/check mismatch passed
// the drift gate silently. A workflow whose check names cannot be resolved (parse
// failure, no jobs, no append_gate) is skipped rather than reported, so this
// never false-positives on a workflow shape it does not understand.
//
// Two limitations are intentional and out of #5010's scope. (1) It proves
// MEMBERSHIP, not CORRESPONDENCE: a gate cross-wired to the wrong-but-existing
// check in the same workflow still passes — it catches "names the workflow
// title / a step / a non-existent job", not "names a sibling gate's job".
// (2) For a matrix job whose name is a ${{ matrix.* }} expression (e.g.
// go-race, corpus-gate), the real per-cell checks are "go-race (1)",
// "corpus-gate (nornicdb)", etc.; ci.job is validated against the job id by
// convention (the umbrella/prefix), so a gate should name the stable umbrella
// job (go-race-complete) or the job id, not a per-cell name.
func checkJobNamesResolve(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	cache := make(map[string]map[string]struct{})
	concreteCache := make(map[string]map[string]struct{})
	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		names, cached := cache[g.CI.Workflow]
		if !cached {
			// filepath.Base strips any path component so the read is confined to
			// wfDir; the filename itself comes from the committed ci-gates.v1.yaml
			// registry, not runtime input.
			wfPath := filepath.Join(wfDir, filepath.Base(g.CI.Workflow))
			raw, err := os.ReadFile(wfPath) // #nosec G304 -- wfDir-confined path; filename is registry-controlled, not runtime input
			if err != nil {
				// Missing/unreadable workflow is reported by
				// checkWorkflowCompleteness; do not double-report here.
				names = nil
			} else {
				names = workflowCheckNames(raw)
				concreteCache[g.CI.Workflow] = workflowConcreteCheckNames(raw)
			}
			cache[g.CI.Workflow] = names
		}
		if names == nil {
			continue
		}
		if _, ok := names[g.CI.Job]; !ok {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q ci.job %q is not a job name or append_gate display in workflow %q; "+
					"set ci.job to the real GitHub check name (a job `name:` value or the append_gate display), not the workflow title",
				g.ID, g.CI.Job, g.CI.Workflow,
			))
		}
		concrete := concreteCache[g.CI.Workflow]
		for _, checkName := range g.CI.CheckNames {
			if _, ok := concrete[checkName]; ok {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"drift: gate %q ci.check_names entry %q is not a concrete check name produced by workflow %q",
				g.ID,
				checkName,
				g.CI.Workflow,
			))
		}
	}
	return errs
}

func workflowConcreteCheckNames(raw []byte) map[string]struct{} {
	names := workflowCheckNames(raw)
	if names == nil {
		names = make(map[string]struct{})
	}
	var workflow struct {
		Jobs map[string]struct {
			Name     string `yaml:"name"`
			Strategy struct {
				Matrix map[string]any `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return names
	}
	for _, job := range workflow.Jobs {
		matches := matrixVariableRE.FindAllStringSubmatch(job.Name, -1)
		if len(matches) != 1 {
			continue
		}
		expression := matches[0][0]
		key := matches[0][1]
		for _, value := range matrixValues(job.Strategy.Matrix, key) {
			name := strings.ReplaceAll(job.Name, expression, fmt.Sprint(value))
			names[name] = struct{}{}
		}
	}
	return names
}

func matrixValues(matrix map[string]any, key string) []any {
	var values []any
	if axis, ok := matrix[key].([]any); ok {
		values = append(values, axis...)
	}
	includes, _ := matrix["include"].([]any)
	for _, item := range includes {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := entry[key]; ok {
			values = append(values, value)
		}
	}
	return values
}

// workflowCheckNames returns the set of GitHub check names a workflow can
// produce: each job's `name:` (or its key when name is absent or a
// ${{ matrix.* }} expression) plus every append_gate display. It returns nil
// when it can resolve no names, so a caller can skip an unparseable workflow
// rather than reject every gate that references it.
func workflowCheckNames(raw []byte) map[string]struct{} {
	names := make(map[string]struct{})
	var wf struct {
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err == nil {
		for key, job := range wf.Jobs {
			if job.Name != "" && !strings.Contains(job.Name, "${{") {
				names[job.Name] = struct{}{}
			} else {
				names[key] = struct{}{}
			}
		}
	}
	for _, m := range appendGateDisplayRE.FindAllSubmatch(raw, -1) {
		names[string(m[1])] = struct{}{}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// Checks 1 and 2 (pre-commit hook parsing, hook-registration, and
// hook-stage-consistency: hookEntry, preCommitFile, parsePreCommitHooks,
// checkHookRegistration, stageConsistentWithTier, checkGateHookIDs) live in
// drift_hooks.go, split out to keep this file under the repository's
// 500-line cap. DriftCheck's doc comment above remains the single source of
// truth for what those two checks assert.

// ─── check 3: workflow ↔ registry completeness ─────────────────────────────

func checkWorkflowCompleteness(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")

	// Build set of workflows referenced by gates.
	gateWFs := make(map[string]struct{})
	for _, g := range reg.Gates {
		if g.CI.Workflow != "" {
			gateWFs[g.CI.Workflow] = struct{}{}
		}
	}
	for _, check := range reg.RequiredStatusChecks {
		if check.Workflow != "" {
			gateWFs[check.Workflow] = struct{}{}
		}
	}

	// Build set of non_gate_workflows entries (and check for stale entries).
	nonGateWFs := make(map[string]struct{}, len(reg.NonGateWorkflows))
	var errs []error
	for _, nf := range reg.NonGateWorkflows {
		nonGateWFs[nf.File] = struct{}{}
		// Check for double-registration.
		if _, inGate := gateWFs[nf.File]; inGate {
			errs = append(errs, fmt.Errorf(
				"drift: workflow %q is referenced by a gate ci.workflow AND listed in non_gate_workflows; "+
					"it must appear in exactly one place",
				nf.File,
			))
		}
		// Check stale on-disk.
		p := filepath.Join(wfDir, nf.File)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf(
				"drift: non_gate_workflows entry %q does not exist on disk (stale entry — remove it)",
				nf.File,
			))
		}
	}

	// List actual workflows on disk.
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No workflows directory at all — nothing to check.
			return errs
		}
		return append(errs, fmt.Errorf("drift: read workflow dir %s: %w", wfDir, err))
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		_, inGate := gateWFs[name]
		_, inNonGate := nonGateWFs[name]
		if !inGate && !inNonGate {
			errs = append(errs, fmt.Errorf(
				"drift: workflow %q is unregistered: add a gate with ci.workflow: %s or list it in non_gate_workflows with a reason",
				name, name,
			))
		}
	}

	return errs
}
