// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// schemaSQL is the repository_selection_observations DDL that migration 166
// applies after its leading comment block. A test keeps the two identical.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS repository_selection_observations (
    scope_id TEXT NOT NULL,
    selector_id TEXT NOT NULL,
    selector_kind TEXT NOT NULL CHECK (selector_kind IN ('github_org', 'explicit')),
    selector_owner TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('selected', 'archived_excluded', 'rule_excluded', 'not_listed')),
    github_repo_id BIGINT NULL,
    last_listed_at TIMESTAMPTZ NULL,
    state_since TIMESTAMPTZ NOT NULL,
    state_cycle_count INTEGER NOT NULL CHECK (state_cycle_count > 0),
    evaluated_at TIMESTAMPTZ NOT NULL,
    liveness_window_seconds INTEGER NOT NULL CHECK (liveness_window_seconds > 0),
    PRIMARY KEY (scope_id, selector_id)
);

CREATE INDEX IF NOT EXISTS repository_selection_observations_selector_idx
    ON repository_selection_observations (selector_id);
`

// knownScopesQuery reads one org's git repository scopes without locking
// them. repository_ref scopes have their own scope_kind and are excluded; the
// org comparison is case-insensitive because GitHub org names are. A
// non-empty $2 also requires the remote host: remote_url is stored as
// https://<lowercase host>/<path> by repositoryidentity.NormalizeRemoteURL,
// so its third '/' field is the host. An empty $2 makes the host predicate
// true for every row, so the read matches the slug-only partition.
const knownScopesQuery = `
SELECT scope_id, payload->>'repo_slug'
FROM ingestion_scopes
WHERE source_system = 'git'
  AND scope_kind = 'repository'
  AND collector_kind = 'git'
  AND lower(split_part(payload->>'repo_slug', '/', 1)) = $1
  AND ($2 = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = $2)
`

// knownScopesPrefixQuery reads one nested GitLab group's git repository
// scopes without locking them. $1 is the lowercased group full path plus a
// trailing slash, so acme/platform/ never matches acme/platformer. It uses
// starts_with instead of LIKE because _ is a LIKE wildcard and legal in
// GitLab paths. repository_ref scopes stay excluded by scope_kind, and the
// host predicate matches knownScopesQuery. See the prove-first evidence in
// the store README ("Performance Evidence (#7765)"): no index serves this
// read, by measurement.
const knownScopesPrefixQuery = `
SELECT scope_id, payload->>'repo_slug'
FROM ingestion_scopes
WHERE source_system = 'git'
  AND scope_kind = 'repository'
  AND collector_kind = 'git'
  AND starts_with(lower(payload->>'repo_slug'), $1)
  AND ($2 = '' OR lower(split_part(payload->>'remote_url', '/', 3)) = $2)
`

// observationsQuery reads every stored observation of one selector,
// including the upstream probe verdict (#8454).
const observationsQuery = `
SELECT scope_id, state, github_repo_id, last_listed_at, state_since,
       state_cycle_count, evaluated_at, liveness_window_seconds,
       upstream_classification, successor_slug, classified_at
FROM repository_selection_observations
WHERE selector_id = $1
`

// upsertClassificationsQuery writes probe verdicts for one selector's
// Confirmed not_listed rows. Only not_listed rows advance, and only when the
// stored classified_at is older than the probe time, so a lagging replica's
// probe never overwrites a newer verdict. The batch is sorted by scope_id
// in Go and re-sorted in the subquery, so concurrent writers meet the rows
// in one order; shard 0 is still the selector's only writer per cycle.
// Inconclusive probes never reach this statement; their prior verdict stands
// untouched.
const upsertClassificationsQuery = `
UPDATE repository_selection_observations AS o SET
    upstream_classification = s.classification,
    successor_slug = NULLIF(s.successor_slug, ''),
    classified_at = $2::timestamptz
FROM (
    SELECT scope_id, classification, successor_slug
    FROM unnest($3::text[], $4::text[], $5::text[]) AS t(scope_id, classification, successor_slug)
    ORDER BY scope_id
) AS s
WHERE o.scope_id = s.scope_id
  AND o.selector_id = $1
  AND o.state = 'not_listed'
  AND (o.classified_at IS NULL OR o.classified_at <= $2::timestamptz)
`

// upsertObservationsQuery writes one evaluation in one statement. Rows are
// inserted in scope_id order so two replicas writing the same selector lock
// rows in the same order and cannot deadlock. A stored row only advances when
// the batch is newer, so a replay or a lagging replica changes nothing. In the
// DO UPDATE arm o is the stored row: a repeated state keeps state_since and
// adds a cycle, a changed state restarts both at the batch time. A probe
// verdict belongs to a not_listed row: any other state clears the three probe
// columns, so a relisted scope never carries a stale verdict and re-probes
// fresh if it unlists again. This must match membership's project function;
// the live store test pins it.
const upsertObservationsQuery = `
INSERT INTO repository_selection_observations AS o (
    scope_id, selector_id, selector_kind, selector_owner, state, github_repo_id,
    last_listed_at, state_since, state_cycle_count,
    evaluated_at, liveness_window_seconds
)
SELECT s.scope_id, $1, $2, $3, s.state, NULLIF(s.github_repo_id, 0),
       CASE WHEN s.state = 'not_listed' THEN NULL ELSE $4::timestamptz END,
       $4::timestamptz, 1,
       $4::timestamptz, $5
FROM unnest($6::text[], $7::text[], $8::bigint[]) AS s(scope_id, state, github_repo_id)
ORDER BY s.scope_id
ON CONFLICT (scope_id, selector_id) DO UPDATE SET
    selector_kind = EXCLUDED.selector_kind,
    selector_owner = EXCLUDED.selector_owner,
    state = EXCLUDED.state,
    github_repo_id = COALESCE(EXCLUDED.github_repo_id, o.github_repo_id),
    last_listed_at = COALESCE(EXCLUDED.last_listed_at, o.last_listed_at),
    state_since = CASE WHEN o.state = EXCLUDED.state THEN o.state_since ELSE EXCLUDED.state_since END,
    state_cycle_count = CASE WHEN o.state = EXCLUDED.state THEN o.state_cycle_count + 1 ELSE 1 END,
    evaluated_at = EXCLUDED.evaluated_at,
    liveness_window_seconds = EXCLUDED.liveness_window_seconds,
    upstream_classification = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.upstream_classification ELSE NULL END,
    successor_slug = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.successor_slug ELSE NULL END,
    classified_at = CASE WHEN EXCLUDED.state = 'not_listed' THEN o.classified_at ELSE NULL END
WHERE o.evaluated_at < EXCLUDED.evaluated_at
`

// Bounds of one DeleteExpiredObservations call: at most
// expiredSweepMaxBatches statements of expiredSweepBatchSize rows each, so a
// large backlog drains across collector cycles instead of in one long pass.
const (
	expiredSweepBatchSize  = 500
	expiredSweepMaxBatches = 20
)

// deleteExpiredObservationsQuery deletes one batch of rows, by primary key,
// whose own liveness window plus the grace period ended before $1, and
// returns each deleted row's state so the caller can count reclaimed
// not_listed rows. A $4 (not_listed) row is deleted only when its selector
// is abandoned -- no repository_selectors marker, or a marker older than the
// row's own window plus grace (#7840): membership.Evaluate reads that
// history to keep scopes already confirmed missing out of the mass-miss
// guard, so deleting it for a selector that still cycles (a guard-tripped
// cycle writes no rows, only the marker) could trip the guard forever once
// access returns. The marker is read from the statement snapshot, so a
// selector silent for window plus grace that revives in the same instant as
// a sweep can lose its history; its next evaluation then re-confirms from
// pending instead of tripping (the cycle that read the priors rewrites the
// rows). Each batch is its own statement, so locks last one batch. SKIP
// LOCKED passes over a row an upsert holds, and FOR UPDATE rechecks the
// expiry predicate against the newest row version, so a row renewed before
// the lock is never deleted. The batch is unordered on purpose: the sweep
// never waits on a lock, so no lock order is needed, and an ORDER BY forces
// a full primary-key index scan on every steady-state probe that finds
// nothing to delete.
const deleteExpiredObservationsQuery = `
WITH doomed AS (
    SELECT o.scope_id, o.selector_id
    FROM repository_selection_observations o
    LEFT JOIN repository_selectors s ON s.selector_id = o.selector_id
    WHERE o.evaluated_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz
      AND (o.state <> $4 OR s.selector_id IS NULL
           OR s.last_cycle_at + make_interval(secs => o.liveness_window_seconds) + make_interval(secs => $2::bigint) < $1::timestamptz)
    LIMIT $3
    FOR UPDATE OF o SKIP LOCKED
)
DELETE FROM repository_selection_observations o
USING doomed d
WHERE o.scope_id = d.scope_id AND o.selector_id = d.selector_id
RETURNING o.state
`

var _ membership.Store = ObservationStore{}

// ObservationStore persists repository selection observations in
// repository_selection_observations and reads the org partition of
// ingestion_scopes (#7625). It implements membership.Store.
type ObservationStore struct {
	database db.ExecQueryer
}

// NewObservationStore constructs a Postgres-backed selection observation
// store.
func NewObservationStore(database db.ExecQueryer) ObservationStore {
	return ObservationStore{database: database}
}

// KnownScopes returns the git repository scopes in partition: first-segment
// owner match, or path-prefix match for a nested group partition, compared
// case-insensitively. When the partition host is non-empty the scope's
// stored remote_url host must equal it (a scope without a remote_url then
// never matches). Owner and host are trimmed and lowercased; a blank owner
// is rejected before any query.
func (s ObservationStore) KnownScopes(ctx context.Context, partition membership.Partition) ([]membership.KnownScope, error) {
	if s.database == nil {
		return nil, errors.New("repository selection store database is required")
	}
	owner := strings.ToLower(strings.TrimSpace(partition.Owner))
	if owner == "" {
		return nil, errors.New("read known repository scopes: owner must not be blank")
	}
	host := strings.ToLower(strings.TrimSpace(partition.Host))
	query, arg := knownScopesQuery, owner
	if partition.PathPrefix {
		query, arg = knownScopesPrefixQuery, owner+"/"
	}
	rows, err := s.database.QueryContext(ctx, query, arg, host)
	if err != nil {
		return nil, fmt.Errorf("read known repository scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var known []membership.KnownScope
	for rows.Next() {
		var scope membership.KnownScope
		if err := rows.Scan(&scope.ScopeID, &scope.Slug); err != nil {
			return nil, fmt.Errorf("read known repository scopes: %w", err)
		}
		known = append(known, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read known repository scopes: %w", err)
	}
	return known, nil
}

// Observations returns every stored observation of selectorID with times in
// UTC. SQL NULLs become zero values, matching membership.Observation.
func (s ObservationStore) Observations(ctx context.Context, selectorID string) ([]membership.Observation, error) {
	if s.database == nil {
		return nil, errors.New("repository selection store database is required")
	}
	if strings.TrimSpace(selectorID) == "" {
		return nil, errors.New("read repository selection observations: selector id must not be blank")
	}
	rows, err := s.database.QueryContext(ctx, observationsQuery, selectorID)
	if err != nil {
		return nil, fmt.Errorf("read repository selection observations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var observations []membership.Observation
	for rows.Next() {
		var (
			observation    membership.Observation
			state          string
			githubRepoID   sql.NullInt64
			lastListedAt   sql.NullTime
			windowSeconds  int
			classification sql.NullString
			successor      sql.NullString
			classifiedAt   sql.NullTime
		)
		if err := rows.Scan(
			&observation.ScopeID, &state, &githubRepoID, &lastListedAt, &observation.StateSince,
			&observation.StateCycleCount, &observation.EvaluatedAt, &windowSeconds,
			&classification, &successor, &classifiedAt,
		); err != nil {
			return nil, fmt.Errorf("read repository selection observations: %w", err)
		}
		observation.State = membership.State(state)
		observation.GitHubRepoID = githubRepoID.Int64
		if lastListedAt.Valid {
			observation.LastListedAt = lastListedAt.Time.UTC()
		}
		observation.StateSince = observation.StateSince.UTC()
		observation.EvaluatedAt = observation.EvaluatedAt.UTC()
		observation.LivenessWindow = time.Duration(windowSeconds) * time.Second
		observation.Classification = membership.UpstreamClassification(classification.String)
		if successor.Valid {
			observation.SuccessorSlug = successor.String
		}
		if classifiedAt.Valid {
			observation.ClassifiedAt = classifiedAt.Time.UTC()
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository selection observations: %w", err)
	}
	return observations, nil
}

// UpsertObservations writes batch in one statement. Rows are sorted by scope
// ID and exact duplicates collapsed; two rows for one scope that disagree, an
// unknown state, a non-selected row from an explicit selector, or an invalid
// selector, time, or liveness window are rejected before anything is written.
// An empty batch writes nothing.
func (s ObservationStore) UpsertObservations(ctx context.Context, batch membership.Batch) error {
	if s.database == nil {
		return errors.New("repository selection store database is required")
	}
	rows, windowSeconds, err := validateBatch(batch)
	if err != nil {
		return fmt.Errorf("upsert repository selection observations: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	scopeIDs := make([]string, len(rows))
	states := make([]string, len(rows))
	githubRepoIDs := make([]int64, len(rows))
	for i, row := range rows {
		scopeIDs[i], states[i], githubRepoIDs[i] = row.ScopeID, string(row.State), row.GitHubRepoID
	}
	if _, err := s.database.ExecContext(ctx, upsertObservationsQuery,
		batch.Selector.ID, batch.Selector.Kind, batch.Selector.Owner,
		batch.EvaluatedAt.UTC(), windowSeconds,
		scopeIDs, states, githubRepoIDs,
	); err != nil {
		return fmt.Errorf("upsert repository selection observations: %w", err)
	}
	return nil
}

// UpsertClassifications writes rows' probe verdicts for one selector in one
// statement. Rows are sorted by scope id; a blank scope id, a blank selector
// id, a zero classified_at, or an unknown classification is rejected before
// anything is written. An empty row set writes nothing.
func (s ObservationStore) UpsertClassifications(ctx context.Context, selectorID string, rows []membership.ClassificationRow, classifiedAt time.Time) error {
	if s.database == nil {
		return errors.New("repository selection store database is required")
	}
	if strings.TrimSpace(selectorID) == "" {
		return errors.New("upsert repository selection classifications: selector id must not be blank")
	}
	if classifiedAt.IsZero() {
		return errors.New("upsert repository selection classifications: classified_at is required")
	}
	if len(rows) == 0 {
		return nil
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b membership.ClassificationRow) int { return cmp.Compare(a.ScopeID, b.ScopeID) })
	scopeIDs := make([]string, len(sorted))
	classifications := make([]string, len(sorted))
	successors := make([]string, len(sorted))
	for i, row := range sorted {
		if strings.TrimSpace(row.ScopeID) == "" {
			return errors.New("upsert repository selection classifications: scope id must not be blank")
		}
		if !validClassification(row.Classification) {
			return fmt.Errorf("upsert repository selection classifications: scope %s has unknown classification %q", row.ScopeID, row.Classification)
		}
		scopeIDs[i], classifications[i], successors[i] = row.ScopeID, string(row.Classification), row.SuccessorSlug
	}
	if _, err := s.database.ExecContext(ctx, upsertClassificationsQuery,
		selectorID, classifiedAt.UTC(), scopeIDs, classifications, successors,
	); err != nil {
		return fmt.Errorf("upsert repository selection classifications: %w", err)
	}
	return nil
}

// validClassification reports whether c is a storable probe verdict. It
// mirrors membership's rule without importing the collector writer package:
// this store must not import its callers.
func validClassification(c membership.UpstreamClassification) bool {
	switch c {
	case membership.UpstreamTransferred, membership.UpstreamRenamed,
		membership.UpstreamStillPresent, membership.UpstreamNotFoundOrInaccessible:
		return true
	default:
		return false
	}
}

// DeleteExpiredObservations deletes rows of every selector whose evaluated_at
// plus their own liveness window plus grace is before now. It runs batches of
// expiredSweepBatchSize rows, stops at the first short batch or after
// expiredSweepMaxBatches, and returns the rows deleted, including those
// deleted before an error. A row still inside its window or the grace after
// it is never deleted; a not_listed row is deleted only for an abandoned
// selector (see deleteExpiredObservationsQuery, #7840) and is counted in
// NotListedReclaimed.
func (s ObservationStore) DeleteExpiredObservations(ctx context.Context, now time.Time, grace time.Duration) (membership.SweepCounts, error) {
	switch {
	case s.database == nil:
		return membership.SweepCounts{}, errors.New("repository selection store database is required")
	case now.IsZero():
		return membership.SweepCounts{}, errors.New("delete expired repository selection observations: now is required")
	case grace < 0:
		return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: grace must not be negative, got %v", grace)
	}
	var counts membership.SweepCounts
	for range expiredSweepMaxBatches {
		batch, err := s.deleteExpiredBatch(ctx, now, grace)
		counts.Deleted += batch.Deleted
		counts.NotListedReclaimed += batch.NotListedReclaimed
		if err != nil {
			return counts, err
		}
		if batch.Deleted < expiredSweepBatchSize {
			break
		}
	}
	return counts, nil
}

// deleteExpiredBatch deletes one sweep batch and counts its rows by the
// states RETURNING reports. A scan or iteration error returns the counts
// read so far: those rows are deleted even though the batch failed.
func (s ObservationStore) deleteExpiredBatch(ctx context.Context, now time.Time, grace time.Duration) (membership.SweepCounts, error) {
	rows, err := s.database.QueryContext(ctx, deleteExpiredObservationsQuery,
		now.UTC(), int64(grace/time.Second), expiredSweepBatchSize, string(membership.StateNotListed))
	if err != nil {
		return membership.SweepCounts{}, fmt.Errorf("delete expired repository selection observations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var counts membership.SweepCounts
	for rows.Next() {
		var state string
		if err := rows.Scan(&state); err != nil {
			return counts, fmt.Errorf("delete expired repository selection observations: %w", err)
		}
		counts.Deleted++
		if membership.State(state) == membership.StateNotListed {
			counts.NotListedReclaimed++
		}
	}
	if err := rows.Err(); err != nil {
		return counts, fmt.Errorf("delete expired repository selection observations: %w", err)
	}
	return counts, nil
}

// validateBatch checks the batch header and returns its rows sorted by scope
// ID with exact duplicates removed, plus the liveness window in whole seconds.
func validateBatch(batch membership.Batch) ([]membership.Row, int64, error) {
	switch {
	case strings.TrimSpace(batch.Selector.ID) == "":
		return nil, 0, errors.New("selector id must not be blank")
	case batch.Selector.Kind != membership.KindGitHubOrg && batch.Selector.Kind != membership.KindExplicit && batch.Selector.Kind != membership.KindGitLabGroup:
		return nil, 0, fmt.Errorf("unsupported selector kind %q", batch.Selector.Kind)
	case strings.TrimSpace(batch.Selector.Owner) == "":
		return nil, 0, errors.New("selector owner must not be blank")
	case batch.EvaluatedAt.IsZero():
		return nil, 0, errors.New("evaluated_at is required")
	case batch.LivenessWindow < time.Second || batch.LivenessWindow/time.Second > math.MaxInt32:
		return nil, 0, fmt.Errorf("liveness window %v is outside 1s..%ds", batch.LivenessWindow, math.MaxInt32)
	}
	rows := slices.Clone(batch.Rows)
	for _, row := range rows {
		if strings.TrimSpace(row.ScopeID) == "" {
			return nil, 0, errors.New("scope id must not be blank")
		}
		switch row.State {
		case membership.StateSelected, membership.StateArchivedExcluded, membership.StateRuleExcluded, membership.StateNotListed:
		default:
			return nil, 0, fmt.Errorf("scope %s has unknown state %q", row.ScopeID, row.State)
		}
		if batch.Selector.Kind == membership.KindExplicit && row.State != membership.StateSelected {
			return nil, 0, fmt.Errorf("explicit selector row for scope %s has state %q; explicit selectors write only selected rows", row.ScopeID, row.State)
		}
		if row.GitHubRepoID < 0 {
			return nil, 0, fmt.Errorf("scope %s has negative GitHub repository id %d", row.ScopeID, row.GitHubRepoID)
		}
	}
	slices.SortFunc(rows, func(a, b membership.Row) int { return cmp.Compare(a.ScopeID, b.ScopeID) })
	unique := rows[:0]
	for _, row := range rows {
		if n := len(unique); n > 0 && unique[n-1].ScopeID == row.ScopeID {
			if unique[n-1] != row {
				return nil, 0, fmt.Errorf("scope %s appears twice with different observations", row.ScopeID)
			}
			continue
		}
		unique = append(unique, row)
	}
	return unique, int64(batch.LivenessWindow / time.Second), nil
}
