package workflow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type terraformStateCollectorConfiguration struct {
	Discovery    terraformStateDiscoveryConfiguration `json:"discovery"`
	AWS          terraformStateAWSConfiguration       `json:"aws"`
	TargetScopes []terraformStateTargetScopeConfig    `json:"target_scopes"`
}

type terraformStateDiscoveryConfiguration struct {
	Graph                bool                                     `json:"graph"`
	Seeds                []terraformStateSeedConfig               `json:"seeds"`
	LocalRepos           []string                                 `json:"local_repos"`
	LocalStateCandidates terraformStateLocalCandidatePolicyConfig `json:"local_state_candidates"`
}

type terraformStateLocalCandidatePolicyConfig struct {
	Mode     string                               `json:"mode"`
	Approved []terraformStateLocalCandidateConfig `json:"approved"`
	Ignored  []terraformStateLocalCandidateConfig `json:"ignored"`
}

type terraformStateLocalCandidateConfig struct {
	RepoID        string `json:"repo_id"`
	Path          string `json:"path"`
	TargetScopeID string `json:"target_scope_id"`
}

type terraformStateSeedConfig struct {
	Kind          string `json:"kind"`
	TargetScopeID string `json:"target_scope_id"`
	Path          string `json:"path"`
	RepoID        string `json:"repo_id"`
	Bucket        string `json:"bucket"`
	Key           string `json:"key"`
	Region        string `json:"region"`
	VersionID     string `json:"version_id"`
}

type terraformStateAWSConfiguration struct {
	RoleARN string `json:"role_arn"`
}

type terraformStateTargetScopeConfig struct {
	TargetScopeID      string   `json:"target_scope_id"`
	Provider           string   `json:"provider"`
	DeploymentMode     string   `json:"deployment_mode"`
	CredentialMode     string   `json:"credential_mode"`
	RoleARN            string   `json:"role_arn"`
	ExternalID         string   `json:"external_id"`
	AllowedRegions     []string `json:"allowed_regions"`
	AllowedBackends    []string `json:"allowed_backends"`
	RedactionPolicyRef string   `json:"redaction_policy_ref"`
}

// ValidateTerraformStateCollectorConfiguration checks the shared
// Terraform-state collector configuration contract.
func ValidateTerraformStateCollectorConfiguration(raw string) error {
	var cfg terraformStateCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &cfg); err != nil {
		return fmt.Errorf("terraform_state configuration: %w", err)
	}

	if !cfg.Discovery.Graph && len(cfg.Discovery.Seeds) == 0 && len(cfg.Discovery.LocalRepos) == 0 {
		return fmt.Errorf("terraform_state configuration discovery must enable graph, seeds, or local_repos")
	}
	if cfg.Discovery.Graph && len(cfg.Discovery.LocalRepos) == 0 {
		return fmt.Errorf("terraform_state configuration discovery.graph requires at least one local_repos entry")
	}
	targetScopes, err := validateTerraformStateTargetScopes(cfg.TargetScopes)
	if err != nil {
		return err
	}
	if len(targetScopes) > 0 && strings.TrimSpace(cfg.AWS.RoleARN) != "" {
		return fmt.Errorf("terraform_state configuration must not mix aws.role_arn with target_scopes")
	}

	for index, repoID := range cfg.Discovery.LocalRepos {
		if strings.TrimSpace(repoID) == "" {
			return fmt.Errorf("terraform_state discovery local_repos %d must not be blank", index)
		}
	}
	if err := validateTerraformStateLocalCandidates(cfg.Discovery.LocalStateCandidates, targetScopes); err != nil {
		return err
	}

	usesS3 := false
	for index, seed := range cfg.Discovery.Seeds {
		kind := strings.ToLower(strings.TrimSpace(seed.Kind))
		if strings.TrimSpace(seed.VersionID) != seed.VersionID {
			return fmt.Errorf("terraform_state discovery seed %d version_id must not have surrounding whitespace", index)
		}
		switch kind {
		case "local":
			path := strings.TrimSpace(seed.Path)
			if path == "" {
				return fmt.Errorf("terraform_state discovery seed %d local path must not be blank", index)
			}
			if !filepath.IsAbs(path) {
				return fmt.Errorf("terraform_state discovery seed %d local path must be absolute", index)
			}
			if seed.VersionID != "" {
				return fmt.Errorf("terraform_state discovery seed %d local version_id is unsupported", index)
			}
			if err := validateSeedTargetScope(index, kind, seed, targetScopes); err != nil {
				return err
			}
		case "s3":
			usesS3 = true
			if strings.TrimSpace(seed.Bucket) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 bucket must not be blank", index)
			}
			if strings.TrimSpace(seed.Key) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 key must not be blank", index)
			}
			if strings.TrimSpace(seed.Region) == "" {
				return fmt.Errorf("terraform_state discovery seed %d s3 region must not be blank", index)
			}
			if err := validateSeedTargetScope(index, kind, seed, targetScopes); err != nil {
				return err
			}
		default:
			return fmt.Errorf("terraform_state discovery seed %d kind %q is unsupported", index, seed.Kind)
		}
	}

	if usesS3 && len(targetScopes) == 0 && strings.TrimSpace(cfg.AWS.RoleARN) == "" {
		return fmt.Errorf("terraform_state configuration aws.role_arn is required for s3 seeds")
	}

	return nil
}

func validateTerraformStateLocalCandidates(
	config terraformStateLocalCandidatePolicyConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) error {
	mode := strings.TrimSpace(config.Mode)
	switch mode {
	case "", "discover_only", "approved_candidates":
	default:
		return fmt.Errorf("terraform_state discovery local_state_candidates mode %q is unsupported", config.Mode)
	}
	if strings.TrimSpace(config.Mode) != config.Mode {
		return fmt.Errorf("terraform_state discovery local_state_candidates mode must not have surrounding whitespace")
	}
	if mode == "approved_candidates" && len(config.Approved) == 0 {
		return fmt.Errorf("terraform_state discovery local_state_candidates approved_candidates mode requires at least one approved path")
	}
	approvedScopes := map[terraformStateLocalCandidateKey]string{}
	for index, candidate := range config.Approved {
		if err := validateTerraformStateLocalCandidateRef("approved", index, candidate, targetScopes); err != nil {
			return err
		}
		key := terraformStateLocalCandidateRefKey(candidate)
		if previousScope, ok := approvedScopes[key]; ok && previousScope != strings.TrimSpace(candidate.TargetScopeID) {
			return fmt.Errorf("terraform_state discovery local_state_candidates approved %d duplicates repo_id/path with conflicting target_scope_id", index)
		}
		approvedScopes[key] = strings.TrimSpace(candidate.TargetScopeID)
	}
	for index, candidate := range config.Ignored {
		if err := validateTerraformStateLocalCandidateRef("ignored", index, candidate, targetScopes); err != nil {
			return err
		}
	}
	return nil
}

func validateTerraformStateLocalCandidateRef(
	field string,
	index int,
	candidate terraformStateLocalCandidateConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) error {
	repoID := strings.TrimSpace(candidate.RepoID)
	if repoID == "" {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d repo_id must not be blank", field, index)
	}
	if repoID != candidate.RepoID {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d repo_id must not have surrounding whitespace", field, index)
	}
	path := strings.TrimSpace(candidate.Path)
	if path == "" {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d path must not be blank", field, index)
	}
	if path != candidate.Path {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d path must not have surrounding whitespace", field, index)
	}
	if filepath.IsAbs(path) || hasParentPathSegment(path) {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d path must be repo-relative", field, index)
	}
	scopeID := strings.TrimSpace(candidate.TargetScopeID)
	if scopeID != candidate.TargetScopeID {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d target_scope_id must not have surrounding whitespace", field, index)
	}
	if scopeID == "" {
		return nil
	}
	scope, ok := targetScopes[scopeID]
	if !ok {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d target_scope_id %q is unknown", field, index, scopeID)
	}
	if len(scope.AllowedBackends) > 0 && !stringSliceContains(scope.AllowedBackends, "local") {
		return fmt.Errorf("terraform_state discovery local_state_candidates %s %d backend local is outside allowed_backends for target_scope_id %q", field, index, scopeID)
	}
	return nil
}

type terraformStateLocalCandidateKey struct {
	repoID string
	path   string
}

func terraformStateLocalCandidateRefKey(candidate terraformStateLocalCandidateConfig) terraformStateLocalCandidateKey {
	path := strings.TrimSpace(strings.ReplaceAll(candidate.Path, "\\", "/"))
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return terraformStateLocalCandidateKey{
		repoID: strings.TrimSpace(candidate.RepoID),
		path:   path,
	}
}

func hasParentPathSegment(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func validateTerraformStateTargetScopes(
	scopes []terraformStateTargetScopeConfig,
) (map[string]terraformStateTargetScopeConfig, error) {
	byID := make(map[string]terraformStateTargetScopeConfig, len(scopes))
	for index, scope := range scopes {
		id := strings.TrimSpace(scope.TargetScopeID)
		if id == "" {
			return nil, fmt.Errorf("terraform_state target_scopes %d target_scope_id must not be blank", index)
		}
		if id != scope.TargetScopeID {
			return nil, fmt.Errorf("terraform_state target_scopes %d target_scope_id must not have surrounding whitespace", index)
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("terraform_state target_scopes %d target_scope_id %q is duplicated", index, id)
		}
		provider := strings.TrimSpace(scope.Provider)
		if provider != scope.Provider || provider != strings.ToLower(provider) {
			return nil, fmt.Errorf("terraform_state target_scopes %d provider must be lowercase and trimmed", index)
		}
		if provider != "aws" {
			return nil, fmt.Errorf("terraform_state target_scopes %d provider %q is unsupported", index, scope.Provider)
		}
		if err := validateTargetScopeCredential(index, scope); err != nil {
			return nil, err
		}
		if err := validateTargetScopeList(index, "allowed_regions", scope.AllowedRegions, false); err != nil {
			return nil, err
		}
		if err := validateTargetScopeList(index, "allowed_backends", scope.AllowedBackends, true); err != nil {
			return nil, err
		}
		if strings.TrimSpace(scope.RedactionPolicyRef) != scope.RedactionPolicyRef {
			return nil, fmt.Errorf("terraform_state target_scopes %d redaction_policy_ref must not have surrounding whitespace", index)
		}
		byID[id] = scope
	}
	return byID, nil
}

func validateTargetScopeCredential(index int, scope terraformStateTargetScopeConfig) error {
	deploymentMode := strings.TrimSpace(scope.DeploymentMode)
	credentialMode := strings.TrimSpace(scope.CredentialMode)
	roleARN := strings.TrimSpace(scope.RoleARN)
	externalID := strings.TrimSpace(scope.ExternalID)
	if deploymentMode != scope.DeploymentMode || deploymentMode != strings.ToLower(deploymentMode) {
		return fmt.Errorf("terraform_state target_scopes %d deployment_mode must be lowercase and trimmed", index)
	}
	if credentialMode != scope.CredentialMode || credentialMode != strings.ToLower(credentialMode) {
		return fmt.Errorf("terraform_state target_scopes %d credential_mode must be lowercase and trimmed", index)
	}
	if roleARN != scope.RoleARN {
		return fmt.Errorf("terraform_state target_scopes %d role_arn must not have surrounding whitespace", index)
	}
	if externalID != scope.ExternalID {
		return fmt.Errorf("terraform_state target_scopes %d external_id must not have surrounding whitespace", index)
	}
	switch deploymentMode {
	case "central":
		if credentialMode != "central_assume_role" {
			return fmt.Errorf("terraform_state target_scopes %d central deployment requires central_assume_role", index)
		}
		if roleARN == "" {
			return fmt.Errorf("terraform_state target_scopes %d role_arn is required for central_assume_role", index)
		}
	case "account_local":
		if credentialMode != "local_workload_identity" {
			return fmt.Errorf("terraform_state target_scopes %d account_local deployment requires local_workload_identity", index)
		}
		if roleARN != "" {
			return fmt.Errorf("terraform_state target_scopes %d role_arn is unsupported for local_workload_identity", index)
		}
		if externalID != "" {
			return fmt.Errorf("terraform_state target_scopes %d external_id is unsupported for local_workload_identity", index)
		}
	default:
		return fmt.Errorf("terraform_state target_scopes %d deployment_mode %q is unsupported", index, scope.DeploymentMode)
	}
	return nil
}

func validateTargetScopeList(index int, field string, values []string, backendList bool) error {
	for valueIndex, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			return fmt.Errorf("terraform_state target_scopes %d %s %d must not be blank", index, field, valueIndex)
		}
		if value != raw {
			return fmt.Errorf("terraform_state target_scopes %d %s %d must be lowercase and trimmed", index, field, valueIndex)
		}
		if backendList && value != "local" && value != "s3" {
			return fmt.Errorf("terraform_state target_scopes %d %s %d backend %q is unsupported", index, field, valueIndex, raw)
		}
	}
	return nil
}

func validateSeedTargetScope(
	index int,
	backend string,
	seed terraformStateSeedConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) error {
	scope, err := targetScopeForSeed(index, seed, targetScopes)
	if err != nil {
		return err
	}
	if scope.TargetScopeID == "" {
		return nil
	}
	if len(scope.AllowedBackends) > 0 && !stringSliceContains(scope.AllowedBackends, backend) {
		return fmt.Errorf("terraform_state discovery seed %d backend %q is outside allowed_backends for target_scope_id %q", index, backend, scope.TargetScopeID)
	}
	if backend == "s3" && len(scope.AllowedRegions) > 0 && !stringSliceContains(scope.AllowedRegions, strings.TrimSpace(seed.Region)) {
		return fmt.Errorf("terraform_state discovery seed %d s3 region %q is outside allowed_regions for target_scope_id %q", index, strings.TrimSpace(seed.Region), scope.TargetScopeID)
	}
	return nil
}

func targetScopeForSeed(
	index int,
	seed terraformStateSeedConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) (terraformStateTargetScopeConfig, error) {
	seedScopeID := strings.TrimSpace(seed.TargetScopeID)
	if seedScopeID != seed.TargetScopeID {
		return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery seed %d target_scope_id must not have surrounding whitespace", index)
	}
	if len(targetScopes) == 0 {
		if seedScopeID != "" {
			return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery seed %d target_scope_id %q is unknown", index, seedScopeID)
		}
		return terraformStateTargetScopeConfig{}, nil
	}
	if seedScopeID != "" {
		scope, ok := targetScopes[seedScopeID]
		if !ok {
			return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery seed %d target_scope_id %q is unknown", index, seedScopeID)
		}
		return scope, nil
	}
	if len(targetScopes) == 1 {
		for _, scope := range targetScopes {
			return scope, nil
		}
	}
	return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery seed %d target_scope_id is required when multiple target_scopes are configured", index)
}

func stringSliceContains(values []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == want {
			return true
		}
	}
	return false
}
