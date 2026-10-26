// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeCICDRunPlanner struct {
	requests []cicdrun.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeCICDRunPlanner) PlanCICDRunWork(
	_ context.Context,
	request cicdrun.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesCICDRunWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "ci_cd_run:collector-ci-cd-run:schedule:continuous-20260607T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorCICDRun),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "ci-cd-run-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "collector-ci-cd-run",
		SourceSystem:        string(scope.CollectorCICDRun),
		ScopeID:             "github-actions://eshu-hq/eshu",
		AcceptanceUnitID:    "github-actions://eshu-hq/eshu",
		SourceRunID:         "ci_cd_run:generation-1",
		GenerationID:        "ci_cd_run:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeCICDRunPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceCICDRunInstance(now)},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-ci-cd-run",
				CollectorKind: scope.CollectorCICDRun,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceCICDRunConfiguration(),
			}},
		},
		Store:          store,
		CICDRunPlanner: planner,
		Clock:          func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260607T150000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func testServiceCICDRunInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-ci-cd-run",
		CollectorKind:  scope.CollectorCICDRun,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceCICDRunConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceCICDRunConfiguration() string {
	return `{
		"targets": [{
			"provider": "github_actions",
			"scope_id": "github-actions://eshu-hq/eshu",
			"repository": "eshu-hq/eshu",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["eshu-hq/eshu"],
			"max_runs": 5,
			"max_jobs": 20,
			"max_artifacts": 10
		}]
	}`
}

type pendingActionsRepositoryStub struct {
	CICDRunActionsTriggerStore
	pending bool
}

func (s pendingActionsRepositoryStub) HasPendingRepository(_ context.Context, repository string) (bool, error) {
	if repository != "eshu-hq/eshu" {
		return false, nil
	}
	return s.pending, nil
}

func TestCICDRunScheduledFallbackDefersToInvisiblePendingWebhook(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := testServiceCICDRunInstance(now)
	planner := cicdrun.WorkPlanner{}
	store := &fakeStore{}
	service := Service{
		Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true},
		Store:  store, CICDRunPlanner: planner,
		CICDRunActionsTriggers: pendingActionsRepositoryStub{pending: true},
	}
	if err := service.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueuedItems) != 0 {
		t.Fatalf("scheduled fallback admitted %d items while webhook delivery pending", len(store.enqueuedItems))
	}
}

func TestCICDRunFallbackKeepsGitLabWhenGitHubDeliveryPending(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := testServiceCICDRunInstance(now)
	instance.Configuration = `{"targets":[
  {"provider":"github_actions","scope_id":"github-actions://eshu-hq/eshu","repository":"eshu-hq/eshu","token_env":"GITHUB_TOKEN","allowed_repositories":["eshu-hq/eshu"],"max_runs":5,"max_jobs":20,"max_artifacts":10},
  {"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/demo","repository":"eshu-hq/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/demo"],"max_runs":1,"max_jobs":10}
 ]}`
	store := &fakeStore{}
	service := Service{
		Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true}, Store: store,
		CICDRunPlanner: cicdrun.WorkPlanner{}, CICDRunActionsTriggers: pendingActionsRepositoryStub{pending: true},
	}
	if err := service.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueuedItems) != 1 || store.enqueuedItems[0].ScopeID != "gitlab-ci://gitlab.com/eshu-hq/demo" {
		t.Fatalf("mixed fallback admitted %+v, want GitLab only", store.enqueuedItems)
	}
	if len(store.createdRuns) != 1 || !strings.Contains(store.createdRuns[0].RunID, "-gitlab-ci") {
		t.Fatalf("pending-webhook run IDs=%+v, want stable GitLab group", store.createdRuns)
	}
	gitlabRunID := store.createdRuns[0].RunID
	// The next tick can see the same configured provider set without a pending
	// delivery. It must reuse GitLab's plan identity while adding GitHub's.
	clearStore := &fakeStore{}
	service.Store = clearStore
	service.CICDRunActionsTriggers = pendingActionsRepositoryStub{pending: false}
	if err := service.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatal(err)
	}
	if len(clearStore.createdRuns) != 2 || clearStore.createdRuns[1].RunID != gitlabRunID {
		t.Fatalf("clear-webhook run IDs=%+v, want unchanged GitLab %q", clearStore.createdRuns, gitlabRunID)
	}
}

func TestCICDRunFallbackPreservesScheduledCadenceModes(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 17, 0, time.UTC)
	cases := []struct {
		name      string
		interval  time.Duration
		override  string
		bootstrap bool
		mode      workflow.CollectorMode
		want      string
	}{
		{name: "unset default", interval: 30 * time.Second, mode: workflow.CollectorModeContinuous, want: "continuous-20261026T040000Z"},
		{name: "explicit slower", interval: 30 * time.Second, override: "1h", mode: workflow.CollectorModeContinuous, want: "continuous-20261026T040000Z"},
		{name: "explicit faster valid custom global", interval: 5 * time.Second, override: "10s", mode: workflow.CollectorModeContinuous, want: "continuous-20261026T040010Z"},
		{name: "custom global", interval: 5 * time.Second, mode: workflow.CollectorModeContinuous, want: "continuous-20261026T040015Z"},
		{name: "bootstrap ignores override", interval: 30 * time.Second, override: "1h", bootstrap: true, mode: workflow.CollectorModeContinuous, want: "bootstrap"},
		{name: "manual mode", interval: 30 * time.Second, mode: workflow.CollectorModeManual, want: "manual-20261026T040000Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instance := testServiceCICDRunInstance(now)
			instance.Bootstrap = tc.bootstrap
			instance.Mode = tc.mode
			if tc.override != "" {
				instance.Configuration = strings.Replace(instance.Configuration, `"targets":`, `"scan_interval":"`+tc.override+`","targets":`, 1)
			}
			service := Service{Config: Config{ReconcileInterval: tc.interval}}
			interval, err := service.scanInterval(instance)
			if err != nil {
				t.Fatal(err)
			}
			if got := scheduledPlanKey(instance, now, interval); got != tc.want {
				t.Fatalf("plan key=%q want %q", got, tc.want)
			}
		})
	}
}

type terminalBetweenCICDProviderAdmissionsStore struct {
	*fakeStore
	firstRunID string
}

func (s *terminalBetweenCICDProviderAdmissionsStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	if s.firstRunID == "" {
		s.firstRunID = run.RunID
		return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	}
	// Model the real store's terminal-run guard after a fast first group.
	if run.RunID == s.firstRunID {
		return workflow.RunAdmission{}, nil
	}
	return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func TestCICDRunMixedProviderAdmissionsSurviveFirstGroupTerminalizing(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := testServiceCICDRunInstance(now)
	instance.Configuration = `{"targets":[
  {"provider":"github_actions","scope_id":"github-actions://eshu-hq/eshu","repository":"eshu-hq/eshu","token_env":"GITHUB_TOKEN","allowed_repositories":["eshu-hq/eshu"],"max_runs":5,"max_jobs":20,"max_artifacts":10},
  {"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/demo","repository":"eshu-hq/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/demo"],"max_runs":1,"max_jobs":10}
 ]}`
	store := &terminalBetweenCICDProviderAdmissionsStore{fakeStore: &fakeStore{}}
	service := Service{Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true}, Store: store, CICDRunPlanner: cicdrun.WorkPlanner{}}
	if err := service.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueuedItems) != 2 || len(store.createdRuns) != 2 {
		t.Fatalf("mixed admissions work=%d runs=%d, want both providers", len(store.enqueuedItems), len(store.createdRuns))
	}
	if store.createdRuns[0].RunID == store.createdRuns[1].RunID {
		t.Fatalf("provider groups share run ID %q", store.createdRuns[0].RunID)
	}
	for _, run := range store.createdRuns {
		if strings.Contains(run.RunID, "github-actions") {
			if !strings.Contains(run.RequestedScopeSet, `"provider":"github_actions"`) || strings.Contains(run.RequestedScopeSet, `"provider":"gitlab_ci"`) {
				t.Fatalf("GitHub run requested scopes=%s", run.RequestedScopeSet)
			}
		} else if strings.Contains(run.RunID, "gitlab-ci") {
			if !strings.Contains(run.RequestedScopeSet, `"provider":"gitlab_ci"`) || strings.Contains(run.RequestedScopeSet, `"provider":"github_actions"`) {
				t.Fatalf("GitLab run requested scopes=%s", run.RequestedScopeSet)
			}
		} else {
			t.Fatalf("run %q lacks stable provider group key", run.RunID)
		}
	}
}

type sharedRunIDGroupedPlanner struct{}

func (sharedRunIDGroupedPlanner) PlanCICDRunWork(ctx context.Context, request cicdrun.PlanRequest) (workflow.Run, []workflow.WorkItem, error) {
	run, items, err := (cicdrun.WorkPlanner{}).PlanCICDRunWork(ctx, request)
	if request.ScheduledProvider != "" {
		run.RunID = "same-provider-run"
	}
	return run, items, err
}

func TestCICDRunMixedProviderRejectsPlannerThatSharesGroupRunID(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := testServiceCICDRunInstance(now)
	instance.Configuration = `{"targets":[
  {"provider":"github_actions","scope_id":"github-actions://eshu-hq/eshu","repository":"eshu-hq/eshu","token_env":"GITHUB_TOKEN","allowed_repositories":["eshu-hq/eshu"],"max_runs":5,"max_jobs":20,"max_artifacts":10},
  {"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/eshu-hq/demo","repository":"eshu-hq/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["eshu-hq/demo"],"max_runs":1,"max_jobs":10}
 ]}`
	service := Service{Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true}, Store: &fakeStore{}, CICDRunPlanner: sharedRunIDGroupedPlanner{}}
	err := service.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{instance})
	if err == nil || !strings.Contains(err.Error(), "share one run id") {
		t.Fatalf("shared provider run ID error=%v", err)
	}
}
