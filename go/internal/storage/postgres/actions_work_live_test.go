// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9146 PostgreSQL 18.6 full-bootstrap proof of production CI/CD planner, guarded workflow admission, exact work lookup after an ACK crash, fenced delivery recovery, and mixed-provider scheduled groups; two focused tests passed in 2.4s using isolated schemas
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/scope"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const cicdActionsLiveDSN = "ESHU_POSTGRES_DSN"

func TestCICDActionsAdmissionBeforeACKCrashUsesExactDurableWorkLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "cicd_actions_handoff", cicdActionsLiveDSN+" is not set")
	workflowStore := NewWorkflowControlStore(SQLDB{DB: database})
	if err := workflowStore.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	actions := actionsstore.NewStore(SQLDB{DB: database})
	if err := actions.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	trigger := webhook.ActionsTrigger{DeliveryID: "crash-delivery", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "123"}
	if _, err := actions.Store(ctx, trigger, now); err != nil {
		t.Fatal(err)
	}
	claimed, err := actions.Claim(ctx, "first", now, 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	instance := workflow.CollectorInstance{
		InstanceID: "actions-instance", CollectorKind: scope.CollectorCICDRun,
		Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration:  `{"targets":[{"provider":"github_actions","scope_id":"github-actions://example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"max_runs":5,"max_jobs":20,"max_artifacts":10}]}`,
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	planner := cicdrun.WorkPlanner{}
	request := cicdrun.PlanRequest{Instance: instance, ObservedAt: now, PlanKey: "webhook-crash", TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{"github-actions://example/repo"}}
	run, items, err := planner.PlanCICDRunWork(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := workflowStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("first admission=%+v err=%v", admission, err)
	}
	if ok, err := workflowStore.HasExactCICDWebhookWork(ctx, items[0]); err != nil || !ok {
		t.Fatalf("exact work=%v err=%v", ok, err)
	}
	// The first coordinator stops before ACK. A second one reaps and replans.
	if count, err := actions.Reap(ctx, now.Add(2*time.Minute), 1); err != nil || count.Requeued != 1 || count.Exhausted != 0 {
		t.Fatalf("reap=%+v err=%v", count, err)
	}
	again, err := actions.Claim(ctx, "second", now.Add(2*time.Minute), 1, time.Minute)
	if err != nil || len(again) != 1 {
		t.Fatalf("reclaim=%+v err=%v", again, err)
	}
	repeatRun, repeatItems, err := planner.PlanCICDRunWork(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if repeatItems[0].WorkItemID != items[0].WorkItemID {
		t.Fatal("delivery plan identity changed")
	}
	admission, err = workflowStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, repeatRun, repeatItems)
	if err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("repeat admission=%+v err=%v", admission, err)
	}
	if ok, err := workflowStore.HasExactCICDWebhookWork(ctx, repeatItems[0]); err != nil || !ok {
		t.Fatalf("recovered exact work=%v err=%v", ok, err)
	}
	if ok, err := actions.Ack(ctx, claimed[0], now.Add(2*time.Minute)); err != nil || ok {
		t.Fatalf("stale ack=%v err=%v", ok, err)
	}
	if ok, err := actions.Ack(ctx, again[0], now.Add(2*time.Minute)); err != nil || !ok {
		t.Fatalf("recovered ack=%v err=%v", ok, err)
	}
}

func TestCICDMixedProviderScheduledGroupsShareDurableRunLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "cicd_mixed_schedule", cicdActionsLiveDSN+" is not set")
	store := NewWorkflowControlStore(SQLDB{DB: database})
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID: "mixed-instance", CollectorKind: scope.CollectorCICDRun, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration:  `{"targets":[{"provider":"github_actions","scope_id":"github-actions://example/repo","repository":"example/repo","token_env":"GITHUB_TOKEN","allowed_repositories":["example/repo"],"max_runs":5,"max_jobs":20,"max_artifacts":10},{"provider":"gitlab_ci","scope_id":"gitlab-ci://gitlab.com/example/demo","repository":"example/demo","token_env":"GITLAB_TOKEN","allowed_repositories":["example/demo"],"max_runs":1,"max_jobs":10}]}`,
		LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	run, items, err := (cicdrun.WorkPlanner{}).PlanCICDRunWork(ctx, cicdrun.PlanRequest{Instance: instance, ObservedAt: now, PlanKey: "continuous-20261026T040000Z"})
	if err != nil || len(items) != 2 {
		t.Fatalf("plan items=%d err=%v", len(items), err)
	}
	for _, item := range items {
		admission, admitErr := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, []workflow.WorkItem{item})
		if admitErr != nil || admission.InsertedWorkItems != 1 {
			t.Fatalf("scope %q admission=%+v err=%v", item.ScopeID, admission, admitErr)
		}
	}
	for _, item := range items {
		if present, checkErr := store.HasExactCICDWebhookWork(ctx, item); checkErr != nil || !present {
			t.Fatalf("scope %q present=%v err=%v", item.ScopeID, present, checkErr)
		}
	}
}
