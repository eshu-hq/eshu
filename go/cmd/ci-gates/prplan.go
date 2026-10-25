// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// prPlan is the versioned, fail-closed interface consumed by the central PR
// scheduler. An empty jobs map is valid only for a proven reuse.
type prPlan struct {
	SchemaVersion int                 `json:"schema_version"`
	Mode          string              `json:"mode"`
	HeadSHA       string              `json:"head_sha"`
	SourceSHA     string              `json:"source_sha,omitempty"`
	SourceFullSHA string              `json:"source_full_sha,omitempty"`
	Tree          string              `json:"tree,omitempty"`
	TargetRunID   int64               `json:"target_run_id"`
	TargetAttempt int                 `json:"target_run_attempt"`
	MergeSHA      string              `json:"merge_sha"`
	MergeTree     string              `json:"merge_tree"`
	Workflows     []string            `json:"workflows"`
	Jobs          map[string][]string `json:"jobs"`
	Gates         []string            `json:"gates"`
	Reason        string              `json:"reason"`
}

func runPRPlan(args []string) error {
	fs := flag.NewFlagSet("pr-plan", flag.ContinueOnError)
	registry := fs.String("registry", "", "trusted default-branch registry")
	repoRoot := fs.String("repo-root", "", "trusted default-branch checkout")
	repo := fs.String("repo", "", "GitHub owner/name")
	pr := fs.Int("pr", 0, "pull request number")
	headSHA := fs.String("head-sha", "", "exact PR head SHA")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *registry == "" || *repoRoot == "" || !validRepoName(*repo) || *pr <= 0 || !commitSHAPattern.MatchString(*headSHA) {
		return fmt.Errorf("--registry, --repo-root, --repo owner/name, --pr, and --head-sha are required")
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	registryPath, err := filepath.Abs(*registry)
	if err != nil {
		return err
	}
	if filepath.Clean(registryPath) != filepath.Join(root, "specs", "ci-gates.v1.yaml") {
		return fmt.Errorf("--registry must be specs/ci-gates.v1.yaml in --repo-root trusted checkout")
	}
	reg, err := cigates.Load(registryPath)
	if err != nil {
		return fmt.Errorf("load trusted registry: %w", err)
	}
	if errs := reg.ValidateRequiredStatusChecks(); len(errs) > 0 {
		return fmt.Errorf("invalid trusted registry: %v", errs)
	}
	defs, err := trustedPRWorkflowDefinitions(root)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID, attempt, err := nativeRunAttempt()
	if err != nil {
		return err
	}
	input, err := readTestedInput(ctx, execGHRunner{}, *repo, *pr, *headSHA, runID, attempt)
	if err != nil {
		return fmt.Errorf("bind PR plan to caller: %w", err)
	}
	ctx = withTestedInput(ctx, input)
	plan, err := buildPRPlan(ctx, execGHRunner{}, reg, defs, *repo, *pr, *headSHA)
	if err != nil {
		return err
	}
	if err := validatePRPlan(plan); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(plan)
}

func trustedPRWorkflowDefinitions(root string) ([]cigates.PRWorkflowDefinition, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		return nil, err
	}
	yamlPaths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yaml"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, yamlPaths...)
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("trusted checkout has no workflow definitions")
	}
	var defs []cigates.PRWorkflowDefinition
	for _, path := range paths {
		raw, err := os.ReadFile(path) // #nosec G304 -- only paths enumerated under the trusted checkout
		if err != nil {
			return nil, err
		}
		def, include, err := cigates.ParsePRWorkflowDefinition(filepath.Base(path), raw)
		if err != nil {
			return nil, err
		}
		if include {
			defs = append(defs, def)
		}
	}
	seen := make(map[string]bool, len(defs))
	for _, def := range defs {
		seen[def.File] = true
	}
	callable, _, err := trustedCallablePRWorkflowDefinitions(root, seen)
	if err != nil {
		return nil, err
	}
	defs = append(defs, callable...)
	if len(defs) == 0 {
		return nil, fmt.Errorf("trusted checkout has no PR workflow definition")
	}
	files := make([]string, 0, len(defs))
	for _, def := range defs {
		files = append(files, def.File)
	}
	if err := cigates.ValidatePRWorkflowCallerStems(files); err != nil {
		return nil, err
	}
	return defs, nil
}

func trustedCallablePRWorkflowDefinitions(root string, seen map[string]bool) ([]cigates.PRWorkflowDefinition, bool, error) {
	dir := filepath.Join(root, "specs", "ci-pr-workflows.d")
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stat PR plan fragments: %w", err)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("PR plan fragments path is not a directory")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, false, err
	}
	ymlPaths, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, false, err
	}
	paths = append(paths, ymlPaths...)
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, true, fmt.Errorf("PR plan fragment directory is empty")
	}
	var defs []cigates.PRWorkflowDefinition
	for _, path := range paths {
		raw, err := os.ReadFile(path) // #nosec G304 -- trusted fragment path from a confined glob
		if err != nil {
			return nil, true, err
		}
		rows, err := cigates.ParsePRPlanFragment(raw)
		if err != nil {
			return nil, true, fmt.Errorf("fragment %s: %w", filepath.Base(path), err)
		}
		for _, row := range rows {
			if seen[row.File] {
				return nil, true, fmt.Errorf("duplicate direct/callable PR workflow %s", row.File)
			}
			seen[row.File] = true
			workflowPath := filepath.Join(root, ".github", "workflows", row.File)
			workflowRaw, err := os.ReadFile(workflowPath) // #nosec G304 -- fragment filename is basename-validated
			if err != nil {
				return nil, true, fmt.Errorf("read callable %s: %w", row.File, err)
			}
			def, err := cigates.ValidateCallablePRWorkflow(row, workflowRaw)
			if err != nil {
				return nil, true, err
			}
			defs = append(defs, def)
		}
	}
	return defs, true, nil
}

func buildPRPlan(ctx context.Context, runner ghRunner, reg *cigates.Registry, defs []cigates.PRWorkflowDefinition, repo string, pr int, headSHA string) (prPlan, error) {
	target, bound := currentTestedInput(ctx)
	if !bound {
		return prPlan{}, fmt.Errorf("PR plan has no exact tested caller input")
	}
	if err := verifyPRHead(ctx, runner, repo, pr, headSHA); err != nil {
		return prPlan{}, err
	}
	baseRef, err := readPRBaseRef(ctx, runner, repo, pr)
	if err != nil {
		return prPlan{}, err
	}
	paths, truncated, err := changedPathsForPR(ctx, runner, repo, pr)
	if err != nil {
		return prPlan{}, err
	}
	var required []cigates.RequiredGate
	if truncated {
		required, err = reg.AllBlockingGates()
	} else {
		required, err = reg.RequiredGates(paths)
	}
	if err != nil {
		return prPlan{}, err
	}
	known := make(map[string]bool, len(defs))
	for _, def := range defs {
		known[def.File] = true
	}
	for _, gate := range required {
		if !known[gate.Workflow] {
			return prPlan{}, fmt.Errorf("selected blocking workflow %s lacks a trusted direct trigger or callable fragment", gate.Workflow)
		}
	}
	plan := prPlan{SchemaVersion: 2, Mode: "full", HeadSHA: headSHA, TargetRunID: target.RunID, TargetAttempt: target.Attempt, MergeSHA: target.MergeSHA, MergeTree: target.MergeTree, Reason: "full applicable synchronize selection"}
	// A PR that edits gate definitions cannot transfer a prior verdict. The
	// default-branch publisher keeps judging with trusted definitions until
	// the changed definitions land, so this path must be a full execution.
	definitionsChanged := !transferPRPathsSafe(paths, truncated) || cigates.HasUnknownPRWorkflowDefinitions(reg, defs)
	if !definitionsChanged {
		reuse, probeErr := probeReuseForAwait(ctx, runner, reg, repo, pr, headSHA)
		if probeErr == nil && reuse.Reuse {
			plan.Mode, plan.SourceSHA, plan.SourceFullSHA, plan.Tree, plan.Reason = "reuse", reuse.SourceSHA, reuse.FullRootSHA, reuse.Tree, reuse.Reason
		} else {
			contexts := make([]string, 0, len(reg.RequiredStatusChecks))
			for _, check := range reg.RequiredStatusChecks {
				contexts = append(contexts, check.Context)
			}
			smoke, smokeErr := probeSmokeEvidence(ctx, runner, repo, pr, headSHA, paths, truncated, contexts)
			if smokeErr == nil && smoke.Applied {
				plan.Mode, plan.SourceSHA, plan.SourceFullSHA, plan.Tree, plan.Reason = "smoke", smoke.SourceSHA, smoke.FullRootSHA, smoke.Tree, "full-backed clean rebase; explicit fast jobs only"
			}
		}
	} else {
		plan.Reason = "gate definition changed or changed-file listing truncated; full selection required"
	}
	var fast map[string][]string
	if plan.Mode == "smoke" {
		fast, err = smokeJobIDs(reg, defs, paths)
		if err != nil {
			plan.Mode, plan.SourceSHA, plan.SourceFullSHA, plan.Tree, plan.Reason = "full", "", "", "", "smoke job ownership unresolved; full selection required: "+err.Error()
		}
	}
	plan.Jobs, err = cigates.PlanPRWorkflows(defs, paths, truncated, baseRef, plan.Mode, fast)
	if err != nil && plan.Mode == "smoke" {
		plan.Mode, plan.SourceSHA, plan.SourceFullSHA, plan.Tree, plan.Reason = "full", "", "", "", "smoke selection incomplete; full selection required: "+err.Error()
		plan.Jobs, err = cigates.PlanPRWorkflows(defs, paths, truncated, baseRef, "full", nil)
	}
	if err != nil {
		return prPlan{}, err
	}
	plan.Gates, err = cigates.SelectPRGateIDs(reg, defs, paths, truncated, plan.Mode, plan.Jobs)
	if err != nil && plan.Mode == "smoke" {
		plan.Mode, plan.SourceSHA, plan.SourceFullSHA, plan.Tree, plan.Reason = "full", "", "", "", "smoke gate-cell ownership unresolved; full selection required: "+err.Error()
		plan.Jobs, err = cigates.PlanPRWorkflows(defs, paths, truncated, baseRef, "full", nil)
		if err != nil {
			return prPlan{}, err
		}
		plan.Gates, err = cigates.SelectPRGateIDs(reg, defs, paths, truncated, "full", plan.Jobs)
	}
	if err != nil {
		return prPlan{}, err
	}
	plan.Workflows = make([]string, 0, len(plan.Jobs))
	for workflow := range plan.Jobs {
		plan.Workflows = append(plan.Workflows, workflow)
	}
	sort.Strings(plan.Workflows)
	if err := verifyPRHead(ctx, runner, repo, pr, headSHA); err != nil {
		return prPlan{}, err
	}
	currentBase, err := readPRBaseRef(ctx, runner, repo, pr)
	if err != nil || currentBase != baseRef {
		return prPlan{}, fmt.Errorf("PR base changed during planning: %s -> %s (%v)", baseRef, currentBase, err)
	}
	return plan, nil
}

func smokeJobIDs(reg *cigates.Registry, defs []cigates.PRWorkflowDefinition, paths []string) (map[string][]string, error) {
	required, err := reg.RequiredGates(paths)
	if err != nil {
		return nil, err
	}
	tiers := make(map[string]cigates.Tier)
	for _, gate := range reg.Gates {
		tiers[gate.ID] = gate.Tier
	}
	defByFile := make(map[string]cigates.PRWorkflowDefinition)
	for _, def := range defs {
		defByFile[def.File] = def
	}
	fast := make(map[string][]string)
	fastRequired := 0
	for _, gate := range required {
		keep := false
		for _, id := range gate.GateIDs {
			tier, known := tiers[id]
			if !known || tier == cigates.TierPreCommit || tier == cigates.TierPrePush {
				keep = true
			}
		}
		if !keep {
			continue
		}
		fastRequired++
		def, known := defByFile[gate.Workflow]
		if !known {
			return nil, fmt.Errorf("unknown workflow %s", gate.Workflow)
		}
		match := ""
		for _, job := range def.Jobs {
			if job == gate.Job || def.JobNames[job] == gate.Job {
				if match != "" {
					return nil, fmt.Errorf("ambiguous job %s in %s", gate.Job, gate.Workflow)
				}
				match = job
			}
		}
		if match == "" {
			return nil, fmt.Errorf("unknown job %s in %s", gate.Job, gate.Workflow)
		}
		exists := false
		for _, job := range fast[gate.Workflow] {
			if job == match {
				exists = true
			}
		}
		if !exists {
			fast[gate.Workflow] = append(fast[gate.Workflow], match)
		}
	}
	if fastRequired == 0 {
		return nil, fmt.Errorf("smoke has no selected fast blocking gate")
	}
	for _, check := range reg.RequiredStatusChecks {
		if check.Context != "go-core-complete" && check.Context != "go-race-complete" {
			continue
		}
		def, known := defByFile[check.Workflow]
		if !known {
			return nil, fmt.Errorf("required status workflow %s is absent", check.Workflow)
		}
		job := ""
		for _, localID := range def.Jobs {
			if localID == check.Job || def.JobNames[localID] == check.Job {
				if job != "" {
					return nil, fmt.Errorf("required status %s has ambiguous local job", check.Context)
				}
				job = localID
			}
		}
		if job == "" {
			return nil, fmt.Errorf("required status %s has no local job", check.Context)
		}
		if !slices.Contains(fast[check.Workflow], job) {
			fast[check.Workflow] = append(fast[check.Workflow], job)
		}
	}
	return fast, nil
}

func readPRBaseRef(ctx context.Context, runner ghRunner, repo string, pr int) (string, error) {
	output, err := runner.Run(ctx, "pr", "view", fmt.Sprint(pr), "--repo", repo, "--json", "baseRefName", "--jq", ".baseRefName")
	if err != nil {
		return "", fmt.Errorf("read PR base: %w", err)
	}
	base := strings.TrimSpace(string(output))
	if base == "" {
		return "", fmt.Errorf("PR has no base branch")
	}
	return base, nil
}
