// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
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
     WHERE status IN ('pending', 'claimed', 'running', 'retrying'))`

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
func pollUntilDrained(
	ctx context.Context,
	q drainQuerier,
	a DrainAssertions,
	expectedPopulatedDomains int,
	timeout, poll time.Duration,
) (counts DrainCounts, ok bool, err error) {
	deadline := time.Now().Add(timeout)
	populated := expectedPopulatedDomains <= 0
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
type residualRow struct {
	Domain       string
	Status       string
	FailureClass string
	Count        int64
}

// residualBreakdownSQL groups whatever the residual count counted. Same
// predicate as factWorkItemsResidualSQL so the totals cannot disagree.
const residualBreakdownSQL = `
SELECT domain, status, COALESCE(failure_class, ''), count(*)
FROM fact_work_items
WHERE status NOT IN ('succeeded', 'superseded')
GROUP BY domain, status, COALESCE(failure_class, '')
ORDER BY count(*) DESC, domain, status`

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
		detail := fmt.Sprintf("%s/%s", row.Domain, row.Status)
		if row.FailureClass != "" {
			detail += "/" + row.FailureClass
		}
		details = append(details, fmt.Sprintf("%s=%d", detail, row.Count))
	}

	summary := fmt.Sprintf("live=%d readiness-deferred=%d dead_letter=%d failed=%d", live, deferred, deadLetter, failed)
	// Only claim "every residual row is waiting" when that is literally true.
	// A terminal row in the residual is a different failure with a different
	// owner, and burying it under a readiness story sends the reader the wrong
	// way — so any dead_letter or failed row suppresses the claim.
	if live == 0 && deferred > 0 && deadLetter == 0 && failed == 0 {
		summary += " — no live work remained: every residual row is waiting on a readiness precondition, so more drain time would not have helped"
	}
	return summary + " [" + strings.Join(details, " ") + "]"
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
		if err := rows.Scan(&r.Domain, &r.Status, &r.FailureClass, &r.Count); err != nil {
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
