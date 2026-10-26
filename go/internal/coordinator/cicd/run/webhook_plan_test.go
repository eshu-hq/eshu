// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package run

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerWebhookScopesOnlyAuthorizedGitHubTarget(t *testing.T) {
	t.Parallel()
	observedAt := time.Date(2026, time.October, 26, 3, 0, 0, 0, time.UTC)
	config := strings.Replace(testCICDRunPlannerConfig(), `"max_artifacts": 100`, `"max_artifacts": 100},{"provider":"github_actions","scope_id":"ci-cd:github-actions:example/other","repository":"example/other","token_env":"OTHER_TOKEN","allowed_repositories":["example/other"],"max_jobs":100,"max_artifacts":100`, 1)
	instance := workflow.CollectorInstance{InstanceID: "ci-cd-primary", CollectorKind: scope.CollectorCICDRun, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: config, LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt}
	run, items, err := (WorkPlanner{}).PlanCICDRunWork(t.Context(), PlanRequest{Instance: instance, ObservedAt: observedAt, PlanKey: "webhook-workflow-delivery-1", TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{"ci-cd:github-actions:example/repo"}})
	if err != nil {
		t.Fatalf("PlanCICDRunWork() error = %v", err)
	}
	if run.TriggerKind != workflow.TriggerKindWebhook {
		t.Fatalf("TriggerKind = %q, want webhook", run.TriggerKind)
	}
	if len(items) != 1 || items[0].ScopeID != "ci-cd:github-actions:example/repo" {
		t.Fatalf("items = %+v, want one scoped target", items)
	}
	if strings.Contains(run.RequestedScopeSet, "example/other") {
		t.Fatalf("RequestedScopeSet includes unrequested target: %s", run.RequestedScopeSet)
	}
}

func TestWorkPlannerScheduledProviderGroupHasOnlyItsConfiguredScopes(t *testing.T) {
	observedAt := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	config := strings.Replace(testCICDRunPlannerConfig(), `"max_artifacts": 100`, `"max_artifacts": 100},{"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/example/demo","repository":"example/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["example/demo"],"max_runs":1,"max_jobs":10`, 1)
	instance := workflow.CollectorInstance{InstanceID: "mixed-instance", CollectorKind: scope.CollectorCICDRun, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: config, LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt}
	for _, test := range []struct {
		provider string
		want     string
		other    string
	}{
		{provider: "github_actions", want: `"provider":"github_actions"`, other: `"provider":"gitlab_ci"`},
		{provider: "gitlab_ci", want: `"provider":"gitlab_ci"`, other: `"provider":"github_actions"`},
	} {
		t.Run(test.provider, func(t *testing.T) {
			run, items, err := (WorkPlanner{}).PlanCICDRunWork(t.Context(), PlanRequest{Instance: instance, ObservedAt: observedAt, PlanKey: "continuous-20261026T040000Z-" + test.provider, ScheduledProvider: test.provider})
			if err != nil || len(items) != 1 {
				t.Fatalf("group plan items=%d err=%v", len(items), err)
			}
			if !strings.Contains(run.RequestedScopeSet, test.want) || strings.Contains(run.RequestedScopeSet, test.other) {
				t.Fatalf("provider %s requested scopes=%s", test.provider, run.RequestedScopeSet)
			}
		})
	}
}

func TestWorkPlannerRejectsInvalidScheduledProviderGroup(t *testing.T) {
	at := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{InstanceID: "ci-cd-primary", CollectorKind: scope.CollectorCICDRun, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: testCICDRunPlannerConfig(), LastObservedAt: at, CreatedAt: at, UpdatedAt: at}
	for _, test := range []struct {
		name    string
		request PlanRequest
	}{
		{name: "unsupported provider", request: PlanRequest{Instance: instance, ObservedAt: at, PlanKey: "scheduled-invalid", ScheduledProvider: "bitbucket"}},
		{name: "unconfigured provider", request: PlanRequest{Instance: instance, ObservedAt: at, PlanKey: "scheduled-unconfigured", ScheduledProvider: "gitlab_ci"}},
		{name: "webhook provider filter", request: PlanRequest{Instance: instance, ObservedAt: at, PlanKey: "webhook-filter", TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{"ci-cd:github-actions:example/repo"}, ScheduledProvider: "github_actions"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := (WorkPlanner{}).PlanCICDRunWork(t.Context(), test.request); err == nil {
				t.Fatal("invalid scheduled provider group accepted")
			}
		})
	}
}

func TestConfiguredGitHubRepositoryLookupsUseRuntimeCanonicalAliases(t *testing.T) {
	for _, configured := range []string{
		" /Example /Repo.git/ ",
		"EXAMPLE/REPO.git",
		" / example / repo / ",
	} {
		t.Run(configured, func(t *testing.T) {
			configuration := strings.Replace(testCICDRunPlannerConfig(), `"repository": "example/repo"`, `"repository": "`+configured+`"`, 1)
			configuration = strings.Replace(configuration, `"allowed_repositories": ["example/repo"]`, `"allowed_repositories": ["`+configured+`"]`, 1)
			if err := workflow.ValidateCICDRunCollectorConfiguration(configuration); err != nil {
				t.Fatalf("alias %q should be accepted by runtime configuration: %v", configured, err)
			}
			scopes, err := ConfiguredGitHubScopes(configuration, "example/repo")
			if err != nil || len(scopes) != 1 || scopes[0] != "ci-cd:github-actions:example/repo" {
				t.Fatalf("alias %q webhook scopes=%v err=%v", configured, scopes, err)
			}
			repositories, err := ConfiguredGitHubTargetRepositories(configuration)
			if err != nil || repositories["ci-cd:github-actions:example/repo"] != "example/repo" {
				t.Fatalf("alias %q pending repository map=%v err=%v", configured, repositories, err)
			}
		})
	}
}
