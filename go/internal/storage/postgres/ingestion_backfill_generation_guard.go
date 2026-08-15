// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// shouldSkipUnchangedGeneration reports whether the scope's active generation
// already matches the incoming freshness hint, so a projection pass over an
// unchanged generation can be skipped.
func (s IngestionStore) shouldSkipUnchangedGeneration(
	ctx context.Context,
	scopeID string,
	freshnessHint string,
) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(freshnessHint) == "" {
		return false, nil
	}

	rows, err := s.db.QueryContext(ctx, activeGenerationFreshnessQuery, scopeID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, nil
	}

	var generationID string
	var activeFreshnessHint string
	if err := rows.Scan(&generationID, &activeFreshnessHint); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	return strings.TrimSpace(activeFreshnessHint) == strings.TrimSpace(freshnessHint), nil
}

// validateGenerationInput checks scope/generation preconditions before
// opening a transaction. Per-fact validation (scope_id, generation_id match)
// happens inside upsertStreamingFacts as facts arrive from the channel.
func validateGenerationInput(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) error {
	if err := generation.ValidateForScope(scopeValue); err != nil {
		return err
	}
	if generation.IsTerminal() {
		return fmt.Errorf("generation %q must not be terminal before projection", generation.GenerationID)
	}

	return nil
}

// repositoryGenerationIdentity binds a repository to its active scope and
// generation for the deferred relationship pass.
type repositoryGenerationIdentity struct {
	RepoID       string
	ScopeID      string
	GenerationID string
}

// loadActiveRepositoryGenerations returns the active (scope_id, generation_id)
// per repository. It filters to fact_kind = 'repository' and therefore excludes
// scopes with no repository fact (for example GCP cloud-relationship scopes); do
// not use it as the partition source for the corpus-wide deferred backfill.
func loadActiveRepositoryGenerations(
	ctx context.Context,
	queryer Queryer,
) (map[string]repositoryGenerationIdentity, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, activeRepositoryGenerationsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]repositoryGenerationIdentity)
	for rows.Next() {
		var identity repositoryGenerationIdentity
		if err := rows.Scan(&identity.RepoID, &identity.ScopeID, &identity.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(identity.RepoID) == "" {
			continue
		}
		result[identity.RepoID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// activeScopeGenerationQuery resolves ONE scope's currently active generation.
//
// It is the single-scope projection of latestGenerationCTE, which every
// corpus-wide deferred query joins against: that CTE picks, per scope,
// COALESCE(ingestion_scopes.active_generation_id, generation.generation_id)
// from the scope_generations row ordered by (ingested_at DESC,
// generation_id DESC). Restricting the same expression, join, and ordering to
// one scope_id and taking LIMIT 1 yields exactly the row DISTINCT ON (scope_id)
// would have chosen for that scope. TestFanInActiveGenerationMatchesCorpusLoader
// pins that correspondence against the shipped corpus-wide loader so the two
// cannot drift apart silently.
//
// The fan-in needs a per-scope lookup rather than loadActiveRepositoryGenerations
// because it runs once per partition. loadActiveRepositoryGenerations scans
// fact_records for every repository fact in the corpus; at ~910 partitions that
// would be ~910 corpus-wide scans per pass. This lookup is a LIMIT 1 read served
// by scope_generations_scope_latest_lookup_idx
// (scope_id, ingested_at DESC, generation_id DESC) from migration 002.
const activeScopeGenerationQuery = `
SELECT COALESCE(scope.active_generation_id, generation.generation_id) AS generation_id
FROM scope_generations AS generation
LEFT JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.scope_id = $1
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// loadActiveGenerationForScope returns the scope's active generation ID. The
// empty string with a nil error means the scope has no generation row at all,
// which the caller treats the same as an advanced generation: nothing to
// publish.
func loadActiveGenerationForScope(ctx context.Context, queryer Queryer, scopeID string) (string, error) {
	rows, err := queryer.QueryContext(ctx, activeScopeGenerationQuery, scopeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", rows.Err()
	}
	var generationID string
	if err := rows.Scan(&generationID); err != nil {
		return "", err
	}
	return generationID, rows.Err()
}
