package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

const listTerraformBackendFactsQuery = `
SELECT
    fact.payload->>'repo_id' AS repo_id,
    fact.payload->'parsed_file_data'->'terraform_backends' AS terraform_backends
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND generation.status = 'active'
  AND fact.payload->>'repo_id' = ANY($1::text[])
  AND jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
ORDER BY repo_id ASC, fact.observed_at ASC, fact.fact_id ASC
`

const terraformStateGitReadinessQuery = `
SELECT EXISTS (
    SELECT 1
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.generation_id = fact.generation_id
     AND generation.scope_id = fact.scope_id
    WHERE fact.fact_kind = 'repository'
      AND fact.source_system = 'git'
      AND generation.status = 'active'
      AND COALESCE(
          fact.payload->>'repo_id',
          fact.payload->>'graph_id',
          fact.payload->>'name',
          ''
      ) = $1
    LIMIT 1
)
`

const listTerraformStatePriorSnapshotMetadataQuery = `
SELECT
    fact.payload->>'locator_hash' AS locator_hash,
    fact.payload->>'etag' AS etag
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'terraform_state_snapshot'
  AND generation.status = 'active'
  AND fact.payload->>'locator_hash' = ANY($1::text[])
  AND COALESCE(fact.payload->>'etag', '') <> ''
ORDER BY fact.observed_at DESC, fact.fact_id DESC
`

// TerraformStateBackendFactReader reads Git-observed Terraform backend facts
// from active repository generations.
type TerraformStateBackendFactReader struct {
	DB Queryer
}

// TerraformStatePriorSnapshotReader reads durable Terraform-state freshness
// metadata from active snapshot facts.
type TerraformStatePriorSnapshotReader struct {
	DB Queryer
}

// TerraformStateGitReadinessChecker reports whether Git evidence for a repo
// has an active committed generation.
type TerraformStateGitReadinessChecker struct {
	DB Queryer
}

// GitGenerationCommitted implements terraformstate.GitReadinessChecker.
func (c TerraformStateGitReadinessChecker) GitGenerationCommitted(
	ctx context.Context,
	repoID string,
) (bool, error) {
	if c.DB == nil {
		return false, fmt.Errorf("terraform state git readiness database is required")
	}
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return false, nil
	}

	rows, err := c.DB.QueryContext(ctx, terraformStateGitReadinessQuery, repoID)
	if err != nil {
		return false, fmt.Errorf("check terraform state git readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ready bool
	if rows.Next() {
		if err := rows.Scan(&ready); err != nil {
			return false, fmt.Errorf("check terraform state git readiness: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check terraform state git readiness: %w", err)
	}
	return ready, nil
}

// TerraformStatePriorSnapshotMetadata implements terraformstate.PriorSnapshotMetadataReader.
func (r TerraformStatePriorSnapshotReader) TerraformStatePriorSnapshotMetadata(
	ctx context.Context,
	states []terraformstate.StateKey,
) (map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata, error) {
	if r.DB == nil {
		return nil, fmt.Errorf("terraform state prior snapshot database is required")
	}
	byHash := map[string]terraformstate.StateKey{}
	hashes := make([]string, 0, len(states))
	for _, state := range states {
		if state.BackendKind != terraformstate.BackendS3 {
			continue
		}
		hash := terraformstate.LocatorHash(state)
		if _, ok := byHash[hash]; ok {
			continue
		}
		byHash[hash] = state
		hashes = append(hashes, hash)
	}
	if len(hashes) == 0 {
		return map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata{}, nil
	}

	rows, err := r.DB.QueryContext(ctx, listTerraformStatePriorSnapshotMetadataQuery, hashes)
	if err != nil {
		return nil, fmt.Errorf("list terraform state prior snapshot metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	metadata := map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata{}
	for rows.Next() {
		var locatorHash string
		var etag string
		if err := rows.Scan(&locatorHash, &etag); err != nil {
			return nil, fmt.Errorf("list terraform state prior snapshot metadata: %w", err)
		}
		state, ok := byHash[locatorHash]
		if !ok {
			continue
		}
		if _, seen := metadata[state]; seen {
			continue
		}
		metadata[state] = terraformstate.PriorSnapshotMetadata{ETag: etag}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state prior snapshot metadata: %w", err)
	}
	return metadata, nil
}

// TerraformStateCandidates implements terraformstate.BackendFactReader.
func (r TerraformStateBackendFactReader) TerraformStateCandidates(
	ctx context.Context,
	query terraformstate.DiscoveryQuery,
) ([]terraformstate.DiscoveryCandidate, error) {
	if r.DB == nil {
		return nil, fmt.Errorf("terraform state backend facts database is required")
	}
	repoIDs := cleanFactKinds(query.RepoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := r.DB.QueryContext(ctx, listTerraformBackendFactsQuery, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("list terraform state backend facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []terraformstate.DiscoveryCandidate
	seen := map[string]struct{}{}
	for rows.Next() {
		rowCandidates, scanErr := scanTerraformBackendFactCandidates(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list terraform state backend facts: %w", scanErr)
		}
		for _, candidate := range rowCandidates {
			key := candidate.State.Locator + "\x00" + candidate.State.VersionID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state backend facts: %w", err)
	}
	return candidates, nil
}

func scanTerraformBackendFactCandidates(rows Rows) ([]terraformstate.DiscoveryCandidate, error) {
	var repoID string
	var rawBackends []byte
	if err := rows.Scan(&repoID, &rawBackends); err != nil {
		return nil, err
	}

	var backends []map[string]any
	if err := json.Unmarshal(rawBackends, &backends); err != nil {
		return nil, fmt.Errorf("decode terraform_backends for repo %q: %w", repoID, err)
	}

	candidates := make([]terraformstate.DiscoveryCandidate, 0, len(backends))
	for _, backend := range backends {
		candidate, ok := terraformBackendCandidate(repoID, backend)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}

func terraformBackendCandidate(
	repoID string,
	backend map[string]any,
) (terraformstate.DiscoveryCandidate, bool) {
	if strings.TrimSpace(stringValue(backend, "backend_kind", "name")) != string(terraformstate.BackendS3) {
		return terraformstate.DiscoveryCandidate{}, false
	}
	if strings.TrimSpace(stringValue(backend, "workspace_key_prefix")) != "" {
		return terraformstate.DiscoveryCandidate{}, false
	}

	bucket := strings.TrimSpace(stringValue(backend, "bucket"))
	key := strings.TrimSpace(stringValue(backend, "key"))
	region := strings.TrimSpace(stringValue(backend, "region"))
	dynamoDBTable := exactOptionalBackendAttribute(backend, "dynamodb_table")
	if !isExactBackendAttribute(backend, "bucket", bucket) ||
		!isExactBackendAttribute(backend, "key", key) ||
		!isExactBackendAttribute(backend, "region", region) {
		return terraformstate.DiscoveryCandidate{}, false
	}
	if strings.HasSuffix(key, "/") {
		return terraformstate.DiscoveryCandidate{}, false
	}

	return terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://" + bucket + "/" + key,
		},
		Source:        terraformstate.DiscoveryCandidateSourceGraph,
		RepoID:        strings.TrimSpace(repoID),
		Region:        region,
		DynamoDBTable: dynamoDBTable,
	}, true
}

func exactOptionalBackendAttribute(values map[string]any, name string) string {
	value := strings.TrimSpace(stringValue(values, name))
	if value == "" || !isExactBackendAttribute(values, name, value) {
		return ""
	}
	return value
}

func isExactBackendAttribute(values map[string]any, name string, value string) bool {
	literalKey := name + "_is_literal"
	switch literal := values[literalKey].(type) {
	case bool:
		return literal && isExactBackendValue(value)
	case string:
		return strings.EqualFold(strings.TrimSpace(literal), "true") && isExactBackendValue(value)
	default:
		return false
	}
}

func isExactBackendValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "${") || strings.Contains(value, "(") {
		return false
	}
	for _, dynamicPrefix := range []string{"var.", "local.", "path.", "terraform."} {
		if strings.HasPrefix(value, dynamicPrefix) {
			return false
		}
	}
	return true
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}
