// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// selectionObservationPriorRowsQuery reads one selector's stored rows: the
// states the last evaluation left behind, which the mass-miss guard and the
// confirmation counters compare against. The (selector_id, evaluated_at
// DESC) index from migration 164 serves the selector equality.
const selectionObservationPriorRowsQuery = `
SELECT scope_id, state, evaluated_at
FROM repository_selection_observations
WHERE selector_id = $1
`

// selectionObservationOrgScopesQuery enumerates the candidate scopes for a
// githubOrg evaluation: every git default-branch repository scope. The
// (source_system, scope_kind) prefix of ingestion_scopes_source_idx bounds
// the scan to those rows; the caller keeps only the scopes whose
// payload repo_slug sits under the evaluating org. Non-default-branch
// (repository_ref) scopes are excluded here, not in Go: a pinned-ref scope
// never appears in an org listing, so admitting it would condemn it as
// not_listed on every evaluation.
const selectionObservationOrgScopesQuery = `
SELECT scope_id, payload->>'repo_slug' AS repo_slug
FROM ingestion_scopes
WHERE source_system = 'git'
  AND scope_kind = 'repository'
`

// recordSelectionEvaluationQuery upserts one evaluation's rows. The input
// arrays are zipped by position, deduplicated, and locked in scope_id order:
// a duplicate inside one statement fails with SQLSTATE 21000, and two
// evaluations that lock overlapping rows in different orders deadlock
// (SQLSTATE 40P01). The conflict arm keeps the confirmation counters, not
// the caller: a re-observed state keeps its state_since and gains a cycle,
// a changed state restarts both, and last_listed_at advances only for
// selected rows. A zero GitHub id means the listing carried none, so the
// stored id survives; NULLIF keeps the unknown marker out of the table.
const recordSelectionEvaluationQuery = `
INSERT INTO repository_selection_observations AS observation (
    scope_id, selector_id, state, github_repository_id, evaluated_at,
    liveness_window_seconds, state_since, state_cycle_count, last_listed_at
)
SELECT scope_id, $2, state, NULLIF(github_id, 0), $3::timestamptz, $4::integer, $3::timestamptz, 1,
       CASE WHEN state = 'selected' THEN $3::timestamptz ELSE NULL END
FROM (
    SELECT DISTINCT ids.scope_id, states.state, github_ids.github_id
    FROM unnest($1::text[]) WITH ORDINALITY AS ids(scope_id, rank)
    JOIN unnest($5::text[]) WITH ORDINALITY AS states(state, rank) USING (rank)
    JOIN unnest($6::bigint[]) WITH ORDINALITY AS github_ids(github_id, rank) USING (rank)
    ORDER BY 1
) AS evaluated
ON CONFLICT (scope_id, selector_id) DO UPDATE
SET state = EXCLUDED.state,
    github_repository_id = COALESCE(EXCLUDED.github_repository_id, observation.github_repository_id),
    evaluated_at = EXCLUDED.evaluated_at,
    liveness_window_seconds = EXCLUDED.liveness_window_seconds,
    state_since = CASE
        WHEN observation.state = EXCLUDED.state THEN observation.state_since
        ELSE EXCLUDED.evaluated_at
    END,
    state_cycle_count = CASE
        WHEN observation.state = EXCLUDED.state THEN observation.state_cycle_count + 1
        ELSE 1
    END,
    last_listed_at = CASE
        WHEN EXCLUDED.state = 'selected' THEN EXCLUDED.evaluated_at
        ELSE observation.last_listed_at
    END
`

// selectionObservationsByScopeQuery reads one scope's observation rows
// across every selector for the freshness read. The (scope_id,
// selector_id) primary key prefix serves the scope equality.
const selectionObservationsByScopeQuery = `
SELECT selector_id, state, github_repository_id, evaluated_at,
       liveness_window_seconds, state_since, state_cycle_count, last_listed_at
FROM repository_selection_observations
WHERE scope_id = $1
ORDER BY selector_id
`

// latestGenerationObservedAtQuery reads the newest observed_at across one
// scope's generations of any status for freshness rule 4 (#7625): a scope
// whose generation was observed after its latest exclusion began reads
// excluded_still_ingested. scope_generations_scope_generation_idx bounds
// the read to the scope's own rows (a Bitmap Index Scan on the seeded live
// corpus); a scope has few generations, so MAX over that indexed set needs
// no further support (the live test records the EXPLAIN).
const latestGenerationObservedAtQuery = `
SELECT MAX(observed_at)
FROM scope_generations
WHERE scope_id = $1
`

// SelectionObservationStore records and reads the #7625 repository
// selection observations: per-(scope, selector) selection states evaluated
// by the git collector. It owns no foreign key and writes nothing back to
// ingestion_scopes; phase 1 observes and reports.
type SelectionObservationStore struct {
	database db.ExecQueryer
	beginner db.Beginner
}

// NewSelectionObservationStore constructs a selection observation store.
// Recording needs transactions; when database does not implement db.Beginner
// the reads still work and RecordSelectionEvaluation reports the missing
// transaction support instead of writing half an evaluation.
func NewSelectionObservationStore(database db.ExecQueryer) SelectionObservationStore {
	store := SelectionObservationStore{database: database}
	if beginner, ok := database.(db.Beginner); ok {
		store.beginner = beginner
	}
	return store
}

// RecordSelectionEvaluation records one selector's evaluation in a single
// transaction: it reads the selector's prior rows, enumerates the known
// same-org scopes (githubOrg only), computes a state per scope, applies the
// safety rails, and upserts the rows.
//
// Safety rails, in order:
//
//  1. An empty discovery listing writes nothing: an empty org listing
//     signals an API failure, not an emptied org.
//  2. The mass-miss guard writes nothing when the newly-missing count --
//     known scopes absent from the listing whose prior row is missing or
//     selected -- exceeds max(10, 10%) of known scopes. Scopes already
//     recorded as excluded do not count: without that, a small org with
//     long-missing repositories would trip the guard on every cycle and its
//     selected rows would lapse into unknown.
//
// Explicit mode skips enumeration and both rails: it writes positive
// selected rows for its configured repositories only, since its listing is
// a configuration, not a complete org census.
//
// The outcome carries the per-state row counts and the selector's newest
// prior evaluated_at (zero on a first evaluation) for the observer's
// liveness-gap check.
func (s SelectionObservationStore) RecordSelectionEvaluation(
	ctx context.Context,
	evaluation scope.SelectionEvaluation,
) (scope.SelectionEvaluationOutcome, error) {
	if s.database == nil {
		return scope.SelectionEvaluationOutcome{}, fmt.Errorf("selection observation store database is required")
	}
	if s.beginner == nil {
		return scope.SelectionEvaluationOutcome{}, fmt.Errorf("selection observation store requires transactions")
	}
	normalized, err := normalizeSelectionEvaluation(evaluation)
	if err != nil {
		return scope.SelectionEvaluationOutcome{}, err
	}

	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return scope.SelectionEvaluationOutcome{}, fmt.Errorf("begin selection evaluation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	prior, priorEvaluatedAt, err := readSelectionPriorRows(ctx, tx, normalized.SelectorID)
	if err != nil {
		return scope.SelectionEvaluationOutcome{}, err
	}

	outcome := scope.SelectionEvaluationOutcome{
		Outcome:          scope.SelectionEvaluationEvaluated,
		PriorEvaluatedAt: priorEvaluatedAt,
	}
	plan := normalized.positiveRows()
	if normalized.SelectorKind == scope.SelectionSelectorKindGitHubOrg {
		known, err := readSelectionOrgScopes(ctx, tx, normalized.Org)
		if err != nil {
			return scope.SelectionEvaluationOutcome{}, err
		}
		outcome.KnownScopes = len(known)
		if normalized.listingEmpty() {
			outcome.Outcome = scope.SelectionEvaluationGuardTripped
			return outcome, tx.Commit()
		}
		plan = normalized.fullPlan(known)
		outcome.NewlyMissing = selectionNewlyMissing(plan, prior)
		if selectionMassMissTripped(outcome.NewlyMissing, len(known)) {
			outcome.Outcome = scope.SelectionEvaluationGuardTripped
			return outcome, tx.Commit()
		}
	}
	if len(plan.rows) == 0 {
		return outcome, tx.Commit()
	}
	if err := writeSelectionEvaluationRows(ctx, tx, normalized, plan); err != nil {
		return scope.SelectionEvaluationOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return scope.SelectionEvaluationOutcome{}, fmt.Errorf("commit selection evaluation: %w", err)
	}
	outcome.Selected = plan.count(scope.SelectionStateSelected)
	outcome.ArchivedExcluded = plan.count(scope.SelectionStateArchivedExcluded)
	outcome.RuleExcluded = plan.count(scope.SelectionStateRuleExcluded)
	outcome.NotListed = plan.count(scope.SelectionStateNotListed)
	return outcome, nil
}

// ReadSelectionObservations returns one scope's observation rows across
// every selector, ordered by selector, for the freshness read.
func (s SelectionObservationStore) ReadSelectionObservations(
	ctx context.Context,
	scopeID string,
) ([]statuspkg.SelectionObservationRow, error) {
	if s.database == nil {
		return nil, fmt.Errorf("selection observation store database is required")
	}
	return readSelectionObservationRows(ctx, s.database, scopeID)
}

// LatestGenerationObservedAt returns the newest observed_at across one
// scope's generations of any status, or the zero time when the scope never
// produced a generation. Freshness rule 4 compares it against the latest
// live state_since.
func (s SelectionObservationStore) LatestGenerationObservedAt(
	ctx context.Context,
	scopeID string,
) (time.Time, error) {
	if s.database == nil {
		return time.Time{}, fmt.Errorf("selection observation store database is required")
	}
	return readLatestGenerationObservedAt(ctx, s.database, scopeID)
}

// readSelectionObservationRows reads one scope's observation rows across
// every selector for the freshness read. It takes a db.Queryer (not the
// store) so RepositoryFreshnessStore reuses it over its own handle.
func readSelectionObservationRows(
	ctx context.Context,
	queryer db.Queryer,
	scopeID string,
) ([]statuspkg.SelectionObservationRow, error) {
	rows, err := queryer.QueryContext(ctx, selectionObservationsByScopeQuery, scopeID)
	if err != nil {
		return nil, fmt.Errorf("read selection observations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []statuspkg.SelectionObservationRow
	for rows.Next() {
		var row statuspkg.SelectionObservationRow
		var githubID sql.NullInt64
		var lastListedAt sql.NullTime
		if scanErr := rows.Scan(
			&row.SelectorID,
			&row.State,
			&githubID,
			&row.EvaluatedAt,
			&row.LivenessWindowSeconds,
			&row.StateSince,
			&row.StateCycleCount,
			&lastListedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("read selection observations: %w", scanErr)
		}
		row.EvaluatedAt = row.EvaluatedAt.UTC()
		row.StateSince = row.StateSince.UTC()
		if githubID.Valid {
			row.GitHubID = githubID.Int64
		}
		if lastListedAt.Valid {
			row.LastListedAt = lastListedAt.Time.UTC()
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read selection observations: %w", err)
	}
	return out, nil
}

// readLatestGenerationObservedAt returns the newest observed_at across one
// scope's generations, or the zero time when the scope has none. It takes a
// db.Queryer (not the store) so RepositoryFreshnessStore reuses it over its
// own handle.
func readLatestGenerationObservedAt(
	ctx context.Context,
	queryer db.Queryer,
	scopeID string,
) (time.Time, error) {
	rows, err := queryer.QueryContext(ctx, latestGenerationObservedAtQuery, scopeID)
	if err != nil {
		return time.Time{}, fmt.Errorf("read latest generation observed_at: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return time.Time{}, rows.Err()
	}
	var latest sql.NullTime
	if scanErr := rows.Scan(&latest); scanErr != nil {
		return time.Time{}, fmt.Errorf("read latest generation observed_at: %w", scanErr)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("read latest generation observed_at: %w", err)
	}
	if !latest.Valid {
		return time.Time{}, nil
	}
	return latest.Time.UTC(), nil
}
