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
	}
	return errs
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

// ─── pre-commit hook parsing ────────────────────────────────────────────────

// hookEntry is a parsed local hook from .pre-commit-config.yaml.
type hookEntry struct {
	ID     string
	Stages []string
}

// preCommitFile is the minimal shape of .pre-commit-config.yaml we need.
type preCommitFile struct {
	Repos []struct {
		Repo  string `yaml:"repo"`
		Hooks []struct {
			ID     string   `yaml:"id"`
			Stages []string `yaml:"stages"`
		} `yaml:"hooks"`
	} `yaml:"repos"`
}

// parsePreCommitHooks reads .pre-commit-config.yaml under repoRoot and returns
// the map of hook id → hookEntry for every hook in a "local" repo block.
func parsePreCommitHooks(repoRoot string) (map[string]hookEntry, []error) {
	p := filepath.Join(repoRoot, ".pre-commit-config.yaml")
	raw, err := os.ReadFile(p) // #nosec G304 -- repoRoot is the operator-provided repo root
	if err != nil {
		return nil, []error{fmt.Errorf("drift: read %s: %w", p, err)}
	}
	var pcf preCommitFile
	if err := yaml.Unmarshal(raw, &pcf); err != nil {
		return nil, []error{fmt.Errorf("drift: parse %s: %w", p, err)}
	}

	hooks := make(map[string]hookEntry)
	for _, repo := range pcf.Repos {
		if repo.Repo != "local" {
			continue
		}
		for _, h := range repo.Hooks {
			id := strings.TrimSpace(h.ID)
			if id == "" {
				continue
			}
			hooks[id] = hookEntry{ID: id, Stages: h.Stages}
		}
	}
	return hooks, nil
}

// ─── check 1: hook → registry/hygiene ──────────────────────────────────────

func checkHookRegistration(hooks map[string]hookEntry, reg *Registry) []error {
	// Build lookup sets.
	gateHookIDs := make(map[string]struct{}, len(reg.Gates))
	for _, g := range reg.Gates {
		if g.HookID != "" {
			gateHookIDs[g.HookID] = struct{}{}
		}
	}
	hygieneIDs := make(map[string]struct{}, len(reg.HygieneHooks))
	for _, h := range reg.HygieneHooks {
		hygieneIDs[h.ID] = struct{}{}
	}

	var errs []error
	for id := range hooks {
		_, isGate := gateHookIDs[id]
		_, isHygiene := hygieneIDs[id]
		if !isGate && !isHygiene {
			errs = append(errs, fmt.Errorf(
				"drift: hook %q is neither a registered gate (hook_id) nor a declared hygiene hook; "+
					"add hook_id to a gate or add it to hygiene_hooks with a reason",
				id,
			))
		}
	}
	return errs
}

// ─── check 2: gate hook_id → present + stage match ─────────────────────────

// stageConsistentWithTier reports whether the hook's declared stages are
// consistent with the gate's tier. A gate with no stages declared (pre-commit
// default) is treated as running at the default stage, which is consistent with
// TierPreCommit but not TierPrePush.
func stageConsistentWithTier(stages []string, tier Tier) bool {
	switch tier {
	case TierPreCommit:
		// Hook must be reachable at pre-commit time. An empty stages list means
		// "default" (pre-commit), which is consistent. An explicit list must
		// include "pre-commit" or "default".
		if len(stages) == 0 {
			return true
		}
		for _, s := range stages {
			if s == "pre-commit" || s == "default" {
				return true
			}
		}
		return false
	case TierPrePush:
		// Hook must be reachable at pre-push time.
		if len(stages) == 0 {
			// Default stage is pre-commit only; not consistent with pre-push.
			return false
		}
		for _, s := range stages {
			if s == "pre-push" {
				return true
			}
		}
		return false
	default:
		// For pre-pr / ci-heavy / manual, hook_id should generally not be set;
		// if it is, we accept any stage rather than false-erroring.
		return true
	}
}

func checkGateHookIDs(hooks map[string]hookEntry, reg *Registry) []error {
	var errs []error
	for _, g := range reg.Gates {
		if g.HookID == "" {
			continue
		}
		he, ok := hooks[g.HookID]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q declares hook_id %q but that hook is not present in .pre-commit-config.yaml",
				g.ID, g.HookID,
			))
			continue
		}
		if !stageConsistentWithTier(he.Stages, g.Tier) {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q (tier %s) hook_id %q has stages %v — inconsistent with gate tier "+
					"(pre-commit gate requires stage pre-commit/default; pre-push gate requires stage pre-push)",
				g.ID, g.Tier, g.HookID, he.Stages,
			))
		}
	}
	return errs
}

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
