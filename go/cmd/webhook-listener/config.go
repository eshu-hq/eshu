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
	GitHubPath          string
	GitLabPath          string
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
		GitHubPath:          firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_GITHUB_PATH")), "/webhooks/github"),
		GitLabPath:          firstNonEmpty(strings.TrimSpace(getenv("ESHU_WEBHOOK_GITLAB_PATH")), "/webhooks/gitlab"),
		MaxRequestBodyBytes: int64FromEnv(getenv, "ESHU_WEBHOOK_MAX_BODY_BYTES", defaultMaxWebhookBodyBytes),
		DefaultBranch:       strings.TrimSpace(getenv("ESHU_WEBHOOK_DEFAULT_BRANCH")),
	}
	if cfg.GitHubSecret == "" && cfg.GitLabToken == "" {
		return webhookListenerConfig{}, fmt.Errorf("at least one webhook provider secret is required")
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
