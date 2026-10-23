// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// requiredPlannerGate adds the central planner's own verdict only after the
// callable inventory is deployed on the trusted default branch. Phase A keeps
// today's direct workflow selection unchanged until that deployment.
func requiredPlannerGate(root string) (resolvedRequiredGate, bool, error) {
	dir := filepath.Join(root, "specs", "ci-pr-workflows.d")
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return resolvedRequiredGate{}, false, nil
	}
	if err != nil {
		return resolvedRequiredGate{}, false, fmt.Errorf("stat trusted PR plan fragments: %w", err)
	}
	if !info.IsDir() {
		return resolvedRequiredGate{}, false, fmt.Errorf("trusted PR plan fragment path is not a directory")
	}
	workflowPath := filepath.Join(root, ".github", "workflows", "test.yml")
	raw, err := os.ReadFile(workflowPath) // #nosec G304 -- fixed path in the trusted checkout
	if err != nil {
		return resolvedRequiredGate{}, false, fmt.Errorf("read trusted planner workflow test.yml: %w", err)
	}
	def, included, err := cigates.ParsePRWorkflowDefinition("test.yml", raw)
	if err != nil {
		return resolvedRequiredGate{}, false, fmt.Errorf("parse trusted planner workflow test.yml: %w", err)
	}
	if !included || !def.Synchronize || !slices.Contains(def.Jobs, "pr-plan") ||
		(def.JobNames["pr-plan"] != "" && def.JobNames["pr-plan"] != "pr-plan") {
		return resolvedRequiredGate{}, false, fmt.Errorf("trusted planner workflow test.yml must define a synchronize pull_request pr-plan job with check name pr-plan")
	}
	gates, err := resolveRequiredGateWorkflows(root, []cigates.RequiredGate{{Workflow: "test.yml", Job: "pr-plan", GateIDs: []string{"central-pr-plan"}}})
	if err != nil || len(gates) != 1 {
		return resolvedRequiredGate{}, false, fmt.Errorf("resolve trusted planner check: %v", err)
	}
	return gates[0], true, nil
}

func changedPRPlanDefinition(paths []string, truncated bool) bool {
	if truncated {
		return true
	}
	for _, path := range paths {
		if strings.HasPrefix(path, "specs/ci-pr-workflows.d/") {
			return true
		}
	}
	return false
}

// transferProvenReuse requires the current trusted planner to pass before a
// full-backed tree-identical verdict can bypass leaf jobs after migration.
func transferProvenReuse(ctx context.Context, runner ghRunner, root, repo string, pr int, headSHA string, pollInterval time.Duration, verdict reuseVerdict) (bool, error) {
	planner, active, err := requiredPlannerGate(root)
	if err != nil {
		return false, err
	}
	if active {
		paths, truncated, err := changedPathsForPR(ctx, runner, repo, pr)
		if err != nil {
			return false, err
		}
		if changedPRPlanDefinition(paths, truncated) {
			_, _ = fmt.Fprintln(os.Stdout, "required-gates: planner definition changed; reuse refused")
			return false, nil
		}
		if err := awaitPRRequiredChecks(ctx, runner, repo, pr, headSHA, []resolvedRequiredGate{planner}, pollInterval, os.Stdout); err != nil {
			return false, err
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "required-gates: reuse source=%s tree=%s (%s)\n", verdict.SourceSHA, verdict.Tree, verdict.Reason)
	if err := verifyPRHead(ctx, runner, repo, pr, headSHA); err != nil {
		return false, err
	}
	return true, fmt.Errorf("%w: source %s tree %s", errReuseProven, verdict.SourceSHA, verdict.Tree)
}
