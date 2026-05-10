package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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

const listTerraformStateLocalCandidateFactsQuery = `
SELECT
    candidate.payload->>'repo_id' AS repo_id,
    candidate.payload->>'relative_path' AS relative_path,
    COALESCE(repository.payload->>'local_path', repository.source_uri, '') AS source_uri
FROM fact_records AS candidate
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidate.scope_id
 AND scope.active_generation_id = candidate.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = candidate.scope_id
 AND generation.generation_id = candidate.generation_id
JOIN fact_records AS repository
  ON repository.scope_id = candidate.scope_id
 AND repository.generation_id = candidate.generation_id
 AND repository.fact_kind = 'repository'
 AND repository.source_system = 'git'
WHERE candidate.fact_kind = 'terraform_state_candidate'
  AND candidate.source_system = 'git'
  AND generation.status = 'active'
  AND candidate.payload->>'repo_id' = ANY($1::text[])
ORDER BY repo_id ASC, relative_path ASC, candidate.observed_at ASC, candidate.fact_id ASC
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
    fact.payload->>'etag' AS etag,
    generation.generation_id
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
		var generationID string
		if err := rows.Scan(&locatorHash, &etag, &generationID); err != nil {
			return nil, fmt.Errorf("list terraform state prior snapshot metadata: %w", err)
		}
		state, ok := byHash[locatorHash]
		if !ok {
			continue
		}
		if _, seen := metadata[state]; seen {
			continue
		}
		metadata[state] = terraformstate.PriorSnapshotMetadata{ETag: etag, GenerationID: generationID}
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
	localCandidates, err := r.localStateCandidates(ctx, query, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, localCandidates...)
	return candidates, nil
}

func (r TerraformStateBackendFactReader) localStateCandidates(
	ctx context.Context,
	query terraformstate.DiscoveryQuery,
	seen map[string]struct{},
) ([]terraformstate.DiscoveryCandidate, error) {
	if !query.IncludeLocalStateCandidates || len(query.ApprovedLocalCandidates) == 0 {
		return nil, nil
	}
	approved := approvedLocalStateCandidateSet(query.ApprovedLocalCandidates)
	if len(approved) == 0 {
		return nil, nil
	}
	rows, err := r.DB.QueryContext(ctx, listTerraformStateLocalCandidateFactsQuery, cleanFactKinds(query.RepoIDs))
	if err != nil {
		return nil, fmt.Errorf("list terraform state local candidate facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []terraformstate.DiscoveryCandidate
	for rows.Next() {
		candidate, ok, scanErr := scanTerraformStateLocalCandidate(rows, approved)
		if scanErr != nil {
			return nil, fmt.Errorf("list terraform state local candidate facts: %w", scanErr)
		}
		if !ok {
			continue
		}
		key := candidate.State.Locator + "\x00" + candidate.State.VersionID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state local candidate facts: %w", err)
	}
	return candidates, nil
}

func approvedLocalStateCandidateSet(refs []terraformstate.LocalStateCandidateRef) map[localCandidateKey]struct{} {
	approved := map[localCandidateKey]struct{}{}
	for _, ref := range refs {
		key := localCandidateKey{
			repoID:       strings.TrimSpace(ref.RepoID),
			relativePath: cleanFactRelativePath(ref.RelativePath),
		}
		if key.repoID == "" || !isSafeFactRelativePath(key.relativePath) {
			continue
		}
		approved[key] = struct{}{}
	}
	return approved
}

type localCandidateKey struct {
	repoID       string
	relativePath string
}

func scanTerraformStateLocalCandidate(
	rows Rows,
	approved map[localCandidateKey]struct{},
) (terraformstate.DiscoveryCandidate, bool, error) {
	var repoID string
	var relativePath string
	var repoRoot string
	if err := rows.Scan(&repoID, &relativePath, &repoRoot); err != nil {
		return terraformstate.DiscoveryCandidate{}, false, err
	}
	key := localCandidateKey{
		repoID:       strings.TrimSpace(repoID),
		relativePath: cleanFactRelativePath(relativePath),
	}
	if !isSafeFactRelativePath(key.relativePath) {
		return terraformstate.DiscoveryCandidate{}, false, nil
	}
	if _, ok := approved[key]; !ok {
		return terraformstate.DiscoveryCandidate{}, false, nil
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return terraformstate.DiscoveryCandidate{}, false, nil
	}
	absolutePath := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(key.relativePath)))
	return terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendLocal,
			Locator:     absolutePath,
		},
		Source:       terraformstate.DiscoveryCandidateSourceGitLocalFile,
		RepoID:       key.repoID,
		RelativePath: key.relativePath,
		StateInVCS:   true,
	}, true, nil
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

func cleanFactRelativePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = strings.TrimPrefix(path, "./")
	return strings.Trim(path, "/")
}

func isSafeFactRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}
