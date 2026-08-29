// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for database/sql

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// SQL for the B-7(a) drain gate. One statement is deliberate: Postgres gives
// every scalar subquery the same MVCC snapshot, so producer completion cannot
// disappear between one ledger read and the consumer work it atomically opens.
// The status set and completed_at semantics match the reducer/projector queue
// contract (see go/internal/storage/postgres):
//
//   - fact_work_items residual: any row not in a clean terminal status. A
//     'dead_letter' or 'failed' row counts as residual on purpose — a drained
//     pipeline has no dead letters.
//   - shared_projection_intents nonterminal: completed_at IS NULL. Per B-13
//     (#3859), the repo_dependency domain is the primary gate, so its subset is
//     reported separately.
const drainCountsSQL = `
SELECT
    (SELECT count(*) FROM fact_work_items
     WHERE status NOT IN ('succeeded', 'superseded')),
    (SELECT count(*) FROM fact_work_items
     WHERE status = 'dead_letter'),
    (SELECT count(*) FROM shared_projection_intents
     WHERE completed_at IS NULL),
    (SELECT count(*) FROM shared_projection_intents
     WHERE completed_at IS NULL
       AND NOT (projection_domain = ANY(string_to_array($1, ',')))),
    (SELECT count(*) FROM shared_projection_intents
     WHERE completed_at IS NULL
       AND projection_domain = ANY(string_to_array($1, ','))),
    (SELECT count(*) FROM shared_projection_intents
     WHERE completed_at IS NULL AND projection_domain = 'repo_dependency'),
    (SELECT count(DISTINCT projection_domain) FROM shared_projection_intents
     WHERE projection_domain = ANY(string_to_array($2, ','))),
    (SELECT count(*) FROM cross_scope_completion_events
     WHERE status IN ('pending', 'claimed', 'running', 'retrying')),
    (SELECT count(*) FROM shared_projection_unroutable_intents)`

// drainQuerier reads the current queue residuals. Defined here where it is
// consumed so tests can fake it without a database.
type drainQuerier interface {
	Counts(ctx context.Context) (DrainCounts, error)
	// ResidualBreakdown groups the rows Counts counted as residual. Called only
	// when the drain has already failed, so its cost never lands on a passing
	// run, and a failure to read it degrades the message rather than the verdict.
	ResidualBreakdown(ctx context.Context) ([]residualRow, error)
	// CompletionEventBreakdown names the producer queues keeping the completion
	// ledger nonterminal. Like ResidualBreakdown, it is failure-path only.
	CompletionEventBreakdown(ctx context.Context) ([]completionEventRow, error)
}

// sqlDrainQuerier reads drain counts from Postgres. advisoryDomains is the
// comma-separated set of shared_projection_intents domains whose nonterminal rows
// are reported but do not block the gate.
type sqlDrainQuerier struct {
	db               *sql.DB
	advisoryDomains  string
	populatedDomains string
}

// openDrainQuerier opens a Postgres connection from the environment using the
// same loader the services under test use.
func openDrainQuerier(ctx context.Context, getenv func(string) string, advisoryDomains, populatedDomains string) (*sqlDrainQuerier, func(), error) {
	db, err := runtimecfg.OpenPostgres(ctx, getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	return &sqlDrainQuerier{db: db, advisoryDomains: advisoryDomains, populatedDomains: populatedDomains}, func() { _ = db.Close() }, nil
}

func (q *sqlDrainQuerier) Counts(ctx context.Context) (DrainCounts, error) {
	var counts DrainCounts
	err := q.db.QueryRowContext(ctx, drainCountsSQL, q.advisoryDomains, q.populatedDomains).Scan(
		&counts.FactWorkItemsResidual,
		&counts.FactWorkItemsDeadLetter,
		&counts.SharedIntentsNonterminal,
		&counts.SharedIntentsRequiredNonterminal,
		&counts.SharedIntentsAdvisoryNonterminal,
		&counts.RepoDependencyNonterminal,
		&counts.PopulatedDomainsPresent,
		&counts.CrossScopeCompletionEventsNonterminal,
		&counts.SharedIntentsUnroutableQuarantined,
	)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("read atomic drain snapshot: %w", err)
	}
	return counts, nil
}

// pollUntilDrained polls q until the queues are within the snapshot bounds AND
// the pipeline has been observed populated, or the context/timeout expires.
//
// expectedPopulatedDomains is the populated-then-drained guard: the poll will not
// accept a drained reading until it has observed at least that many
// require-populated domains present (i.e. the reducer actually emitted work).
// Without it, a poll that fires before the reducer starts reads an empty 0/0 and
// would pass on an unreduced pipeline. "Populated" is sticky across polls because
// completed intents are also counted, so a domain that drained between polls still
// counts as observed.
//
// It always returns the last observed counts so the caller can report the residual
// even on a timeout. ok reports whether both populated and drained held.
//
// progress and progressEvery are the periodic-residual fix for #6149 follow-up
// item 7: before this, the drain's residual was only ever reported at the
// bound (runDrains's post-return diagnostic), so "still draining" and
// "wedged" produced identical silence while the poll was actually running --
// only the status class at timeout distinguished them, and reconstructing
// which one happened cost real time diagnosing a flake. When progress is
// non-nil and progressEvery > 0, one line is written at most once per
// progressEvery of wall-clock time (never once per poll -- the default
// 2s poll interval against a 10-minute timeout would be up to 300 lines,
// which is noise, not signal), naming the SAME fields runDrains's own
// timeout message reports, so a reader tailing output sees the identical
// vocabulary trend toward (or stuck short of) that final line. Passing a nil
// writer or a non-positive progressEvery disables this entirely -- every
// pre-existing caller does exactly that, so this is strictly additive.
func pollUntilDrained(
	ctx context.Context,
	q drainQuerier,
	a DrainAssertions,
	expectedPopulatedDomains int,
	timeout, poll time.Duration,
	progress io.Writer,
	progressEvery time.Duration,
) (counts DrainCounts, ok bool, err error) {
	deadline := time.Now().Add(timeout)
	populated := expectedPopulatedDomains <= 0
	progressEnabled := progress != nil && progressEvery > 0
	start := time.Now()
	lastProgress := start
	for {
		counts, err = q.Counts(ctx)
		if err != nil {
			return counts, false, err
		}
		if counts.PopulatedDomainsPresent >= int64(expectedPopulatedDomains) {
			populated = true
		}
		if populated && counts.Drained(a) {
			return counts, true, nil
		}
		if progressEnabled && time.Since(lastProgress) >= progressEvery {
			lastProgress = time.Now()
			_, _ = fmt.Fprintf(progress,
				"drains: still polling after %s (fact residual=%d, required intents=%d, completion events=%d, populated domains=%d/%d)\n",
				lastProgress.Sub(start).Round(time.Second),
				counts.FactWorkItemsResidual, counts.SharedIntentsRequiredNonterminal,
				counts.CrossScopeCompletionEventsNonterminal,
				counts.PopulatedDomainsPresent, expectedPopulatedDomains,
			)
		}
		if !time.Now().Before(deadline) {
			return counts, false, nil
		}
		select {
		case <-ctx.Done():
			return counts, false, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// residualRow is one (domain, status, failure_class) group of work items left
// behind when the drain gave up. The drain already counts these rows; grouping
// them costs one extra query and turns an unactionable number into a diagnosis.
//
// FailureMessage carries the error text behind the group. failure_class is a
// triage bucket the projector assigns ("projection_bug", "input_invalid"), not
// the failure: a real reducer defect and a machine-contention timeout both land
// in the same bucket. The gate destroys its Postgres and its temp dir on exit,
// so the moment this row is not printed the error text is unrecoverable and the
// two are indistinguishable afterwards (#6306).
type residualRow struct {
	Domain       string
	Status       string
	FailureClass string
	Count        int64
	// FailureMessage is the distinct failure_message text for this group,
	// already collapsed and length-bounded by residualBreakdownSQL. Empty when
	// no row in the group recorded one (a pending row never failed).
	FailureMessage string
}

// residualBreakdownSQL groups whatever the residual count counted. Same
// predicate as factWorkItemsResidualSQL so the totals cannot disagree.
//
// The message column aggregates WITHIN the existing grouping rather than
// joining failure_message into the GROUP BY. That keeps the returned row count
// byte-identical to what it was before messages existed, which matters because
// ResidualWorkItems hands these same rows to the zero-correlation diagnosis: a
// finer grouping would have silently changed that unrelated message too. That
// is measured, not assumed: the live differential runs this query beside
// residualBreakdownCountsSQL and compares the four columns row by row.
// string_agg ignores NULL inputs, so a group of pending rows aggregates to NULL
// and COALESCE renders it as no message rather than a literal "<nil>".
//
// The query is assembled from two halves so a test can rebuild the pre-message
// query by derivation instead of hand-copying it, which is how a frozen
// differential goes quietly false-green.
const (
	// residualBreakdownColumnsSQL is the four columns the breakdown returned
	// before it carried a message, and the four EvaluateDrains's number is
	// reconciled against.
	residualBreakdownColumnsSQL = `SELECT domain, status, COALESCE(failure_class, ''), count(*)`

	// residualBreakdownScopeSQL is the row-set half: the drain's residual
	// predicate, its grouping, and its order. Shared verbatim by the shipped
	// query and by the reference query the live differential compares against.
	residualBreakdownScopeSQL = `
FROM fact_work_items
WHERE status NOT IN ('succeeded', 'superseded')
GROUP BY domain, status, COALESCE(failure_class, '')
ORDER BY count(*) DESC, domain, status`
)

// residualBreakdownCountsSQL is the breakdown WITHOUT the message column: the
// query as it stood before #6306. The live differential runs it beside the
// shipped one to prove the message column changed the output and not the row
// set. Derived from the same two constants, so it cannot drift away from
// production and start proving nothing.
func residualBreakdownCountsSQL() string {
	return residualBreakdownColumnsSQL + residualBreakdownScopeSQL
}

// #nosec G202 -- every operand is a compile-time constant: the two SQL halves
// above are untyped string constants, and residualMessageAggregateSQL formats
// only residualMessageColumnSQL (a constant) and residualMessageFetchLen (an
// int constant). No caller input, query parameter, or database value reaches
// this string, so there is no injection surface for the concatenation to open.
var residualBreakdownSQL = residualBreakdownColumnsSQL + ",\n       " +
	residualMessageAggregateSQL() + residualBreakdownScopeSQL

// readinessDeferredFailureClasses are the failure classes a work item
// self-assigns when a readiness gate defers it: it is waiting for an upstream
// phase to commit, not failing on its own merits. The reducer exempts these from
// the retry budget for that reason (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go).
//
// Listed here rather than imported because this is a diagnostic label, not a
// control decision: a class missing from this list is reported as live work,
// which is a slightly less precise message and never a wrong verdict. Keeping
// the gate free of a dependency on reducer internals is worth that trade.
var readinessDeferredFailureClasses = map[string]bool{
	"aws_cloud_runtime_drift_state_pending":    true,
	"aws_cloud_runtime_drift_write_superseded": true,
	"secrets_iam_endpoint_not_ready":           true,
	"kubernetes_correlation_nodes_not_ready":   true,
	"gcp_relationship_nodes_not_ready":         true,
	"ec2_instance_identity_nodes_not_ready":    true,
	"cross_scope_producer_not_ready":           true,
	// #5717: an ec2_instance_uses_ami edge waiting on the EC2 instance node
	// phase. Without this entry the drain breakdown counts it as live work and
	// reports "the pipeline just needed longer" for a queue that is actually
	// blocked on a precondition.
	"aws_relationship_ec2_instance_nodes_not_ready": true,
}

// formatResidualBreakdown renders the residual rows as one line for the drain
// timeout message.
//
// The distinction it draws is the one the bare count cannot: residual made
// entirely of readiness deferrals means the pipeline stopped making progress and
// is waiting on a precondition, which more time will not fix if nothing is left
// to satisfy it. Residual containing live work means the drain simply ran out of
// budget. Those need opposite responses, and today both print "residual=N".
func formatResidualBreakdown(rows []residualRow) string {
	if len(rows) == 0 {
		return ""
	}

	var live, deferred, deadLetter, failed int64
	details := make([]string, 0, len(rows))
	for _, row := range rows {
		switch {
		case row.Status == "dead_letter":
			deadLetter += row.Count
		// `failed` is terminal, same as dead_letter. Postgres already draws this
		// line: the outstanding-work count in generation_lifecycle_sql.go is
		// `status IN ('pending','claimed','running','retrying')`, which excludes
		// it. Counting a failed row as live would report a stuck pipeline as a
		// busy one and send the reader looking for progress that is not coming.
		case row.Status == "failed":
			failed += row.Count
		case readinessDeferredFailureClasses[row.FailureClass]:
			deferred += row.Count
		default:
			live += row.Count
		}
		details = append(details, fmt.Sprintf("%s=%d", residualGroupLabel(row), row.Count))
	}

	summary := fmt.Sprintf("live=%d readiness-deferred=%d dead_letter=%d failed=%d", live, deferred, deadLetter, failed)
	// Only claim "every residual row is waiting" when that is literally true.
	// A terminal row in the residual is a different failure with a different
	// owner, and burying it under a readiness story sends the reader the wrong
	// way — so any dead_letter or failed row suppresses the claim.
	if live == 0 && deferred > 0 && deadLetter == 0 && failed == 0 {
		summary += " — no live work remained: every residual row is waiting on a readiness precondition, so more drain time would not have helped"
	}
	return summary + " [" + strings.Join(details, " ") + "]" + formatResidualMessages(rows)
}

// ResidualBreakdown implements drainQuerier.
func (q *sqlDrainQuerier) ResidualBreakdown(ctx context.Context) ([]residualRow, error) {
	rows, err := q.db.QueryContext(ctx, residualBreakdownSQL)
	if err != nil {
		return nil, fmt.Errorf("residual breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []residualRow
	for rows.Next() {
		var r residualRow
		if err := rows.Scan(&r.Domain, &r.Status, &r.FailureClass, &r.Count, &r.FailureMessage); err != nil {
			return nil, fmt.Errorf("scan residual breakdown: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate residual breakdown: %w", err)
	}
	return out, nil
}

type completionEventRow struct {
	ProducerDomain string
	Status         string
	Count          int64
}

const completionEventBreakdownSQL = `
SELECT producer_domain, status, count(*)
FROM cross_scope_completion_events
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY producer_domain, status
ORDER BY count(*) DESC, producer_domain, status`

// CompletionEventBreakdown implements drainQuerier.
func (q *sqlDrainQuerier) CompletionEventBreakdown(ctx context.Context) ([]completionEventRow, error) {
	rows, err := q.db.QueryContext(ctx, completionEventBreakdownSQL)
	if err != nil {
		return nil, fmt.Errorf("completion-event breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []completionEventRow
	for rows.Next() {
		var row completionEventRow
		if err := rows.Scan(&row.ProducerDomain, &row.Status, &row.Count); err != nil {
			return nil, fmt.Errorf("scan completion-event breakdown: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate completion-event breakdown: %w", err)
	}
	return out, nil
}

func formatCompletionEventBreakdown(rows []completionEventRow) string {
	details := make([]string, 0, len(rows))
	for _, row := range rows {
		details = append(details, fmt.Sprintf("%s/%s=%d", row.ProducerDomain, row.Status, row.Count))
	}
	return strings.Join(details, " ")
}

// ResidualWorkItems implements pipelineStateQuerier.
//
// Deliberately delegates to ResidualBreakdown rather than issuing its own
// query. The two names describe one predicate, and two SQL
// statements expressing one predicate is how they drift apart: a later change
// to the residual predicate would silently stop matching what the
// zero-correlation diagnosis reports.
func (q *sqlDrainQuerier) ResidualWorkItems(ctx context.Context) ([]residualRow, error) {
	return q.ResidualBreakdown(ctx)
}
