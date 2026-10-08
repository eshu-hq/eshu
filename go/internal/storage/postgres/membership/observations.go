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

// schemaSQL is the repository_selection_observations DDL that migration 163
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

// observationsQuery reads every stored observation of one selector.
const observationsQuery = `
SELECT scope_id, state, github_repo_id, last_listed_at, state_since,
       state_cycle_count, evaluated_at, liveness_window_seconds
FROM repository_selection_observations
WHERE selector_id = $1
`

// upsertObservationsQuery writes one evaluation in one statement. Rows are
// inserted in scope_id order so two replicas writing the same selector lock
// rows in the same order and cannot deadlock. A stored row only advances when
// the batch is newer, so a replay or a lagging replica changes nothing. In the
// DO UPDATE arm o is the stored row: a repeated state keeps state_since and
// adds a cycle, a changed state restarts both at the batch time. This must
// match membership's project function; the live store test pins it.
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
    liveness_window_seconds = EXCLUDED.liveness_window_seconds
WHERE o.evaluated_at < EXCLUDED.evaluated_at
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

// KnownScopes returns the git repository scopes whose repo slug org equals
// owner, compared case-insensitively, and, when host is non-empty, whose
// stored remote_url host equals host (a scope without a remote_url then never
// matches). owner and host are trimmed and lowercased; a blank owner is
// rejected before any query.
func (s ObservationStore) KnownScopes(ctx context.Context, owner, host string) ([]membership.KnownScope, error) {
	if s.database == nil {
		return nil, errors.New("repository selection store database is required")
	}
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" {
		return nil, errors.New("read known repository scopes: owner must not be blank")
	}
	host = strings.ToLower(strings.TrimSpace(host))
	rows, err := s.database.QueryContext(ctx, knownScopesQuery, owner, host)
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
			observation   membership.Observation
			state         string
			githubRepoID  sql.NullInt64
			lastListedAt  sql.NullTime
			windowSeconds int
		)
		if err := rows.Scan(
			&observation.ScopeID, &state, &githubRepoID, &lastListedAt, &observation.StateSince,
			&observation.StateCycleCount, &observation.EvaluatedAt, &windowSeconds,
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

// validateBatch checks the batch header and returns its rows sorted by scope
// ID with exact duplicates removed, plus the liveness window in whole seconds.
func validateBatch(batch membership.Batch) ([]membership.Row, int64, error) {
	switch {
	case strings.TrimSpace(batch.Selector.ID) == "":
		return nil, 0, errors.New("selector id must not be blank")
	case batch.Selector.Kind != membership.KindGitHubOrg && batch.Selector.Kind != membership.KindExplicit:
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
