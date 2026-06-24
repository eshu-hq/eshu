// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/jira"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type jiraRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
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
	MetadataLimit   int    `json:"metadata_limit"`
}

func loadJiraSourceConfig(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (jira.SourceConfig, error) {
	var decoded jiraRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return jira.SourceConfig{}, fmt.Errorf("decode jira collector configuration: %w", err)
	}
	targets := make([]jira.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return jira.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return jira.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Targets:             targets,
	}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (jira.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return jira.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	email := ""
	if emailEnv := strings.TrimSpace(target.EmailEnv); emailEnv != "" {
		email = strings.TrimSpace(getenv(emailEnv))
	}
	lookback, err := parseOptionalDuration(target.UpdatedLookback)
	if err != nil {
		return jira.TargetConfig{}, err
	}
	jql, err := resolveJQL(target, getenv)
	if err != nil {
		return jira.TargetConfig{}, err
	}
	return jira.TargetConfig{
		Provider:        strings.TrimSpace(target.Provider),
		ScopeID:         strings.TrimSpace(target.ScopeID),
		SiteID:          strings.TrimSpace(target.SiteID),
		BaseURL:         strings.TrimRight(strings.TrimSpace(target.BaseURL), "/"),
		Email:           email,
		Token:           token,
		JQL:             jql,
		IssueLimit:      target.IssueLimit,
		UpdatedLookback: lookback,
		ChangelogLimit:  target.ChangelogLimit,
		RemoteLinkLimit: target.RemoteLinkLimit,
		MetadataLimit:   target.MetadataLimit,
	}, nil
}

func resolveJQL(target targetJSON, getenv func(string) string) (string, error) {
	directJQL := strings.TrimSpace(target.JQL)
	jqlEnv := strings.TrimSpace(target.JQLEnv)
	switch {
	case directJQL != "" && jqlEnv != "":
		return "", fmt.Errorf("only one of jql or jql_env may be set")
	case directJQL != "":
		return directJQL, nil
	case jqlEnv != "":
		jql := strings.TrimSpace(getenv(jqlEnv))
		if jql == "" {
			return "", fmt.Errorf("jql_env %s did not resolve a JQL query", jqlEnv)
		}
		return jql, nil
	default:
		return "", fmt.Errorf("jql or jql_env is required")
	}
}

func parseOptionalDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse updated_lookback: %w", err)
	}
	return value, nil
}
