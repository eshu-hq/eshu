// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
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
	if got := config.Source.Targets[0].JQL; got != "project = OPS ORDER BY updated ASC" {
		t.Fatalf("JQL = %q, want direct JQL", got)
	}
	if got := config.Source.Targets[0].MetadataLimit; got != 25 {
		t.Fatalf("MetadataLimit = %d, want 25", got)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testJiraGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigResolvesJQLEnv(t *testing.T) {
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
					"token_env":"JIRA_API_TOKEN",
					"jql_env":"ESHU_JIRA_JQL",
					"issue_limit":25,
					"updated_lookback":"24h",
					"changelog_limit":25,
					"remote_link_limit":25
				}]
			}
		}]`,
		"JIRA_API_TOKEN": "secret-token",
		"ESHU_JIRA_JQL":  "project = OPS AND updated >= -7d ORDER BY updated ASC",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got := config.Source.Targets[0].JQL; got != "project = OPS AND updated >= -7d ORDER BY updated ASC" {
		t.Fatalf("JQL = %q, want env-backed JQL", got)
	}
}

func TestLoadClaimedRuntimeConfigRejectsMissingJQLEnv(t *testing.T) {
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
					"token_env":"JIRA_API_TOKEN",
					"jql_env":"ESHU_JIRA_JQL",
					"issue_limit":25,
					"updated_lookback":"24h",
					"changelog_limit":25,
					"remote_link_limit":25
				}]
			}
		}]`,
		"JIRA_API_TOKEN": "secret-token",
	}

	_, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want missing JQL env")
	}
	if got := err.Error(); got != "targets[0]: jql_env ESHU_JIRA_JQL did not resolve a JQL query" {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want missing JQL env message", got)
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

func testJiraGetenv(key string) string {
	switch key {
	case envCollectorInstances:
		return `[{
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
					"token_env":"JIRA_API_TOKEN",
					"jql":"project = OPS ORDER BY updated ASC"
				}]
			}
		}]`
	case "JIRA_API_TOKEN":
		return "secret-token"
	case envCollectorInstanceID:
		return "jira-primary"
	case envCollectorOwnerID:
		return "jira-owner"
	default:
		return ""
	}
}
