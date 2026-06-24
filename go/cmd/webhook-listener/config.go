// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultMaxWebhookBodyBytes = int64(1 << 20)

type webhookListenerConfig struct {
	GitHubSecret        string
	GitLabToken         string
	BitbucketSecret     string
	PagerDutySecret     string
	JiraSecret          string
	AWSFreshnessToken   string
	GitHubPath          string
	GitLabPath          string
	BitbucketPath       string
	PagerDutyPath       string
	JiraPath            string
	AWSFreshnessPath    string
	PagerDutyScopeID    string
	JiraScopeID         string
	MaxRequestBodyBytes int64
	DefaultBranch       string
}

func loadWebhookListenerConfig(getenv func(string) string) (webhookListenerConfig, error) {
	if getenv == nil {
		return webhookListenerConfig{}, fmt.Errorf("webhook listener getenv is required")
	}
	cfg := webhookListenerConfig{
		GitHubSecret:        strings.TrimSpace(getenv("ESHU_WEBHOOK_GITHUB_SECRET")),
		GitLabToken:         strings.TrimSpace(getenv("ESHU_WEBHOOK_GITLAB_TOKEN")),
		BitbucketSecret:     strings.TrimSpace(getenv("ESHU_WEBHOOK_BITBUCKET_SECRET")),
		PagerDutySecret:     strings.TrimSpace(getenv("ESHU_WEBHOOK_PAGERDUTY_SECRET")),
		JiraSecret:          strings.TrimSpace(getenv("ESHU_WEBHOOK_JIRA_SECRET")),
		AWSFreshnessToken:   strings.TrimSpace(getenv("ESHU_AWS_FRESHNESS_TOKEN")),
		GitHubPath:          firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_GITHUB_PATH")), "/webhooks/github"),
		GitLabPath:          firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_GITLAB_PATH")), "/webhooks/gitlab"),
		BitbucketPath:       firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_BITBUCKET_PATH")), "/webhooks/bitbucket"),
		PagerDutyPath:       firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_PAGERDUTY_PATH")), "/webhooks/pagerduty"),
		JiraPath:            firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_JIRA_PATH")), "/webhooks/jira"),
		AWSFreshnessPath:    firstNonEmpty(strings.TrimSpace(getenv("ESHU_AWS_FRESHNESS_PATH")), "/webhooks/aws/eventbridge"),
		PagerDutyScopeID:    strings.TrimSpace(getenv("ESHU_WEBHOOK_PAGERDUTY_SCOPE_ID")),
		JiraScopeID:         strings.TrimSpace(getenv("ESHU_WEBHOOK_JIRA_SCOPE_ID")),
		MaxRequestBodyBytes: int64FromEnv(getenv, "ESHU_WEBHOOK_MAX_BODY_BYTES", defaultMaxWebhookBodyBytes),
		DefaultBranch:       strings.TrimSpace(getenv("ESHU_WEBHOOK_DEFAULT_BRANCH")),
	}
	if cfg.GitHubSecret == "" && cfg.GitLabToken == "" && cfg.BitbucketSecret == "" &&
		cfg.PagerDutySecret == "" && cfg.JiraSecret == "" && cfg.AWSFreshnessToken == "" {
		return webhookListenerConfig{}, fmt.Errorf("at least one webhook provider secret or AWS freshness token is required")
	}
	if cfg.PagerDutySecret != "" && cfg.PagerDutyScopeID == "" {
		return webhookListenerConfig{}, fmt.Errorf("pagerduty webhook scope id is required")
	}
	if cfg.JiraSecret != "" && cfg.JiraScopeID == "" {
		return webhookListenerConfig{}, fmt.Errorf("jira webhook scope id is required")
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		return webhookListenerConfig{}, fmt.Errorf("webhook max body bytes must be positive")
	}
	return cfg, nil
}

func int64FromEnv(getenv func(string) string, key string, defaultValue int64) int64 {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
