package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

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
	filters := cleanTerraformStateBackendFilters(query.BackendFilters)
	if len(repoIDs) == 0 && len(filters) == 0 {
		return nil, nil
	}
	if len(repoIDs) == 0 {
		return r.filteredTerraformStateCandidates(ctx, filters, nil)
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
		candidates = appendTerraformStateCandidatesWithFilterEnrichment(candidates, seen, rowCandidates, filters)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terraform state backend facts: %w", err)
	}
	terragruntCandidates, err := r.terragruntRemoteStateCandidates(ctx, repoIDs, seen, filters)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, terragruntCandidates...)
	localCandidates, err := r.localStateCandidates(ctx, query, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, localCandidates...)
	filteredCandidates, err := r.filteredTerraformStateCandidates(ctx, filters, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, filteredCandidates...)
	return candidates, nil
}

// terragruntRemoteStateCandidates queries the parsed Terragrunt remote_state
// rows for the requested repos and translates each row into a
// DiscoveryCandidate with the underlying backend kind. Rows that fail the
// resolver's literal-attribute checks are silently skipped; callers do not
// learn about ambiguous Terragrunt configs from this path because the same
// rows will surface as warning facts elsewhere in the parser pipeline.
func (r TerraformStateBackendFactReader) terragruntRemoteStateCandidates(
	ctx context.Context,
	repoIDs []string,
	seen map[string]struct{},
	filters []terraformstate.DiscoveryBackendFilter,
) ([]terraformstate.DiscoveryCandidate, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	rows, err := r.DB.QueryContext(ctx, listTerragruntRemoteStateFactsQuery, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("list terragrunt remote_state facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var candidates []terraformstate.DiscoveryCandidate
	for rows.Next() {
		rowCandidates, scanErr := scanTerragruntRemoteStateCandidates(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list terragrunt remote_state facts: %w", scanErr)
		}
		candidates = appendTerraformStateCandidatesWithFilterEnrichment(candidates, seen, rowCandidates, filters)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list terragrunt remote_state facts: %w", err)
	}
	return candidates, nil
}

// scanTerragruntRemoteStateCandidates decodes one row of remote_state payloads
// and converts each entry through the typed resolver. The query joins the
// repository fact's local_path so the resolver can compute repo-relative
// paths for local-backend candidates; rows from a generation that lacks a
// repository fact arrive with an empty repoLocalPath, which the resolver
// rejects for local backends but tolerates for S3 backends.
func scanTerragruntRemoteStateCandidates(rows Rows) ([]terraformstate.DiscoveryCandidate, error) {
	var repoID string
	var repoLocalPath string
	var rawRemoteStates []byte
	if err := rows.Scan(&repoID, &repoLocalPath, &rawRemoteStates); err != nil {
		return nil, err
	}

	var entries []map[string]any
	if err := json.Unmarshal(rawRemoteStates, &entries); err != nil {
		return nil, fmt.Errorf("decode terragrunt_remote_states for repo %q: %w", repoID, err)
	}

	candidates := make([]terraformstate.DiscoveryCandidate, 0, len(entries))
	for _, entry := range entries {
		candidate, ok := terraformstate.TerragruntRemoteStateCandidate(repoID, repoLocalPath, entry)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
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
	repoIDs := cleanFactKinds(query.RepoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}
	approved := approvedLocalStateCandidateSet(query.ApprovedLocalCandidates)
	if len(approved) == 0 {
		return nil, nil
	}
	rows, err := r.DB.QueryContext(ctx, listTerraformStateLocalCandidateFactsQuery, repoIDs)
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

func approvedLocalStateCandidateSet(refs []terraformstate.LocalStateCandidateRef) map[localCandidateKey]string {
	approved := map[localCandidateKey]string{}
	for _, ref := range refs {
		key := localCandidateKey{
			repoID:       strings.TrimSpace(ref.RepoID),
			relativePath: cleanFactRelativePath(ref.RelativePath),
		}
		if key.repoID == "" || !isSafeFactRelativePath(key.relativePath) {
			continue
		}
		approved[key] = strings.TrimSpace(ref.TargetScopeID)
	}
	return approved
}

type localCandidateKey struct {
	repoID       string
	relativePath string
}

func scanTerraformStateLocalCandidate(
	rows Rows,
	approved map[localCandidateKey]string,
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
	targetScopeID, ok := approved[key]
	if !ok {
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
		Source:        terraformstate.DiscoveryCandidateSourceGitLocalFile,
		TargetScopeID: strings.TrimSpace(targetScopeID),
		RepoID:        key.repoID,
		RelativePath:  key.relativePath,
		StateInVCS:    true,
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
