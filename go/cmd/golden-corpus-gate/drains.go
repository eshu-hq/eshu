// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for database/sql

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// SQL for the B-7(a) drain gate. The status set and completed_at semantics match
// the reducer/projector queue contract (see go/internal/storage/postgres):
//
//   - fact_work_items residual: any row not in a clean terminal status. A
//     'dead_letter' or 'failed' row counts as residual on purpose — a drained
//     pipeline has no dead letters.
//   - shared_projection_intents nonterminal: completed_at IS NULL. Per B-13
//     (#3859), the repo_dependency domain is the primary gate, so its subset is
//     reported separately.
const (
	factWorkItemsResidualSQL = `
SELECT count(*) FROM fact_work_items
WHERE status NOT IN ('succeeded', 'superseded')`

	factWorkItemsDeadLetterSQL = `
SELECT count(*) FROM fact_work_items
WHERE status = 'dead_letter'`

	sharedIntentsNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL`

	// $1 is a comma-separated advisory-domain list. string_to_array('', ',')
	// yields an empty array, so `projection_domain = ANY(...)` is false for every
	// row and an empty advisory list cleanly degrades to "required = total,
	// advisory = 0". The caller trims each element before joining, so a list like
	// "a, b" cannot smuggle a leading space into a domain name.
	sharedIntentsRequiredNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL
  AND NOT (projection_domain = ANY(string_to_array($1, ',')))`

	sharedIntentsAdvisoryNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL
  AND projection_domain = ANY(string_to_array($1, ','))`

	repoDependencyNonterminalSQL = `
SELECT count(*) FROM shared_projection_intents
WHERE completed_at IS NULL AND projection_domain = 'repo_dependency'`

	// Counts how many of the require-populated domains have at least one intent
	// (completed or not). Counting completed rows too is deliberate: even if the
	// reducer emitted and completed a domain's intents before the first poll, we
	// still observe that it ran — only a reducer that never ran reads 0 here.
	sharedIntentsPopulatedDomainsSQL = `
SELECT count(DISTINCT projection_domain) FROM shared_projection_intents
WHERE projection_domain = ANY(string_to_array($1, ','))`
)

// drainQuerier reads the current queue residuals. Defined here where it is
// consumed so tests can fake it without a database.
type drainQuerier interface {
	Counts(ctx context.Context) (DrainCounts, error)
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

func (q *sqlDrainQuerier) scalar(ctx context.Context, query string, args ...any) (int64, error) {
	var n int64
	if err := q.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (q *sqlDrainQuerier) Counts(ctx context.Context) (DrainCounts, error) {
	fact, err := q.scalar(ctx, factWorkItemsResidualSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("fact_work_items residual: %w", err)
	}
	deadLetter, err := q.scalar(ctx, factWorkItemsDeadLetterSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("fact_work_items dead_letter: %w", err)
	}
	intents, err := q.scalar(ctx, sharedIntentsNonterminalSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("shared_projection_intents nonterminal: %w", err)
	}
	required, err := q.scalar(ctx, sharedIntentsRequiredNonterminalSQL, q.advisoryDomains)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("shared_projection_intents required nonterminal: %w", err)
	}
	advisory, err := q.scalar(ctx, sharedIntentsAdvisoryNonterminalSQL, q.advisoryDomains)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("shared_projection_intents advisory nonterminal: %w", err)
	}
	repoDep, err := q.scalar(ctx, repoDependencyNonterminalSQL)
	if err != nil {
		return DrainCounts{}, fmt.Errorf("repo_dependency nonterminal: %w", err)
	}
	var populated int64
	if q.populatedDomains != "" {
		populated, err = q.scalar(ctx, sharedIntentsPopulatedDomainsSQL, q.populatedDomains)
		if err != nil {
			return DrainCounts{}, fmt.Errorf("populated domains present: %w", err)
		}
	}
	return DrainCounts{
		FactWorkItemsResidual:            fact,
		FactWorkItemsDeadLetter:          deadLetter,
		SharedIntentsNonterminal:         intents,
		SharedIntentsRequiredNonterminal: required,
		SharedIntentsAdvisoryNonterminal: advisory,
		RepoDependencyNonterminal:        repoDep,
		PopulatedDomainsPresent:          populated,
	}, nil
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
