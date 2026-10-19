// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

func validateTrustedWorkflowTriggers(check RequiredStatusCheck, raw []byte) []error {
	triggers, err := workflowTriggerKeys(raw)
	if err != nil {
		return []error{fmt.Errorf("required status context %q: parse workflow triggers: %w", check.Context, err)}
	}

	var errs []error
	if _, ok := triggers["workflow_run"]; !ok {
		errs = append(errs, fmt.Errorf("required status context %q: trusted publisher must trigger on workflow_run", check.Context))
	}
	sources, sourceErr := workflowRunSources(raw)
	if sourceErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run sources: %w", check.Context, sourceErr))
	} else if !slicesContain(sources, check.SourceWorkflow) {
		errs = append(errs, fmt.Errorf(
			"required status context %q: workflow_run sources %q do not include declared source workflow %q",
			check.Context,
			sources,
			check.SourceWorkflow,
		))
	}
	types, typesErr := workflowRunList(raw, "types")
	if typesErr != nil {
		errs = append(errs, fmt.Errorf("required status context %q: parse workflow_run activity types: %w", check.Context, typesErr))
	} else {
		for _, requiredType := range []string{"in_progress", "completed"} {
			if !slicesContain(types, requiredType) {
				errs = append(errs, fmt.Errorf(
					"required status context %q: workflow_run activity types must include %q",
					check.Context,
					requiredType,
				))
			}
		}
	}
	errs = append(errs, validateWorkflowRunBranchFilter(check, raw)...)
	for _, forbidden := range []string{"pull_request", "pull_request_target"} {
		if _, ok := triggers[forbidden]; ok {
			errs = append(errs, fmt.Errorf(
				"required status context %q: trusted publisher must not execute from untrusted %s workflow code",
				check.Context,
				forbidden,
			))
		}
	}
	return errs
}

// validateCompletedOnlyAggregation pins the cost split between the two
// workflow_run activity types (#8617). An in_progress event exists so the
// pending write invalidates a stale success when a blocking workflow starts
// (or re-runs) on the same head; that write is one API call. Everything after
// it — checkout, Go build, the await, and the terminal publish — classifies
// gate outcomes, and on an in_progress event it can only wait for events that
// have not happened yet, holding a runner for up to the job timeout per
// source workflow. Those steps must therefore run only for a completed
// event, which every in-flight source workflow still sends when it finishes
// or is cancelled, so no verdict depends on an in_progress run aggregating.
func validateCompletedOnlyAggregation(check RequiredStatusCheck, job requiredWorkflowJob) []error {
	pending := -1
	for i, step := range job.Steps {
		if strings.Contains(step.Run, "state=pending") {
			pending = i
			break
		}
	}
	if pending < 0 {
		// The missing pending write is reported by the publisher-shape
		// validators; without it there is no split point to judge here.
		return nil
	}
	var errs []error
	for _, step := range job.Steps[pending+1:] {
		condition := strings.ReplaceAll(step.If, " ", "")
		// A disjunction would re-admit in_progress through its other arm
		// while still containing the required literal, so reject it outright.
		if strings.Contains(condition, "||") {
			errs = append(errs, fmt.Errorf(
				"required status context %q: step %q uses a disjunction (||) after the pending write, which can "+
					"re-admit an in_progress event past the completed-only condition; use a plain conjunction",
				check.Context,
				step.Name,
			))
			continue
		}
		if !strings.Contains(condition, "github.event.action=='completed'") {
			errs = append(errs, fmt.Errorf(
				"required status context %q: step %q runs after the pending write without a completed-only "+
					"condition, so an in_progress event pays the full aggregation cost; add github.event.action == 'completed' to its if",
				check.Context,
				step.Name,
			))
		}
	}
	return errs
}

// aggregatorIgnoredBranch is the only head branch the aggregator's workflow_run
// trigger may exclude. A main push fans the ~21 listed workflows that run on
// push x {in_progress, completed} into one per-SHA concurrency group
// (#7111 F8); the job-level `if`
// skips them, but concurrency has already cancelled all but the last pending
// run, so each main push buried the run list in cancelled rows. The trigger
// excludes them instead. Pull-request heads and merge-queue heads
// (gh-readonly-queue/...) are never `main`, so the status they read is
// unchanged. The filter is `branches-ignore` and nothing else: a `branches`
// allowlist or a wider exclusion would silently stop aggregating some pull
// requests, which strands them on a `required-gates-complete` that never posts.
const aggregatorIgnoredBranch = "main"

func validateWorkflowRunBranchFilter(check RequiredStatusCheck, raw []byte) []error {
	if _, err := workflowRunList(raw, "branches"); err == nil {
		return []error{fmt.Errorf(
			"required status context %q: workflow_run must not use a branches allowlist; exclude only %q with branches-ignore",
			check.Context,
			aggregatorIgnoredBranch,
		)}
	}
	ignored, err := workflowRunList(raw, "branches-ignore")
	if err != nil || len(ignored) != 1 || ignored[0] != aggregatorIgnoredBranch {
		return []error{fmt.Errorf(
			"required status context %q: workflow_run branches-ignore must be exactly [%s] so main-push events "+
				"stop fanning into cancelled runs while every pull-request and merge-queue head still aggregates; got %q (parse error: %v)",
			check.Context,
			aggregatorIgnoredBranch,
			ignored,
			err,
		)}
	}
	return nil
}
