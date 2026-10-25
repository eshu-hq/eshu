// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
)

// targetSet returns the repositories this target collects (#8838): its
// repository, or the allowlisted repositories of its organization. For a
// shard target (#9019) the allowlist is the shard's own repositories, so each
// shard records its own set and its unchanged-generation decision moves only
// when its own membership changes. It is
// recorded with each generation and moves the freshness digest, so a changed
// allowlist or a reused scope_id that names another repository always commits
// a new generation instead of being skipped as unchanged.
func (t TargetConfig) targetSet() scope.SecurityAlertTargetSet {
	if t.IsOrganizationScope() {
		return scope.NewSecurityAlertTargetSet(organizationFanoutRepositories(t))
	}
	return scope.NewSecurityAlertTargetSet([]string{t.Repository})
}

func validateTarget(target TargetConfig) (TargetConfig, error) {
	target.Provider = strings.TrimSpace(target.Provider)
	target.Scope = strings.TrimSpace(target.Scope)
	if target.Scope == "" {
		target.Scope = TargetScopeRepository
	}
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.Repository = normalizeRepository(target.Repository)
	target.Organization = normalizeOrganization(target.Organization)
	target.Token = strings.TrimSpace(target.Token)
	target.AuthMode = strings.ToLower(strings.TrimSpace(target.AuthMode))
	if target.AuthMode == "" {
		target.AuthMode = AuthModeTokenEnv
	}
	target.GitHubAppID = strings.TrimSpace(target.GitHubAppID)
	target.GitHubAppInstallationID = strings.TrimSpace(target.GitHubAppInstallationID)
	target.GitHubAppPrivateKey = strings.TrimSpace(target.GitHubAppPrivateKey)
	target.APIBaseURL = strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/")
	target.SourceURI = strings.TrimSpace(target.SourceURI)
	target.AllowedRepositories = cleanRepositories(target.AllowedRepositories)
	if target.Provider != ProviderGitHubDependabot {
		if target.Provider == "" {
			return TargetConfig{}, fmt.Errorf("provider is required")
		}
		return TargetConfig{}, fmt.Errorf("unsupported security alert provider %q", target.Provider)
	}
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if shard.HasMarker(target.ScopeID) {
		// Generated org shard scope ids use the marker (#9019).
		return TargetConfig{}, fmt.Errorf("scope_id must not contain the reserved %q marker", shard.Marker)
	}
	switch target.AuthMode {
	case AuthModeTokenEnv:
		if target.Token == "" {
			return TargetConfig{}, fmt.Errorf("token is required")
		}
		if target.GitHubAppID != "" || target.GitHubAppInstallationID != "" || target.GitHubAppPrivateKey != "" {
			return TargetConfig{}, fmt.Errorf("github_app credentials must be empty when auth_mode is %q", AuthModeTokenEnv)
		}
	case AuthModeGitHubApp:
		if target.Token != "" {
			return TargetConfig{}, fmt.Errorf("token must be empty when auth_mode is %q", AuthModeGitHubApp)
		}
		if target.GitHubAppID == "" {
			return TargetConfig{}, fmt.Errorf("github_app_id is required when auth_mode is %q", AuthModeGitHubApp)
		}
		if target.GitHubAppInstallationID == "" {
			return TargetConfig{}, fmt.Errorf("github_app_installation_id is required when auth_mode is %q", AuthModeGitHubApp)
		}
		if target.GitHubAppPrivateKey == "" {
			return TargetConfig{}, fmt.Errorf("github_app_private_key is required when auth_mode is %q", AuthModeGitHubApp)
		}
	default:
		return TargetConfig{}, fmt.Errorf("unsupported security alert auth_mode %q", target.AuthMode)
	}
	switch target.Scope {
	case TargetScopeRepository:
		if target.Organization != "" {
			return TargetConfig{}, fmt.Errorf("organization must be empty for repository scope")
		}
		if target.Repository == "" {
			return TargetConfig{}, fmt.Errorf("repository must be owner/name")
		}
		if len(target.AllowedRepositories) == 0 {
			return TargetConfig{}, fmt.Errorf("allowed_repositories is required")
		}
		if !repositoryAllowed(target.Repository, target.AllowedRepositories) {
			return TargetConfig{}, fmt.Errorf("repository must be listed in allowed_repositories")
		}
	case TargetScopeOrganization:
		if target.Organization == "" {
			return TargetConfig{}, fmt.Errorf("organization is required for org scope")
		}
		if target.Repository != "" {
			return TargetConfig{}, fmt.Errorf("repository must be empty for org scope")
		}
		if len(target.AllowedRepositories) == 0 {
			return TargetConfig{}, fmt.Errorf("allowed_repositories is required for org scope")
		}
	default:
		return TargetConfig{}, fmt.Errorf("unsupported security alert scope %q", target.Scope)
	}
	if target.RepositoryAlertLimit < 0 || target.RepositoryAlertLimit > 100 {
		return TargetConfig{}, fmt.Errorf("repository_alert_limit must be between 0 and 100")
	}
	if target.MaxPages <= 0 {
		target.MaxPages = 1
	}
	if target.MaxPages > 100 {
		return TargetConfig{}, fmt.Errorf("max_pages must be between 1 and 100")
	}
	if target.APIBaseURL != "" {
		if _, err := sdk.ParseBaseURL("github dependabot", target.APIBaseURL); err != nil {
			return TargetConfig{}, fmt.Errorf("api_base_url is invalid")
		}
	}
	return target, nil
}

func normalizeOrganization(organization string) string {
	return shard.NormalizeOrganization(organization)
}

func cleanRepositories(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := normalizeRepository(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func repositoryAllowed(repository string, allowed []string) bool {
	for _, value := range allowed {
		if value == repository {
			return true
		}
	}
	return false
}

// RepositoryAllowed reports whether the given full repository name
// (owner/repo, already normalized) is in the target's allowed_repositories
// list. For org targets this is the allowlist guardrail that bounds which
// repositories receive fan-out facts.
func (t TargetConfig) RepositoryAllowed(fullName string) bool {
	return repositoryAllowed(fullName, t.AllowedRepositories)
}

// normalizeRepository is the shared owner/name normalization
// (shard.NormalizeRepository) the planner and the query layer also use.
func normalizeRepository(repository string) string {
	return shard.NormalizeRepository(repository)
}
