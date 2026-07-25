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
	if got, want := config.GitHubSource.Targets[0].Token, "token-value"; got != want {
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

// TestLoadClaimedRuntimeConfigSplitsTargetsByProvider proves a single ci_cd_run
// instance configuring BOTH a github_actions and a gitlab_ci target routes
// each to its own SourceConfig (codex P1 on PR #5778: before gitlabciruntime
// and this split existed, service.go only ever constructed
// ghactionsruntime.NewClaimedSource with ghactionsruntime.GitHubClient, so a
// gitlab_ci target was silently unreachable -- accepted by config parsing but
// never actually fetched from).
func TestLoadClaimedRuntimeConfigSplitsTargetsByProvider(t *testing.T) {
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
						"max_runs": 1,
						"max_jobs": 25,
						"max_artifacts": 25
					}, {
						"provider": "gitlab_ci",
						"scope_id": "gitlab-ci://gitlab.com/eshu-hq/demo",
						"repository": "eshu-hq/demo",
						"token_env": "GITLAB_TOKEN",
						"allowed_repositories": ["eshu-hq/demo"],
						"max_runs": 1,
						"max_jobs": 25
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "cicd-run-primary"
		case "GITHUB_TOKEN":
			return "github-token-value"
		case "GITLAB_TOKEN":
			return "gitlab-token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := len(config.GitHubSource.Targets), 1; got != want {
		t.Fatalf("len(GitHubSource.Targets) = %d, want %d", got, want)
	}
	if got, want := config.GitHubSource.Targets[0].Token, "github-token-value"; got != want {
		t.Fatalf("GitHubSource.Targets[0].Token = %q, want %q", got, want)
	}
	if got, want := len(config.GitLabSource.Targets), 1; got != want {
		t.Fatalf("len(GitLabSource.Targets) = %d, want %d", got, want)
	}
	if got, want := config.GitLabSource.Targets[0].Token, "gitlab-token-value"; got != want {
		t.Fatalf("GitLabSource.Targets[0].Token = %q, want %q", got, want)
	}
	if got, want := config.GitLabSource.Targets[0].ProjectPath, "eshu-hq/demo"; got != want {
		t.Fatalf("GitLabSource.Targets[0].ProjectPath = %q, want %q", got, want)
	}
}

// TestLoadClaimedRuntimeConfigRejectsUnsupportedProvider proves an
// unrecognized provider value fails loudly at config-parse time rather than
// silently being dropped or misrouted to the wrong runtime.
func TestLoadClaimedRuntimeConfigRejectsUnsupportedProvider(t *testing.T) {
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
					"provider": "bitbucket_pipelines",
					"scope_id": "ci-cd:bitbucket:example/repo",
					"repository": "example/repo",
					"token_env": "TOKEN",
					"allowed_repositories": ["example/repo"],
					"max_runs": 1,
					"max_jobs": 25
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want unsupported provider rejection")
	}
	if !strings.Contains(err.Error(), "unsupported ci/cd run provider") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want \"unsupported ci/cd run provider\"", err)
	}
}

// TestBuildClaimedServiceRoutesMixedProviderTargets proves
// buildClaimedService, given a config with both provider targets, wires a
// Source that resolves BOTH scope_ids rather than only the github_actions
// one (the codex P1 regression this change fixes).
func TestBuildClaimedServiceRoutesMixedProviderTargets(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, func(key string) string {
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
						"max_runs": 1,
						"max_jobs": 25,
						"max_artifacts": 25
					}, {
						"provider": "gitlab_ci",
						"scope_id": "gitlab-ci://gitlab.com/eshu-hq/demo",
						"repository": "eshu-hq/demo",
						"token_env": "GITLAB_TOKEN",
						"allowed_repositories": ["eshu-hq/demo"],
						"max_runs": 1,
						"max_jobs": 25
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "cicd-run-primary"
		case "GITHUB_TOKEN":
			return "github-token-value"
		case "GITLAB_TOKEN":
			return "gitlab-token-value"
		default:
			return ""
		}
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	routed, ok := service.Source.(providerRoutedSource)
	if !ok {
		t.Fatalf("Source = %T, want providerRoutedSource", service.Source)
	}
	for _, scopeID := range []string{
		"ci-cd:github-actions:example-org/example-repo",
		"gitlab-ci://gitlab.com/eshu-hq/demo",
	} {
		if _, ok := routed.byScopeID[scopeID]; !ok {
			t.Fatalf("byScopeID missing entry for %q", scopeID)
		}
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
