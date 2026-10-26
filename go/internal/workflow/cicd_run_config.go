// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxCICDRunLimit      = 100
	maxCICDJobLimit      = 500
	maxCICDArtifactLimit = 500
	// maxCICDDeploymentLimit bounds max_deployments; GitHub's deployments list
	// pages at 100, matching the other per-page bounds in this family.
	maxCICDDeploymentLimit = 100
)

type cicdRunCollectorConfiguration struct {
	Targets []cicdRunTargetConfiguration `json:"targets"`
}

type cicdRunTargetConfiguration struct {
	Provider                   string   `json:"provider"`
	ScopeID                    string   `json:"scope_id"`
	Repository                 string   `json:"repository"`
	TokenEnv                   string   `json:"token_env"`
	AuthMode                   string   `json:"auth_mode"`
	HostTokenFile              string   `json:"host_token_file"`
	GitHubAppIDEnv             string   `json:"github_app_id_env"`
	GitHubAppInstallationIDEnv string   `json:"github_app_installation_id_env"`
	GitHubAppPrivateKeyEnv     string   `json:"github_app_private_key_env"`
	AllowedRepositories        []string `json:"allowed_repositories"`
	APIBaseURL                 string   `json:"api_base_url"`
	MaxRuns                    int      `json:"max_runs"`
	MaxJobs                    int      `json:"max_jobs"`
	MaxArtifacts               int      `json:"max_artifacts"`
	MaxDeployments             int      `json:"max_deployments"`
	SourceURI                  string   `json:"source_uri"`
}

// ValidateCICDRunCollectorConfiguration checks bounded provider CI/CD run
// targets without resolving credentials or contacting a provider.
func ValidateCICDRunCollectorConfiguration(raw string) error {
	var decoded cicdRunCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode ci/cd run collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("ci/cd run collector configuration requires targets")
	}
	seen := make(map[string]struct{}, len(decoded.Targets))
	for i, target := range decoded.Targets {
		if err := validateCICDRunTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate ci/cd run target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

// cicdRunProviderGitHubActions and cicdRunProviderGitLabCI mirror
// go/internal/collector/cicdrun.ProviderGitHubActions/ProviderGitLabCI as
// plain strings: this package cannot import the collector package (it would
// be a dependency-direction violation — internal/workflow is imported BY
// internal/collector, not the reverse), so the provider identifier is
// duplicated here rather than shared. cicd_run_config_test.go's
// TestCICDRunCollectorInstanceValidationUsesCICDRunConfig and
// go/cmd/collector-cicd-run's own provider switch
// (parseCICDRunRuntimeConfiguration) are what keep the two string sets in
// sync; a typo in either place fails loudly (an unrecognized provider is
// rejected, never silently accepted).
const (
	cicdRunProviderGitHubActions = "github_actions"
	cicdRunProviderGitLabCI      = "gitlab_ci"
)

// cicdRunAuthModeTokenEnv and cicdRunAuthModeHostMinted mirror
// go/internal/collector/cicdrun/ghactionsruntime.AuthModeTokenEnv/AuthModeHostMinted
// as plain strings, for the same dependency-direction reason as the provider
// constants above: the lane in go/cmd/collector-cicd-run pins the sync
// behaviorally (its unknown-mode and never-mix tests fail against either
// side drifting), so a typo here fails loudly instead of silently changing
// which credential shape a target selects.
const (
	cicdRunAuthModeTokenEnv   = "token_env"
	cicdRunAuthModeHostMinted = "host_minted"
)

func validateCICDRunTargetConfiguration(target cicdRunTargetConfiguration) error {
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	provider := strings.TrimSpace(target.Provider)
	switch provider {
	case cicdRunProviderGitHubActions, cicdRunProviderGitLabCI:
	case "":
		return fmt.Errorf("provider is required")
	default:
		return fmt.Errorf("unsupported ci/cd run provider %q", provider)
	}
	// GitHub Actions repositories are always exactly "owner/repo" (two
	// segments); GitLab CI projects may nest under any number of subgroups
	// ("group/subgroup/project", three or more segments) — see
	// go/internal/collector/cicdrun/gitlabciruntime's normalizeProjectPath,
	// which this validation must accept the same shapes as.
	var repository string
	if provider == cicdRunProviderGitLabCI {
		repository = normalizeCICDProjectPath(target.Repository)
	} else {
		repository = normalizeCICDRepository(target.Repository)
	}
	if repository == "" {
		if provider == cicdRunProviderGitLabCI {
			return fmt.Errorf("repository must be namespace/project")
		}
		return fmt.Errorf("repository must be owner/name")
	}
	if err := validateCICDRunAuth(target, provider); err != nil {
		return err
	}
	if len(cleanCICDRepositories(target.AllowedRepositories)) == 0 {
		return fmt.Errorf("allowed_repositories is required")
	}
	if !cicdRepositoryAllowed(repository, target.AllowedRepositories, provider) {
		return fmt.Errorf("repository must be listed in allowed_repositories")
	}
	// An omitted/zero max_runs is valid here: the runtime collector resolves
	// it to its own default (10, see defaultMaxRuns in
	// go/internal/collector/cicdrun/ghactionsruntime/source.go and
	// go/internal/collector/cicdrun/gitlabciruntime/source.go) rather than
	// requiring every target config to spell out a limit. Only an explicit
	// out-of-range value (negative, or above the hard cap) is rejected.
	if target.MaxRuns < 0 || target.MaxRuns > maxCICDRunLimit {
		return fmt.Errorf("max_runs must be between 0 and %d (0 uses the default of 10)", maxCICDRunLimit)
	}
	if target.MaxJobs <= 0 || target.MaxJobs > maxCICDJobLimit {
		return fmt.Errorf("max_jobs must be between 1 and %d", maxCICDJobLimit)
	}
	// max_artifacts is a GitHub-Actions-only bound: GitLab's Jobs API reports
	// job artifacts inline on the job with no separate paginated endpoint to
	// bound (see gitlabciruntime's README.md), so it is not required — and
	// not read — for a gitlab_ci target.
	if provider == cicdRunProviderGitHubActions {
		if target.MaxArtifacts <= 0 || target.MaxArtifacts > maxCICDArtifactLimit {
			return fmt.Errorf("max_artifacts must be between 1 and %d", maxCICDArtifactLimit)
		}
		// max_deployments bounds the GitHub Deployments window (#5425). Unlike
		// max_artifacts it is optional: 0 takes the collector default, so an
		// existing configuration that predates deploy-time events keeps
		// working unchanged.
		if target.MaxDeployments < 0 || target.MaxDeployments > maxCICDDeploymentLimit {
			return fmt.Errorf("max_deployments must be between 0 and %d (0 uses the default)", maxCICDDeploymentLimit)
		}
	}
	if err := validateCICDURL("api_base_url", target.APIBaseURL, true); err != nil {
		return err
	}
	return validateCICDURL("source_uri", target.SourceURI, false)
}

// validateCICDRunAuth keeps GitHub's three credential shapes exclusive. GitLab
// retains its token_env requirement and ignores GitHub-only auth fields.
func validateCICDRunAuth(target cicdRunTargetConfiguration, provider string) error {
	if provider == cicdRunProviderGitLabCI {
		if strings.TrimSpace(target.TokenEnv) == "" {
			return fmt.Errorf("token_env is required")
		}
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(target.AuthMode))
	if mode == "" {
		mode = cicdRunAuthModeTokenEnv
	}
	hostFile := strings.TrimSpace(target.HostTokenFile)
	appRefs := strings.TrimSpace(target.GitHubAppIDEnv) != "" ||
		strings.TrimSpace(target.GitHubAppInstallationIDEnv) != "" ||
		strings.TrimSpace(target.GitHubAppPrivateKeyEnv) != ""
	switch mode {
	case cicdRunAuthModeTokenEnv:
		if strings.TrimSpace(target.TokenEnv) == "" {
			return fmt.Errorf("token_env is required")
		}
		if hostFile != "" {
			return fmt.Errorf("host_token_file must be empty when auth_mode is %q", mode)
		}
		if appRefs {
			return fmt.Errorf("github_app env references must be empty when auth_mode is %q", mode)
		}
	case cicdRunAuthModeHostMinted:
		if hostFile == "" {
			return fmt.Errorf("host_token_file is required")
		}
		if strings.TrimSpace(target.TokenEnv) != "" {
			return fmt.Errorf("token_env must be empty when auth_mode is %q", mode)
		}
		if appRefs {
			return fmt.Errorf("github_app env references must be empty when auth_mode is %q", mode)
		}
	case "github_app":
		if strings.TrimSpace(target.TokenEnv) != "" {
			return fmt.Errorf("token_env must be empty when auth_mode is %q", mode)
		}
		if hostFile != "" {
			return fmt.Errorf("host_token_file must be empty when auth_mode is %q", mode)
		}
		if strings.TrimSpace(target.GitHubAppIDEnv) == "" {
			return fmt.Errorf("github_app_id_env is required when auth_mode is %q", mode)
		}
		if strings.TrimSpace(target.GitHubAppInstallationIDEnv) == "" {
			return fmt.Errorf("github_app_installation_id_env is required when auth_mode is %q", mode)
		}
		if strings.TrimSpace(target.GitHubAppPrivateKeyEnv) == "" {
			return fmt.Errorf("github_app_private_key_env is required when auth_mode is %q", mode)
		}
	default:
		return fmt.Errorf("unsupported ci/cd run auth_mode %q", target.AuthMode)
	}
	return nil
}

func validateCICDURL(field, raw string, requireHTTPS bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	return nil
}

// cicdRepositoryAllowed and cleanCICDRepositories are used for BOTH
// providers' allowed-list checks; the caller passes the already
// provider-normalized candidate ("repository" or "project path") and this
// re-normalizes each allowed-list entry the SAME way the candidate was
// normalized, matching go/internal/collector/cicdrun/ghactionsruntime and
// gitlabciruntime's own validateTarget normalization of AllowedRepositories/
// AllowedProjectPaths.
func cicdRepositoryAllowed(repository string, allowed []string, provider string) bool {
	for _, value := range allowed {
		if normalizeCICDRepositoryForProvider(value, provider) == repository {
			return true
		}
	}
	return false
}

func cleanCICDRepositories(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeCICDRepository(value); normalized != "" {
			out = append(out, normalized)
			continue
		}
		if normalized := normalizeCICDProjectPath(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeCICDRepositoryForProvider(repository, provider string) string {
	if provider == cicdRunProviderGitLabCI {
		return normalizeCICDProjectPath(repository)
	}
	return normalizeCICDRepository(repository)
}

// normalizeCICDRepository normalizes a GitHub Actions "owner/repo" locator:
// always exactly two "/"-segments.
func normalizeCICDRepository(repository string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(parts[0]))
	repo := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git"))
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// normalizeCICDProjectPath normalizes a GitLab CI "namespace/project" locator
// (or "group/subgroup/.../project" for a project nested under any number of
// subgroups) — at least two "/"-segments, unlike GitHub's exactly two. Mirrors
// go/internal/collector/cicdrun/gitlabciruntime's normalizeProjectPath.
func normalizeCICDProjectPath(projectPath string) string {
	trimmed := strings.ToLower(strings.Trim(strings.TrimSpace(projectPath), "/"))
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if trimmed == "" || !strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}
