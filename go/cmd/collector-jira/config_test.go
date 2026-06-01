package main

import (
	"testing"
	"time"
)

func TestLoadClaimedRuntimeConfigSelectsJiraInstanceAndResolvesCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"jira-primary",
			"collector_kind":"jira",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"jira_cloud",
					"scope_id":"jira:site:example",
					"site_id":"example.atlassian.net",
					"base_url":"https://example.atlassian.net",
					"email_env":"JIRA_EMAIL",
					"token_env":"JIRA_API_TOKEN",
					"jql":"project = OPS ORDER BY updated ASC",
					"issue_limit":25,
					"updated_lookback":"24h",
					"changelog_limit":25,
					"remote_link_limit":25,
					"metadata_limit":25
				}]
			}
		}]`,
		"JIRA_EMAIL":           "user@example.com",
		"JIRA_API_TOKEN":       "secret-token",
		envPollInterval:        "2s",
		envClaimLeaseTTL:       "30s",
		envHeartbeatInterval:   "10s",
		envCollectorOwnerID:    "jira-owner",
		envCollectorInstanceID: "jira-primary",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.InstanceID != "jira-primary" {
		t.Fatalf("InstanceID = %q, want jira-primary", config.Instance.InstanceID)
	}
	if config.OwnerID != "jira-owner" {
		t.Fatalf("OwnerID = %q, want jira-owner", config.OwnerID)
	}
	if config.PollInterval != 2*time.Second {
		t.Fatalf("PollInterval = %s, want 2s", config.PollInterval)
	}
	if got := config.Source.Targets[0].Token; got != "secret-token" {
		t.Fatalf("resolved token = %q, want secret-token", got)
	}
	if got := config.Source.Targets[0].Email; got != "user@example.com" {
		t.Fatalf("resolved email = %q, want user@example.com", got)
	}
	if got := config.Source.Targets[0].MetadataLimit; got != 25 {
		t.Fatalf("MetadataLimit = %d, want 25", got)
	}
}

func TestLoadClaimedRuntimeConfigRejectsUnresolvedCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"jira-primary",
			"collector_kind":"jira",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"jira_cloud",
					"scope_id":"jira:site:example",
					"site_id":"example.atlassian.net",
					"token_env":"JIRA_API_TOKEN"
				}]
			}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want unresolved credential")
	}
}
