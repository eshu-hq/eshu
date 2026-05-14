package query

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const repositoryCatalogIDExpr = "coalesce(payload->>'repo_id', payload->>'id', scope_id)"

// ListRepositories returns repository catalog entries from the relational data plane.
func (cr *ContentReader) ListRepositories(ctx context.Context) ([]RepositoryCatalogEntry, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT `+repositoryCatalogIDExpr+` AS id,
		       coalesce(payload->>'name', payload->>'repo_name', payload->>'repo_slug', scope_id) AS name,
		       coalesce(payload->>'path', '') AS path,
		       coalesce(payload->>'local_path', payload->>'path', '') AS local_path,
		       coalesce(payload->>'remote_url', '') AS remote_url,
		       coalesce(payload->>'repo_slug', '') AS repo_slug,
		       CASE WHEN coalesce(payload->>'remote_url', '') <> '' THEN true ELSE false END AS has_remote
		FROM ingestion_scopes
		WHERE scope_kind = 'repository'
		ORDER BY name, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	repositories := make([]RepositoryCatalogEntry, 0)
	for rows.Next() {
		var repo RepositoryCatalogEntry
		if err := rows.Scan(
			&repo.ID,
			&repo.Name,
			&repo.Path,
			&repo.LocalPath,
			&repo.RemoteURL,
			&repo.RepoSlug,
			&repo.HasRemote,
		); err != nil {
			return nil, fmt.Errorf("scan repository catalog row: %w", err)
		}
		repositories = append(repositories, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository catalog rows: %w", err)
	}
	return repositories, nil
}

// ListWorkloadIdentities returns workload handles from reducer read-model facts.
func (cr *ContentReader) ListWorkloadIdentities(
	ctx context.Context,
	limit int,
) ([]CatalogWorkloadIdentityEntry, bool, error) {
	if cr == nil || cr.db == nil {
		return nil, false, nil
	}
	if limit <= 0 {
		return nil, false, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT DISTINCT entity_key AS workload_name,
		       coalesce(scope.payload->>'repo_id', scope.payload->>'id', scope.scope_id) AS repo_id,
		       coalesce(scope.payload->>'name', scope.payload->>'repo_name', scope.payload->>'repo_slug', scope.scope_id) AS repo_name
		FROM fact_records facts
		JOIN ingestion_scopes scope ON scope.scope_id = facts.scope_id
		CROSS JOIN jsonb_array_elements_text(coalesce(facts.payload->'entity_keys', '[]'::jsonb)) AS entity_key
		WHERE scope.scope_kind = 'repository'
		  AND facts.fact_kind = 'reducer_workload_identity'
		  AND NOT facts.is_tombstone
		ORDER BY workload_name, repo_name
		LIMIT $1
	`, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list workload identities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	identities := make([]CatalogWorkloadIdentityEntry, 0)
	for rows.Next() {
		var identity CatalogWorkloadIdentityEntry
		if err := rows.Scan(&identity.Name, &identity.RepoID, &identity.RepoName); err != nil {
			return nil, false, fmt.Errorf("scan workload identity catalog row: %w", err)
		}
		identity.Name = strings.TrimPrefix(identity.Name, "workload:")
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate workload identity catalog rows: %w", err)
	}
	truncated := len(identities) > limit
	if truncated {
		identities = identities[:limit]
	}
	return identities, truncated, nil
}

// ResolveRepository resolves one repository selector from the relational catalog.
func (cr *ContentReader) ResolveRepository(ctx context.Context, selector string) (*RepositoryCatalogEntry, error) {
	matches, err := cr.MatchRepositories(ctx, selector)
	if err != nil {
		return nil, err
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		repo := matches[0]
		return &repo, nil
	default:
		ids := make([]string, 0, len(matches))
		for _, repo := range matches {
			if repo.ID != "" {
				ids = append(ids, repo.ID)
			}
		}
		slices.Sort(ids)
		return nil, fmt.Errorf("repository selector %q matched multiple repositories: %s", selector, joinRepositoryIDs(ids))
	}
}

// MatchRepositories returns exact repository catalog matches for one selector.
func (cr *ContentReader) MatchRepositories(ctx context.Context, selector string) ([]RepositoryCatalogEntry, error) {
	if cr == nil || cr.db == nil {
		return nil, nil
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT `+repositoryCatalogIDExpr+` AS id,
		       coalesce(payload->>'name', payload->>'repo_name', payload->>'repo_slug', scope_id) AS name,
		       coalesce(payload->>'path', '') AS path,
		       coalesce(payload->>'local_path', payload->>'path', '') AS local_path,
		       coalesce(payload->>'remote_url', '') AS remote_url,
		       coalesce(payload->>'repo_slug', '') AS repo_slug,
		       CASE WHEN coalesce(payload->>'remote_url', '') <> '' THEN true ELSE false END AS has_remote
		FROM ingestion_scopes
		WHERE scope_kind = 'repository'
		  AND (
			`+repositoryCatalogIDExpr+` = $1 OR
			scope_id = $1 OR
			payload->>'name' = $1 OR
			payload->>'repo_name' = $1 OR
			payload->>'repo_slug' = $1 OR
			payload->>'path' = $1 OR
			payload->>'local_path' = $1
		  )
		ORDER BY scope_id
	`, selector)
	if err != nil {
		return nil, fmt.Errorf("match repository catalog rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	repositories := make([]RepositoryCatalogEntry, 0, 1)
	for rows.Next() {
		var repo RepositoryCatalogEntry
		if err := rows.Scan(
			&repo.ID,
			&repo.Name,
			&repo.Path,
			&repo.LocalPath,
			&repo.RemoteURL,
			&repo.RepoSlug,
			&repo.HasRemote,
		); err != nil {
			return nil, fmt.Errorf("scan repository catalog match: %w", err)
		}
		repositories = append(repositories, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository catalog matches: %w", err)
	}
	return repositories, nil
}

func joinRepositoryIDs(ids []string) string {
	return strings.Join(ids, ", ")
}
