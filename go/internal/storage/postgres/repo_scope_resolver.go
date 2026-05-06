package postgres

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const resolveRepoActiveGenerationsQuery = `
WITH latest_generations AS (
    SELECT
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            (
                SELECT generation_id
                FROM scope_generations AS candidate
                WHERE candidate.scope_id = generation.scope_id
                ORDER BY candidate.ingested_at DESC, candidate.generation_id DESC
                LIMIT 1
            )
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    GROUP BY generation.scope_id, scope.active_generation_id
)
SELECT DISTINCT ON (repo_id)
    repo_id,
    fact.scope_id,
    fact.generation_id
FROM (
    SELECT
        COALESCE(
            fact.payload->>'repo_id',
            fact.payload->>'graph_id',
            fact.payload->>'name',
            ''
        ) AS repo_id,
        fact.scope_id,
        fact.generation_id,
        fact.observed_at,
        fact.fact_id
    FROM fact_records AS fact
    JOIN latest_generations AS latest
      ON latest.scope_id = fact.scope_id
     AND latest.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'repository'
) AS fact
WHERE repo_id = ANY($1)
ORDER BY repo_id, observed_at DESC, fact_id DESC
`

// RepoScopeResolver resolves repository graph IDs to their active scope and
// generation identities. Implements reducer.DeploymentRepoScopeResolver.
type RepoScopeResolver struct {
	DB Queryer
}

// ResolveRepoActiveGenerations returns the active scope and generation for each
// requested repository graph ID. Repositories without an active generation are
// omitted from the result.
func (r RepoScopeResolver) ResolveRepoActiveGenerations(
	ctx context.Context,
	repoIDs []string,
) (map[string]reducer.RepoScopeIdentity, error) {
	if r.DB == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := r.DB.QueryContext(ctx, resolveRepoActiveGenerationsQuery, repoIDs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]reducer.RepoScopeIdentity, len(repoIDs))
	for rows.Next() {
		var repoID, scopeID, generationID string
		if err := rows.Scan(&repoID, &scopeID, &generationID); err != nil {
			return nil, err
		}
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		result[repoID] = reducer.RepoScopeIdentity{
			ScopeID:      scopeID,
			GenerationID: generationID,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
