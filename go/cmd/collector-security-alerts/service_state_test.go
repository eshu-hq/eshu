// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// orgStateGetenv configures one org target.
func orgStateGetenv() func(string) string {
	return orgStateGetenvAllowing(`"example-org/example-repo"`)
}

// orgStateGetenvAllowing is orgStateGetenv with the given JSON allowlist.
func orgStateGetenvAllowing(allowedJSON string) func(string) string {
	return func(key string) string {
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
						"scope": "org",
						"scope_id": "security-alert:github-org:example-org",
						"organization": "example-org",
						"token_env": "GITHUB_TOKEN",
						"allowed_repositories": [` + allowedJSON + `],
						"max_pages": 1
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
}

// TestBuildClaimedServiceCommitsOrgReadStateWithItsGeneration pins the #8943
// wiring on the production construction path: the service buildClaimedService
// returns has a committer that persists generation state, and its source hands
// an org claim a state writer. Both halves are needed: without either, a
// truncated org read commits without its record and every unread repository
// keeps reading complete.
func TestBuildClaimedServiceCommitsOrgReadStateWithItsGeneration(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedServiceWithClients(nil, orgStateGetenv(), nil, nil, nil,
		func(alertruntime.TargetConfig) (alertruntime.RepositoryAlertClient, error) {
			return emptyOrgClient{}, nil
		})
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if _, ok := service.Committer.(collector.StateClaimedCommitter); !ok {
		t.Fatalf("committer %T does not implement collector.StateClaimedCommitter", service.Committer)
	}

	now := time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)
	collected, ok, err := service.Source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "work-org",
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "security-alert-primary",
		ScopeID:             "security-alert:github-org:example-org",
		GenerationID:        "generation-org",
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v, err %v, want a collected org generation", ok, err)
	}
	if collected.StateWriter == nil {
		t.Fatal("org claim from the built service has no StateWriter: the read state is not wired")
	}
}

// emptyOrgClient answers every provider read with an empty, complete result.
type emptyOrgClient struct{}

func (emptyOrgClient) ListRepositoryAlertsPages(
	context.Context, string, int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	return securityalerts.GitHubDependabotAlertResult{}, nil
}

func (emptyOrgClient) ListOrganizationAlertsPages(
	context.Context, string, int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	return securityalerts.GitHubDependabotAlertResult{}, nil
}
