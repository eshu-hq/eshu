// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ghactionsruntime target validation and URL/repository normalization,
// split out of source.go to keep both files clear of the 500-line cap.
package ghactionsruntime

import (
	"fmt"
	"net/url"
	"strings"
)

func validateTarget(target TargetConfig) (TargetConfig, error) {
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.Repository = normalizeRepository(target.Repository)
	target.Token = strings.TrimSpace(target.Token)
	target.APIBaseURL = strings.TrimSpace(target.APIBaseURL)
	target.SourceURI = strings.TrimSpace(target.SourceURI)
	if target.APIBaseURL == "" {
		target.APIBaseURL = "https://api.github.com"
	}
	if target.SourceURI == "" && target.Repository != "" {
		target.SourceURI = "https://github.com/" + target.Repository
	}
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.Repository == "" {
		return TargetConfig{}, fmt.Errorf("repository must be owner/name")
	}
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
	}
	if !repositoryAllowed(target.Repository, target.AllowedRepositories) {
		return TargetConfig{}, fmt.Errorf("repository must be listed in allowed_repositories")
	}
	if target.MaxRuns == 0 {
		target.MaxRuns = defaultMaxRuns
	}
	if target.MaxRuns < 0 || target.MaxRuns > maxRunPages {
		return TargetConfig{}, fmt.Errorf("max_runs must be between 0 and %d (0 uses the default of %d)", maxRunPages, defaultMaxRuns)
	}
	if target.MaxJobs <= 0 || target.MaxJobs > maxJobPages {
		return TargetConfig{}, fmt.Errorf("max_jobs must be between 1 and %d", maxJobPages)
	}
	if target.MaxArtifacts <= 0 || target.MaxArtifacts > maxArtifactPages {
		return TargetConfig{}, fmt.Errorf("max_artifacts must be between 1 and %d", maxArtifactPages)
	}
	target, err := boundMaxDeployments(target)
	if err != nil {
		return TargetConfig{}, err
	}
	if err := validateTargetURL("api_base_url", target.APIBaseURL, true); err != nil {
		return TargetConfig{}, err
	}
	if err := validateTargetURL("source_uri", target.SourceURI, false); err != nil {
		return TargetConfig{}, err
	}
	return target, nil
}

func sanitizeArtifacts(artifacts []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		next := make(map[string]any, len(artifact))
		for key, value := range artifact {
			if key == "archive_download_url" {
				if raw, ok := value.(string); ok {
					next[key] = stripURLQuery(raw)
					continue
				}
			}
			next[key] = value
		}
		out = append(out, next)
	}
	return out
}

func stripURLQuery(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func repositoryAllowed(repository string, allowed []string) bool {
	for _, candidate := range allowed {
		if normalizeRepository(candidate) == repository {
			return true
		}
	}
	return false
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

func validateTargetURL(field, raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
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
