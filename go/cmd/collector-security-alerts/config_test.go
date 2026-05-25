package main

import (
	"strings"
	"testing"

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
