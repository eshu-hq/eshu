// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Outcomes that are not gate results (#6189).
//
// The aggregate's `default:` arm treats "anything that is not pass, pending, or
// a known cancellation" as a failed gate, and publishes "A required gate
// failed". Three GitHub shapes reach that arm without any gate having failed.
// They are collected here rather than in await.go so the classification and
// the one API call it needs stay next to each other, and so await.go stays
// clear of the 500-line cap.
//
// All three resolve to the same operator repair -- re-run the workflow -- so
// they share exit code 13 and the publisher arm that already exists for it,
// rather than each earning a code, a workflow arm, and a mirrored constant.

// isCancelledCheck reports whether a check rollup entry describes a run that
// was CANCELLED rather than one that failed.
//
// Both signals are accepted because the runner's `gh` version is not pinned by
// this repository. gh buckets CANCELLED as "cancel" (verified in cli/cli
// v2.97.0 pkg/cmd/pr/checks/aggregate.go, the version installed here); older
// gh releases folded CANCELLED into the "fail" bucket alongside ERROR and
// TIMED_OUT. The `state` field carries the literal GitHub conclusion in both,
// which is why the #6189 transcript could report `=CANCELLED` on findings the
// aggregate had already filed as failures. Matching either signal means a gh
// upgrade or downgrade on the runner cannot silently restore the overclaim.
func isCancelledCheck(check checkRollup) bool {
	return strings.EqualFold(check.State, "CANCELLED") || strings.EqualFold(check.Bucket, "cancel")
}

// isStaleCheck reports whether GitHub marked the check run STALE.
//
// STALE is a conclusion only GitHub can set, on a check run it orphaned -- the
// run was superseded and its result will never arrive. It is terminal and it
// is nobody's fault, so it is not a gate failure.
//
// It is matched on the state alone, deliberately. gh sends STALE through the
// bucket switch's `default:` arm, so it arrives bucketed "pending" (cli/cli
// v2.97.0 pkg/cmd/pr/checks/aggregate.go: the default comment lists STALE
// beside QUEUED and IN_PROGRESS). Before #6189's second round that made a
// STALE gate wait out the full 55-minute timeout and then publish NOTHING,
// leaving required-gates-complete on the pending status the first step wrote
// -- a blocked pull request with no red check anywhere to act on. Because the
// bucket says "pending", the caller MUST test this before it tests the pending
// bucket, or the classification never runs.
func isStaleCheck(check checkRollup) bool {
	return strings.EqualFold(check.State, "STALE")
}

// isSkippedCheck reports whether GitHub reported the check as SKIPPED.
//
// Matched on the state, not on gh's bucket: gh maps SKIPPED *and* NEUTRAL to
// the "skipping" bucket, and NEUTRAL is a conclusion a job actually reached.
// Only SKIPPED is ambiguous enough to need the parent-run lookup below, so
// only SKIPPED is matched here; NEUTRAL keeps failing closed unconditionally.
func isSkippedCheck(check checkRollup) bool {
	return strings.EqualFold(check.State, "SKIPPED")
}

// runConclusions maps a workflow NAME -- the same identity `gh pr checks`
// reports in its `workflow` field, so the two join directly -- to the
// lower-cased conclusion of the run that owns this head's checks for it.
//
// Three states, not two, and the third is why this is not a map of bool
// (#6189, third round). A re-run of a cancelled workflow is the repair the
// cancelled-gate description tells an operator to perform, and between the
// replacement run starting and its check runs replacing the old ones in the
// rollup, the aggregate sees a SKIPPED check whose owning run has not
// concluded. Collapsing that into "not cancelled" republishes "A required gate
// failed" on a head where nothing failed -- the overclaim this change exists to
// remove, reintroduced on its own repair path.
//
//   - absent: no run for that workflow was seen on this head. Unknown, so a
//     SKIPPED gate keeps failing closed. A failed lookup lands here too, by
//     returning a nil map.
//   - present and empty: the run exists and has NOT concluded. Its verdict is
//     not knowable yet, so the gate stays pending.
//   - present and non-empty: the run's terminal conclusion.
type runConclusions map[string]string

// cancelled reports whether the run owning this workflow's checks on the head
// concluded `cancelled`.
func (r runConclusions) cancelled(workflow string) bool {
	return r[workflow] == "cancelled"
}

// inFlight reports whether a run for this workflow exists on the head but has
// not concluded. Distinct from absent: absent is unknown and fails closed,
// in-flight is a verdict that has not arrived yet and is worth waiting for.
func (r runConclusions) inFlight(workflow string) bool {
	conclusion, ok := r[workflow]
	return ok && conclusion == ""
}

// isNotAGateResult reports whether a check describes something other than a
// verdict the gate reached, and so must not publish `failure`.
//
// runs carries the conclusion of the run owning each workflow's checks. A nil
// or absent entry means "not known to be cancelled", which keeps a skipped gate
// failing closed -- the safe default, and the one a failed lookup falls back
// to.
//
// The asymmetry between the three is the point. CANCELLED and STALE are
// self-describing: the check itself says it never produced a verdict. SKIPPED
// is not. GitHub uses SKIPPED both for "a `needs:` dependency was cancelled so
// this job never ran" and for "this job's own `if:` excluded it", and the
// second is a genuine discrepancy between the registry's selection and the
// workflow's own conditions, which MUST keep failing closed. The only thing
// that separates them is the conclusion of the workflow run that owns the job.
func isNotAGateResult(check checkRollup, runs runConclusions) bool {
	switch {
	case isCancelledCheck(check):
		return true
	case isStaleCheck(check):
		return true
	case isSkippedCheck(check):
		return runs.cancelled(check.Workflow)
	default:
		return false
	}
}

// isAwaitingRunConclusion reports whether a SKIPPED check cannot be classified
// yet because the workflow run that owns it is still executing.
//
// This is the re-run window. GitHub returns runs newest-first, so once a
// cancelled workflow is re-run the newest run for that name is the replacement,
// and it carries `conclusion: null` until it finishes -- while the check rollup
// still reports the cancelled run's SKIPPED job, because the replacement's
// check runs have not landed in it. required-gates.yml triggers the aggregate
// on `in_progress`, so the aggregate really does read that pair.
//
// Neither terminal answer is honest there. "Cancelled" would publish `error`
// against a run that may yet pass, and "not cancelled" would publish "A
// required gate failed" against a run that has not failed. Waiting is, and the
// wait terminates: the replacement run's own completion re-triggers this
// workflow with a rollup that can be decided.
func isAwaitingRunConclusion(check checkRollup, runs runConclusions) bool {
	return isSkippedCheck(check) && runs.inFlight(check.Workflow)
}

// anySelectedCheckSkipped reports whether any selected gate resolved to a
// SKIPPED check, which is the only reason to spend an API call on run
// conclusions. The await loop polls every 30 seconds for up to 55 minutes, so
// an unconditional extra call per poll would be real traffic for a lookup that
// almost never changes an outcome.
func anySelectedCheckSkipped(required []resolvedRequiredGate, checks []checkRollup) bool {
	for _, gate := range required {
		for _, check := range matchingChecks(gate, checks) {
			if isSkippedCheck(check) {
				return true
			}
		}
	}
	return false
}

// workflowRunConclusion is the slice of a workflow run this command reads.
type workflowRunConclusion struct {
	Name       string `json:"name"`
	Event      string `json:"event"`
	Conclusion string `json:"conclusion"`
}

// workflowRunConclusions reads the conclusion of each pull-request workflow run
// on this head, keyed by workflow NAME.
//
// Needs the `actions: read` permission, which the aggregate job already
// declares in .github/workflows/required-gates.yml; no new token scope and no
// repository secret is involved.
//
// GitHub returns runs newest-first, and gh's own rollup keeps the most
// recently started check for a name, so taking the first run seen per workflow
// name matches the row the rollup reported. A re-run therefore replaces the
// cancelled run's conclusion, which is what should happen: once the workflow
// has been re-run, its skipped jobs are no longer cancellation artifacts. While
// that replacement is still executing its conclusion is empty rather than
// absent, which runConclusions.inFlight reads as "wait" rather than as either
// terminal answer.
//
// No direction of a mismatch can open the gate. Reading a cancelled run as
// not-cancelled leaves a skipped gate publishing `failure`, which is the
// behaviour that predates #6189; reading a live run as cancelled downgrades it
// to `error`, which still blocks the merge; reading a concluded run as
// in-flight leaves the gate pending, which publishes nothing at all. None of
// the three can turn a skipped gate green.
func workflowRunConclusions(ctx context.Context, runner ghRunner, repo, headSHA string) (runConclusions, error) {
	endpoint := "repos/" + repo + "/actions/runs?per_page=100&head_sha=" + url.QueryEscape(headSHA)
	output, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("list workflow runs for %s: %w", headSHA, err)
	}
	var pages []struct {
		WorkflowRuns []workflowRunConclusion `json:"workflow_runs"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode workflow runs for %s: %w", headSHA, err)
	}
	conclusions := make(runConclusions)
	for _, page := range pages {
		for _, run := range page.WorkflowRuns {
			if run.Event != "pull_request" {
				continue
			}
			// Newest-first, so the first run seen for a name wins; a later
			// page repeating the name is an older attempt.
			if _, seen := conclusions[run.Name]; seen {
				continue
			}
			// GitHub sends `conclusion: null` for a run that has not
			// finished, which decodes to the empty string -- the in-flight
			// state runConclusions.inFlight reads.
			conclusions[run.Name] = strings.ToLower(run.Conclusion)
		}
	}
	return conclusions, nil
}
