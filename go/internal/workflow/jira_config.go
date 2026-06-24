// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	maxJiraIssueLimit      = 100
	maxJiraChangelogLimit  = 100
	maxJiraRemoteLinkLimit = 100
	maxJiraUpdatedLookback = 30 * 24 * time.Hour
)

type jiraCollectorConfiguration struct {
	Targets []jiraTargetConfiguration `json:"targets"`
}

type jiraTargetConfiguration struct {
	Provider        string `json:"provider"`
	ScopeID         string `json:"scope_id"`
	SiteID          string `json:"site_id"`
	BaseURL         string `json:"base_url"`
	EmailEnv        string `json:"email_env"`
	TokenEnv        string `json:"token_env"`
	JQL             string `json:"jql"`
	JQLEnv          string `json:"jql_env"`
	IssueLimit      int    `json:"issue_limit"`
	UpdatedLookback string `json:"updated_lookback"`
	ChangelogLimit  int    `json:"changelog_limit"`
	RemoteLinkLimit int    `json:"remote_link_limit"`
}

// ValidateJiraCollectorConfiguration checks bounded Jira work-item collector
// targets without resolving credentials or contacting Jira.
func ValidateJiraCollectorConfiguration(raw string) error {
	var decoded jiraCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode jira collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("jira collector configuration requires targets")
	}
	seen := make(map[string]struct{}, len(decoded.Targets))
	for i, target := range decoded.Targets {
		if err := validateJiraTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate jira target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func validateJiraTargetConfiguration(target jiraTargetConfiguration) error {
	switch strings.TrimSpace(target.Provider) {
	case "jira_cloud":
	case "":
		return fmt.Errorf("provider is required")
	default:
		return fmt.Errorf("unsupported jira provider %q", strings.TrimSpace(target.Provider))
	}
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	if strings.TrimSpace(target.SiteID) == "" {
		return fmt.Errorf("site_id is required")
	}
	if strings.TrimSpace(target.TokenEnv) == "" {
		return fmt.Errorf("token_env is required")
	}
	if err := validateJiraJQLSource(target); err != nil {
		return err
	}
	if err := validateJiraLimit("issue_limit", target.IssueLimit, maxJiraIssueLimit); err != nil {
		return err
	}
	if err := validateJiraLimit("changelog_limit", target.ChangelogLimit, maxJiraChangelogLimit); err != nil {
		return err
	}
	if err := validateJiraLimit("remote_link_limit", target.RemoteLinkLimit, maxJiraRemoteLinkLimit); err != nil {
		return err
	}
	if err := validateJiraBaseURL(target.BaseURL); err != nil {
		return err
	}
	return validateJiraUpdatedLookback(target.UpdatedLookback)
}

func validateJiraJQLSource(target jiraTargetConfiguration) error {
	hasDirectJQL := strings.TrimSpace(target.JQL) != ""
	hasJQLEnv := strings.TrimSpace(target.JQLEnv) != ""
	switch {
	case hasDirectJQL && hasJQLEnv:
		return fmt.Errorf("only one of jql or jql_env may be set")
	case hasDirectJQL || hasJQLEnv:
		return nil
	default:
		return fmt.Errorf("jql or jql_env is required")
	}
}

func validateJiraLimit(field string, value int, max int) error {
	if value < 0 || value > max {
		return fmt.Errorf("%s must be between 0 and %d", field, max)
	}
	return nil
}

func validateJiraBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("base_url must include scheme and host")
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("base_url must use https")
	}
	if parsed.User != nil {
		return fmt.Errorf("base_url must not include credentials")
	}
	return nil
}

func validateJiraUpdatedLookback(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	lookback, err := time.ParseDuration(trimmed)
	if err != nil {
		return fmt.Errorf("parse updated_lookback: %w", err)
	}
	if lookback <= 0 || lookback > maxJiraUpdatedLookback {
		return fmt.Errorf("updated_lookback must be greater than 0 and at most %s", maxJiraUpdatedLookback)
	}
	return nil
}
