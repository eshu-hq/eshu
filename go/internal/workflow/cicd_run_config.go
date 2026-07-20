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
)

type cicdRunCollectorConfiguration struct {
	Targets []cicdRunTargetConfiguration `json:"targets"`
}

type cicdRunTargetConfiguration struct {
	Provider            string   `json:"provider"`
	ScopeID             string   `json:"scope_id"`
	Repository          string   `json:"repository"`
	TokenEnv            string   `json:"token_env"`
	AllowedRepositories []string `json:"allowed_repositories"`
	APIBaseURL          string   `json:"api_base_url"`
	MaxRuns             int      `json:"max_runs"`
	MaxJobs             int      `json:"max_jobs"`
	MaxArtifacts        int      `json:"max_artifacts"`
	SourceURI           string   `json:"source_uri"`
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

func validateCICDRunTargetConfiguration(target cicdRunTargetConfiguration) error {
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	switch strings.TrimSpace(target.Provider) {
	case "github_actions":
	case "":
		return fmt.Errorf("provider is required")
	default:
		return fmt.Errorf("unsupported ci/cd run provider %q", strings.TrimSpace(target.Provider))
	}
	repository := normalizeCICDRepository(target.Repository)
	if repository == "" {
		return fmt.Errorf("repository must be owner/name")
	}
	if strings.TrimSpace(target.TokenEnv) == "" {
		return fmt.Errorf("token_env is required")
	}
	if len(cleanCICDRepositories(target.AllowedRepositories)) == 0 {
		return fmt.Errorf("allowed_repositories is required")
	}
	if !cicdRepositoryAllowed(repository, target.AllowedRepositories) {
		return fmt.Errorf("repository must be listed in allowed_repositories")
	}
	// An omitted/zero max_runs is valid here: the ghactionsruntime collector
	// resolves it to its own default (10, see defaultMaxRuns in
	// go/internal/collector/cicdrun/ghactionsruntime/source.go) rather than
	// requiring every target config to spell out a limit. Only an explicit
	// out-of-range value (negative, or above the hard cap) is rejected.
	if target.MaxRuns < 0 || target.MaxRuns > maxCICDRunLimit {
		// The default of 10 is owned by ghactionsruntime.defaultMaxRuns; this
		// validation layer only bounds the range and accepts 0 to defer to it.
		return fmt.Errorf("max_runs must be between 0 and %d (0 uses the default of 10)", maxCICDRunLimit)
	}
	if target.MaxJobs <= 0 || target.MaxJobs > maxCICDJobLimit {
		return fmt.Errorf("max_jobs must be between 1 and %d", maxCICDJobLimit)
	}
	if target.MaxArtifacts <= 0 || target.MaxArtifacts > maxCICDArtifactLimit {
		return fmt.Errorf("max_artifacts must be between 1 and %d", maxCICDArtifactLimit)
	}
	if err := validateCICDURL("api_base_url", target.APIBaseURL, true); err != nil {
		return err
	}
	return validateCICDURL("source_uri", target.SourceURI, false)
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

func cicdRepositoryAllowed(repository string, allowed []string) bool {
	for _, value := range allowed {
		if normalizeCICDRepository(value) == repository {
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
		}
	}
	return out
}

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
