package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxSecurityAlertRepositoryAlertLimit = 100
	maxSecurityAlertPages                = 100

	securityAlertScopeRepository = "repository"
	securityAlertScopeOrg        = "org"
)

type securityAlertCollectorConfiguration struct {
	Targets []securityAlertTargetConfiguration `json:"targets"`
}

type securityAlertTargetConfiguration struct {
	Provider             string   `json:"provider"`
	Scope                string   `json:"scope"`
	ScopeID              string   `json:"scope_id"`
	Repository           string   `json:"repository"`
	Organization         string   `json:"organization"`
	TokenEnv             string   `json:"token_env"`
	AllowedRepositories  []string `json:"allowed_repositories"`
	APIBaseURL           string   `json:"api_base_url"`
	RepositoryAlertLimit int      `json:"repository_alert_limit"`
	MaxPages             int      `json:"max_pages"`
	SourceURI            string   `json:"source_uri"`
}

// ValidateSecurityAlertCollectorConfiguration checks bounded provider security
// alert targets without resolving credentials or contacting a provider.
func ValidateSecurityAlertCollectorConfiguration(raw string) error {
	var decoded securityAlertCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode security alert collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("security alert collector configuration requires targets")
	}
	seen := make(map[string]struct{}, len(decoded.Targets))
	for i, target := range decoded.Targets {
		if err := validateSecurityAlertTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate security alert target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func validateSecurityAlertTargetConfiguration(target securityAlertTargetConfiguration) error {
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	switch strings.TrimSpace(target.Provider) {
	case "github_dependabot":
	case "":
		return fmt.Errorf("provider is required")
	default:
		return fmt.Errorf("unsupported security alert provider %q", strings.TrimSpace(target.Provider))
	}
	if strings.TrimSpace(target.TokenEnv) == "" {
		return fmt.Errorf("token_env is required")
	}
	scope := strings.TrimSpace(target.Scope)
	if scope == "" {
		scope = securityAlertScopeRepository
	}
	switch scope {
	case securityAlertScopeRepository:
		repository := normalizeSecurityAlertRepository(target.Repository)
		if repository == "" {
			return fmt.Errorf("repository must be owner/name")
		}
		if strings.TrimSpace(target.Organization) != "" {
			return fmt.Errorf("organization must be empty for repository scope")
		}
		if len(cleanSecurityAlertRepositories(target.AllowedRepositories)) == 0 {
			return fmt.Errorf("allowed_repositories is required")
		}
		if !securityAlertRepositoryAllowed(repository, target.AllowedRepositories) {
			return fmt.Errorf("repository must be listed in allowed_repositories")
		}
	case securityAlertScopeOrg:
		if strings.TrimSpace(target.Organization) == "" {
			return fmt.Errorf("organization is required for org scope")
		}
		if strings.TrimSpace(target.Repository) != "" {
			return fmt.Errorf("repository must be empty for org scope")
		}
		if len(cleanSecurityAlertRepositories(target.AllowedRepositories)) == 0 {
			return fmt.Errorf("allowed_repositories is required for org scope")
		}
	default:
		return fmt.Errorf("unsupported security alert scope %q", scope)
	}
	if target.RepositoryAlertLimit < 0 || target.RepositoryAlertLimit > maxSecurityAlertRepositoryAlertLimit {
		return fmt.Errorf("repository_alert_limit must be between 0 and %d", maxSecurityAlertRepositoryAlertLimit)
	}
	if target.MaxPages < 0 || target.MaxPages > maxSecurityAlertPages {
		return fmt.Errorf("max_pages must be between 1 and %d", maxSecurityAlertPages)
	}
	if err := validateSecurityAlertURL("api_base_url", target.APIBaseURL, true); err != nil {
		return err
	}
	return validateSecurityAlertURL("source_uri", target.SourceURI, false)
}

func validateSecurityAlertURL(field, raw string, requireHTTPS bool) error {
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

func securityAlertRepositoryAllowed(repository string, allowed []string) bool {
	for _, value := range allowed {
		if normalizeSecurityAlertRepository(value) == repository {
			return true
		}
	}
	return false
}

func cleanSecurityAlertRepositories(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeSecurityAlertRepository(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeSecurityAlertRepository(repository string) string {
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
