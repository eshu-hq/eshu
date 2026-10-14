// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
)

// Why this file exists (#8053).
//
// The pull-request await polls `gh pr checks`, which reports check runs by
// name on the commit with no event affinity: when two `pull_request` events
// run on one head (a close/reopen to get a fresh event cancels the older run
// through the workflow concurrency group), the rollup can surface a FAILURE
// row from the superseded, cancelled event while the newest event is still
// running -- or green. Judging that row red publishes `failure` on a head
// whose newest event never failed.
//
// A failed rollup row is therefore not yet a failed gate. Before returning
// errGateFailed, the await loop attributes each failed gate to the newest
// workflow run for the commit (the same newest-by-id, suite-joined rows the
// merge-group poll reads) and judges the newest run's verdict instead:
//
//   - newest run failed the job: genuine failure, carrying the newest run id
//     so the log names the event that was judged.
//   - newest run passed it: the rollup row is superseded and ignored; the
//     gate is confirmed green for this poll.
//   - newest run has no verdict yet (pending, missing with an in-flight run):
//     keep waiting. A later aggregate re-triggers on the newest run's
//     completion and reads its rows.
//   - newest run cancelled (or its verdict says the job never produced one):
//     cancelled infrastructure state, never a gate failure.
//
// Anything the attribution cannot decide -- a lookup error, or a newest run
// that concluded without ever producing the job -- fails closed on the
// original rollup row, exactly the pre-#8053 behaviour. No direction through
// this function can turn a genuine failure green: the only exit that drops a
// failure row is a SUCCESS check run for the same job in the newest run.

// attributedFailures is where each failed rollup row landed after newest-run
// attribution. Confirmed holds the genuine failures; Redirected holds gates
// whose verdict moved to pending or cancelled. A gate whose failure row was
// superseded by a newest-run success appears in neither bucket, which is what
// a pass does.
type attributedFailures struct {
	Confirmed  []requiredCheckFinding
	Redirected requiredCheckEvaluation
}

// attributeFailedGatesToNewestRun re-judges failed rollup rows against the
// newest workflow run for the commit. event scopes the run lookup to the same
// event the check rows came from, as the evaluator's run-conclusions lookup
// does. runs is the owning-run conclusions the poll already read; they decide
// the skipped and missing shapes the same way the evaluator decides them.
func attributeFailedGatesToNewestRun(
	ctx context.Context,
	runner ghRunner,
	repo, headSHA, event string,
	failed []requiredCheckFinding,
	runs runConclusions,
	out io.Writer,
) attributedFailures {
	var verdict attributedFailures
	newest, err := readEventChecks(ctx, runner, repo, headSHA, event)
	if err != nil {
		// Fail closed on the rollup rows: without attribution the honest
		// answer is the behaviour that predates #8053, never a pass.
		_, _ = fmt.Fprintf(out,
			"required-gates: could not read newest-run check runs (%v); judging the reported rows as failures\n", err)
		verdict.Confirmed = failed
		return verdict
	}
	for _, finding := range failed {
		gate := resolvedRequiredGate{
			WorkflowName: finding.WorkflowName,
			Job:          finding.Job,
			GateIDs:      append([]string(nil), finding.GateIDs...),
			Event:        event,
		}
		matches := matchingChecks(gate, newest)
		if len(matches) == 0 {
			attributeMissingFromNewestRun(gate, runs, &verdict)
			continue
		}
		single := evaluateRequiredChecks([]resolvedRequiredGate{gate}, newest, runs)
		switch {
		case len(single.Failed) > 0:
			verdict.Confirmed = append(verdict.Confirmed, single.Failed...)
			_, _ = fmt.Fprintf(out, "required-gates: %s\n",
				formatFindings(single.Failed))
		case len(single.Pending) > 0:
			verdict.Redirected.Pending = append(verdict.Redirected.Pending, single.Pending...)
			_, _ = fmt.Fprintf(out, "required-gates: %s / %s has no newest-run verdict yet; waiting\n",
				gate.WorkflowName, gate.Job)
		case len(single.Cancelled) > 0:
			verdict.Redirected.Cancelled = append(verdict.Redirected.Cancelled, single.Cancelled...)
			_, _ = fmt.Fprintf(out, "required-gates: %s\n",
				formatFindings(single.Cancelled))
		default:
			// The newest run passed this job: the rollup's FAILURE row
			// belongs to a superseded event and is ignored for this poll.
			// Nothing is recorded -- the gate simply stops contributing to
			// every bucket, which is what a pass does. The next poll
			// re-attributes from the current newest run, so a re-run that
			// fails again is still judged on its own verdict.
			_, _ = fmt.Fprintf(out,
				"required-gates: ignoring superseded %s / %s failure; newest %s run %d passed\n",
				gate.WorkflowName, gate.Job, event, matches[0].RunID)
		}
	}
	return verdict
}

// attributeMissingFromNewestRun decides a failed gate the newest run has no
// row for at all. A cancelled newest run makes it cancellation state; an
// in-flight one means the verdict has not arrived, so the wait continues; a
// newest run that concluded without the job is a real disagreement between the
// registry and the workflow and fails closed on the original row.
func attributeMissingFromNewestRun(gate resolvedRequiredGate, runs runConclusions, verdict *attributedFailures) {
	switch {
	case missingCheckIsCancellationArtifact(gate, runs):
		verdict.Redirected.Cancelled = append(verdict.Redirected.Cancelled,
			findingFor(gate, "MISSING (newest run cancelled)", 0))
	case runs.inFlight(gate.WorkflowName):
		verdict.Redirected.Pending = append(verdict.Redirected.Pending,
			findingFor(gate, "PENDING (newest run in flight)", 0))
	default:
		verdict.Confirmed = append(verdict.Confirmed,
			findingFor(gate, "MISSING (newest run concluded without it)", 0))
	}
}
