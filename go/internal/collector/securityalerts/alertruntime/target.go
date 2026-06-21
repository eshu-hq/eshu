package alertruntime

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

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
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
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
	return strings.ToLower(strings.Trim(strings.TrimSpace(organization), "/"))
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

func normalizeRepository(repository string) string {
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
