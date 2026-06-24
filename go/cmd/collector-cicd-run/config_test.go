// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsCICDRunInstanceAndLoadsTokenEnv(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "cicd-run-primary",
				"collector_kind": "ci_cd_run",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"targets": [{
						"provider": "github_actions",
						"scope_id": "ci-cd:github-actions:example-org/example-repo",
						"repository": "example-org/example-repo",
						"token_env": "GITHUB_TOKEN",
						"allowed_repositories": ["example-org/example-repo"],
						"api_base_url": "https://api.github.com",
						"max_runs": 1,
						"max_jobs": 25,
						"max_artifacts": 25
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "cicd-run-primary"
		case envOwnerID:
			return "pod-cicd-run"
		case "GITHUB_TOKEN":
			return "token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorCICDRun; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "pod-cicd-run"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := config.Source.Targets[0].Token, "token-value"; got != want {
		t.Fatalf("Target token = %q, want %q", got, want)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testCICDRunGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsMissingGitHubToken(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "cicd-run-primary",
			"collector_kind": "ci_cd_run",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "github_actions",
					"scope_id": "ci-cd:github-actions:example-org/example-repo",
					"repository": "example-org/example-repo",
					"token_env": "GITHUB_TOKEN",
					"allowed_repositories": ["example-org/example-repo"],
					"max_runs": 1,
					"max_jobs": 25,
					"max_artifacts": 25
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

func TestLoadClaimedRuntimeConfigRejectsDisabledOrUnclaimableInstances(t *testing.T) {
	t.Parallel()

	for name, flags := range map[string]string{
		"disabled":    `"enabled": false, "claims_enabled": true`,
		"unclaimable": `"enabled": true, "claims_enabled": false`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := loadClaimedRuntimeConfig(func(key string) string {
				if key == "GITHUB_TOKEN" {
					return "token-value"
				}
				if key != envCollectorInstances {
					return ""
				}
				return `[{
					"instance_id": "cicd-run-primary",
					"collector_kind": "ci_cd_run",
					"mode": "continuous",
					` + flags + `,
					"configuration": {
						"targets": [{
							"provider": "github_actions",
							"scope_id": "ci-cd:github-actions:example-org/example-repo",
							"repository": "example-org/example-repo",
							"token_env": "GITHUB_TOKEN",
							"allowed_repositories": ["example-org/example-repo"],
							"max_runs": 1,
							"max_jobs": 25,
							"max_artifacts": 25
						}]
					}
				}]`
			})
			if err == nil {
				t.Fatal("loadClaimedRuntimeConfig() error = nil, want instance validation error")
			}
		})
	}
}

func TestLoadClaimedRuntimeConfigRejectsHeartbeatAtOrAboveLeaseTTL(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return testCICDRunCollectorInstancesJSON()
		case "GITHUB_TOKEN":
			return "token-value"
		case envClaimLeaseTTL:
			return "30s"
		case envHeartbeatInterval:
			return "30s"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want heartbeat lease error")
	}
	if !strings.Contains(err.Error(), "heartbeat interval") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want heartbeat interval", err)
	}
}

func testCICDRunCollectorInstancesJSON() string {
	return `[{
		"instance_id": "cicd-run-primary",
		"collector_kind": "ci_cd_run",
		"mode": "continuous",
		"enabled": true,
		"claims_enabled": true,
		"configuration": {
			"targets": [{
				"provider": "github_actions",
				"scope_id": "ci-cd:github-actions:example-org/example-repo",
				"repository": "example-org/example-repo",
				"token_env": "GITHUB_TOKEN",
				"allowed_repositories": ["example-org/example-repo"],
				"max_runs": 1,
				"max_jobs": 25,
				"max_artifacts": 25
			}]
		}
	}]`
}

func testCICDRunGetenv(key string) string {
	switch key {
	case envCollectorInstances:
		return testCICDRunCollectorInstancesJSON()
	case "GITHUB_TOKEN":
		return "token-value"
	case envCollectorInstanceID:
		return "cicd-run-primary"
	case envOwnerID:
		return "pod-cicd-run"
	default:
		return ""
	}
}
