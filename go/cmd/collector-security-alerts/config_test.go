package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadClaimedRuntimeConfigSelectsSecurityAlertInstanceAndLoadsTokenEnv(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "security-alert-primary",
				"collector_kind": "security_alert",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"targets": [{
						"provider": "github_dependabot",
						"scope_id": "security-alert:github:example-org/example-repo",
						"repository": "example-org/example-repo",
						"token_env": "GITHUB_TOKEN",
						"allowed_repositories": ["example-org/example-repo"],
						"repository_alert_limit": 25,
						"max_pages": 2
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "security-alert-primary"
		case envOwnerID:
			return "pod-security-alerts"
		case "GITHUB_TOKEN":
			return "token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorSecurityAlert; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.Source.Targets[0].Token, "token-value"; got != want {
		t.Fatalf("Target token = %q, want %q", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsMissingGitHubToken(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "security-alert-primary",
			"collector_kind": "security_alert",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"]
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want missing credential error")
	}
	if strings.Contains(err.Error(), "token-value") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want credential env reference without value", err)
	}
}

func TestRunProviderAccessPreflightReportsSanitizedAuthDenied(t *testing.T) {
	t.Parallel()

	client := &preflightAlertClient{
		err: securityalerts.GitHubDependabotError{
			StatusCode: 403,
			Message:    "raw upstream error mentions token-value and example-org/example-repo",
		},
	}
	err := runProviderAccessPreflightWithFactory(t.Context(), testSecurityAlertGetenv, func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
		return client, nil
	})
	if err == nil {
		t.Fatal("runProviderAccessPreflightWithFactory() error = nil, want auth-denied failure")
	}
	if got, want := client.maxPages, 1; got != want {
		t.Fatalf("preflight maxPages = %d, want %d", got, want)
	}
	if strings.Contains(err.Error(), "token-value") || strings.Contains(err.Error(), "example-org/example-repo") {
		t.Fatalf("runProviderAccessPreflightWithFactory() error = %q, want sanitized failure", err)
	}
	if !strings.Contains(err.Error(), "auth_denied") {
		t.Fatalf("runProviderAccessPreflightWithFactory() error = %q, want auth_denied", err)
	}
}

func TestRunProviderAccessPreflightPassesWithBoundedProviderRead(t *testing.T) {
	t.Parallel()

	client := &preflightAlertClient{}
	if err := runProviderAccessPreflightWithFactory(t.Context(), testSecurityAlertGetenv, func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
		return client, nil
	}); err != nil {
		t.Fatalf("runProviderAccessPreflightWithFactory() error = %v, want nil", err)
	}
	if got, want := client.maxPages, 1; got != want {
		t.Fatalf("preflight maxPages = %d, want %d", got, want)
	}
}

func testSecurityAlertGetenv(key string) string {
	switch key {
	case envCollectorInstances:
		return `[{
			"instance_id": "security-alert-primary",
			"collector_kind": "security_alert",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_dependabot",
					"scope_id": "security-alert:github:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"],
					"repository_alert_limit": 25,
					"max_pages": 2
				}]
			}
		}]`
	case envCollectorInstanceID:
		return "security-alert-primary"
	case "GITHUB_TOKEN":
		return "token-value"
	default:
		return ""
	}
}

type preflightAlertClient struct {
	err      error
	maxPages int
}

func (c *preflightAlertClient) ListRepositoryAlertsPages(
	_ context.Context,
	_ string,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	c.maxPages = maxPages
	return securityalerts.GitHubDependabotAlertResult{}, c.err
}
