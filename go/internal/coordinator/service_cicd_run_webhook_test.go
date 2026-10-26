// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type cicdWebhookTestInbox struct {
	claim         webhook.StoredActionsTrigger
	status        webhook.TriggerStatus
	acked         int
	failACKOnce   bool
	released      string
	failed        string
	reapRequeued  int
	reapExhausted int
}

func (f *cicdWebhookTestInbox) Claim(_ context.Context, _ string, at time.Time, _ int, lease time.Duration) ([]webhook.StoredActionsTrigger, error) {
	if f.status != webhook.TriggerStatusQueued {
		return nil, nil
	}
	f.status = webhook.TriggerStatusClaimed
	f.claim.ClaimFencingToken++
	f.claim.ClaimExpiresAt = at.Add(lease)
	return []webhook.StoredActionsTrigger{f.claim}, nil
}

func (f *cicdWebhookTestInbox) Reap(context.Context, time.Time, int) (webhook.ActionsReapResult, error) {
	result := webhook.ActionsReapResult{Requeued: f.reapRequeued, Exhausted: f.reapExhausted}
	f.reapRequeued, f.reapExhausted = 0, 0
	return result, nil
}

func (f *cicdWebhookTestInbox) Ack(_ context.Context, claim webhook.StoredActionsTrigger, at time.Time) (bool, error) {
	if f.failACKOnce {
		f.failACKOnce = false
		return false, errors.New("simulated ACK crash")
	}
	if f.status != webhook.TriggerStatusClaimed || claim.ClaimFencingToken != f.claim.ClaimFencingToken || !at.Before(f.claim.ClaimExpiresAt) {
		return false, nil
	}
	f.status = webhook.TriggerStatusHandedOff
	f.acked++
	return true, nil
}

func (f *cicdWebhookTestInbox) Release(_ context.Context, _ webhook.StoredActionsTrigger, _ time.Time, class string) (bool, error) {
	f.status = webhook.TriggerStatusQueued
	f.released = class
	return true, nil
}

func (f *cicdWebhookTestInbox) Fail(_ context.Context, _ webhook.StoredActionsTrigger, _ time.Time, class string) (bool, error) {
	f.status = webhook.TriggerStatusFailed
	f.failed = class
	return true, nil
}

func (f *cicdWebhookTestInbox) HasPendingRepository(_ context.Context, repository string) (bool, error) {
	return f.claim.Repository == repository && (f.status == webhook.TriggerStatusQueued || f.status == webhook.TriggerStatusClaimed), nil
}

type exactCICDWebhookTestStore struct{ *fakeStore }

func (f exactCICDWebhookTestStore) HasExactCICDWebhookWork(_ context.Context, item workflow.WorkItem) (bool, error) {
	for _, stored := range f.plannedItems {
		if stored.WorkItemID == item.WorkItemID && stored.RunID == item.RunID && stored.CollectorInstanceID == item.CollectorInstanceID && stored.ScopeID == item.ScopeID && stored.GenerationID == item.GenerationID && stored.AcceptanceUnitID == item.AcceptanceUnitID && stored.TenantID == item.TenantID && stored.WorkspaceID == item.WorkspaceID && stored.SubjectClass == item.SubjectClass && stored.PolicyRevisionHash == item.PolicyRevisionHash {
			return true, nil
		}
	}
	return false, nil
}

func cicdWebhookTestService(now time.Time) (Service, *cicdWebhookTestInbox, *fakeStore) {
	instance := testServiceCICDRunInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	inbox := &cicdWebhookTestInbox{
		claim:  webhook.StoredActionsTrigger{ActionsTrigger: webhook.ActionsTrigger{DeliveryID: "delivery-1", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "eshu-hq/eshu", RunID: "123"}, HandoffKey: "batch-v1:delivery-1", ReceivedAt: now},
		status: webhook.TriggerStatusQueued,
	}
	service := Service{
		Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true},
		Store:  exactCICDWebhookTestStore{store}, CICDRunPlanner: cicdrun.WorkPlanner{},
		CICDRunActionsTriggers: inbox, Clock: func() time.Time { return now },
	}
	return service, inbox, store
}

func TestCICDWebhookHandoffAdmitsThenRecoversSameDurableWork(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, store := cicdWebhookTestService(now)
	inbox.failACKOnce = true
	if err := service.runCICDRunWebhookHandoff(t.Context()); err == nil || !strings.Contains(err.Error(), "simulated ACK crash") {
		t.Fatalf("first handoff error=%v", err)
	}
	if inbox.acked != 0 || len(store.enqueuedItems) != 1 {
		t.Fatalf("first handoff ack=%d items=%d", inbox.acked, len(store.enqueuedItems))
	}
	if store.createdRuns[0].TriggerKind != workflow.TriggerKindWebhook {
		t.Fatalf("trigger kind=%q", store.createdRuns[0].TriggerKind)
	}
	// Model an ACK crash after the guarded work insert, then a terminal work row.
	inbox.status = webhook.TriggerStatusQueued
	store.enqueuedItems = nil
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 1 || len(store.createdRuns) != 1 {
		t.Fatalf("recovered handoff ack=%d runs=%d", inbox.acked, len(store.createdRuns))
	}
}

func TestCICDWebhookHandoffRetainsDeliveryBehindUnrelatedOpenTarget(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, store := cicdWebhookTestService(now)
	instance := store.instances[0]
	_, items, err := (cicdrun.WorkPlanner{}).PlanCICDRunWork(t.Context(), cicdrun.PlanRequest{Instance: instance, ObservedAt: now, PlanKey: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	store.enqueuedItems = items
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 0 || inbox.released != "blocked" || inbox.status != webhook.TriggerStatusQueued {
		t.Fatalf("unrelated open ACK=%d release=%q status=%q", inbox.acked, inbox.released, inbox.status)
	}
	if len(store.createdRuns) != 0 {
		t.Fatalf("created %d webhook runs", len(store.createdRuns))
	}
}

func TestCICDWebhookHandoffFailsUnconfiguredTarget(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, _ := cicdWebhookTestService(now)
	inbox.claim.Repository = "unconfigured/repo"
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 0 || inbox.failed != "unauthorized_target" || inbox.status != webhook.TriggerStatusFailed {
		t.Fatalf("unconfigured ACK=%d failure=%q status=%q", inbox.acked, inbox.failed, inbox.status)
	}
}

func TestCICDWebhookHandoffRejectsExpiredACKUsingFreshClock(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, _ := cicdWebhookTestService(now)
	calls := 0
	service.Clock = func() time.Time {
		calls++
		if calls == 1 {
			return now
		}
		return now.Add(2 * time.Minute)
	}
	err := service.runCICDRunWebhookHandoff(t.Context())
	if err == nil || !strings.Contains(err.Error(), "expired before ACK") {
		t.Fatalf("handoff error=%v", err)
	}
	if inbox.acked != 0 {
		t.Fatalf("expired ACK count=%d", inbox.acked)
	}
}

func TestCICDWebhookHandoffRetriesWhileEgressIsDenied(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, _ := cicdWebhookTestService(now)
	service.Config.CollectorEgressPolicy = mustParseCollectorEgressPolicy(t, `{"mode":"restricted"}`)
	service.GovernanceAudit = &fakeGovernanceAuditAppender{}
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 0 || inbox.failed != "" || inbox.released != "no_active_instance" || inbox.status != webhook.TriggerStatusQueued {
		t.Fatalf("egress denied ACK=%d failure=%q release=%q status=%q", inbox.acked, inbox.failed, inbox.released, inbox.status)
	}
}

func TestCICDWebhookHandoffAndFallbackMetricsReflectAdmissions(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("cicd-handoff-proof"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, _ := cicdWebhookTestService(now)
	service.CICDRunInstruments = instruments
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	var warnings strings.Builder
	service.Logger = slog.New(slog.NewTextHandler(&warnings, nil))
	inbox.reapRequeued, inbox.reapExhausted = 1, 1
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Reap accounting must survive a later instance-list failure.
	inbox.reapExhausted = 1
	failedList := Service{Config: service.Config, Store: &fakeStore{listErr: errors.New("late list failure")}, CICDRunActionsTriggers: inbox, CICDRunInstruments: instruments, Clock: service.Clock}
	if err := failedList.runCICDRunWebhookHandoff(t.Context()); err == nil || !strings.Contains(err.Error(), "late list failure") {
		t.Fatalf("list failure after reap=%v", err)
	}
	// A separate configured target with no pending delivery takes scheduled fallback.
	fallback := Service{Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true}, Store: &fakeStore{}, CICDRunPlanner: cicdrun.WorkPlanner{}, CICDRunInstruments: instruments}
	if err := fallback.scheduleCICDRunWork(t.Context(), now, []workflow.CollectorInstance{testServiceCICDRunInstance(now)}); err != nil {
		t.Fatal(err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int64)
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, measured := range scopeMetrics.Metrics {
			if sum, ok := measured.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					seen[measured.Name] += point.Value
					if measured.Name == "eshu_dp_ci_cd_run_webhook_reaps_total" {
						if point.Value <= 0 {
							t.Fatalf("reaper emitted a zero or negative count: %+v", point)
						}
						outcome, ok := point.Attributes.Value(attribute.Key("outcome"))
						if !ok {
							t.Fatal("reaper outcome label is missing")
						}
						seen[measured.Name+"/"+outcome.AsString()] += point.Value
					}
				}
			}
		}
	}
	if seen["eshu_dp_ci_cd_run_webhook_handoffs_total"] != 1 || seen["eshu_dp_ci_cd_run_poll_fallback_total"] != 1 {
		t.Fatalf("admission metrics=%+v", seen)
	}
	if seen["eshu_dp_ci_cd_run_webhook_reaps_total/requeued"] != 1 ||
		seen["eshu_dp_ci_cd_run_webhook_reaps_total/exhausted"] != 2 ||
		!strings.Contains(warnings.String(), "ci_cd_run.actions_reap_exhausted=1") {
		t.Fatalf("reaper metrics=%+v warnings=%q", seen, warnings.String())
	}
}

type batchCICDWebhookInbox struct {
	CICDRunActionsTriggerStore
	claims   []webhook.StoredActionsTrigger
	acked    []string
	released []string
}

func (f *batchCICDWebhookInbox) Reap(context.Context, time.Time, int) (webhook.ActionsReapResult, error) {
	return webhook.ActionsReapResult{}, nil
}

func (f *batchCICDWebhookInbox) Claim(context.Context, string, time.Time, int, time.Duration) ([]webhook.StoredActionsTrigger, error) {
	return f.claims, nil
}

func (f *batchCICDWebhookInbox) Ack(_ context.Context, claim webhook.StoredActionsTrigger, _ time.Time) (bool, error) {
	f.acked = append(f.acked, claim.DeliveryID)
	return true, nil
}

func (f *batchCICDWebhookInbox) Release(_ context.Context, claim webhook.StoredActionsTrigger, _ time.Time, _ string) (bool, error) {
	f.released = append(f.released, claim.DeliveryID)
	return true, nil
}

type onceFailedCICDWebhookStore struct {
	exactCICDWebhookTestStore
	failed bool
}

func (f *onceFailedCICDWebhookStore) CreateRunWithWorkItemsIfNoOpenTargets(ctx context.Context, run workflow.Run, items []workflow.WorkItem) (workflow.RunAdmission, error) {
	if !f.failed {
		f.failed = true
		return workflow.RunAdmission{}, errors.New("transient admission error")
	}
	return f.exactCICDWebhookTestStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func TestCICDWebhookHandoffContinuesBatchAfterOneAdmissionError(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, _, store := cicdWebhookTestService(now)
	first := webhook.StoredActionsTrigger{ActionsTrigger: webhook.ActionsTrigger{DeliveryID: "first", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "eshu-hq/eshu", RunID: "123"}, HandoffKey: "batch-v1:first", ReceivedAt: now, ClaimFencingToken: 1, ClaimExpiresAt: now.Add(time.Minute)}
	second := first
	second.DeliveryID = "second"
	second.HandoffKey = "batch-v1:second"
	inbox := &batchCICDWebhookInbox{claims: []webhook.StoredActionsTrigger{first, second}}
	service.Store = &onceFailedCICDWebhookStore{exactCICDWebhookTestStore: exactCICDWebhookTestStore{store}}
	service.CICDRunActionsTriggers = inbox
	err := service.runCICDRunWebhookHandoff(t.Context())
	if err == nil || !strings.Contains(err.Error(), "transient admission error") {
		t.Fatalf("handoff error=%v", err)
	}
	if len(inbox.released) != 1 || inbox.released[0] != "first" || len(inbox.acked) != 1 || inbox.acked[0] != "second" {
		t.Fatalf("released=%v acked=%v", inbox.released, inbox.acked)
	}
}

func TestCICDWebhookHandoffCoalescesClaimedRepositoryBatch(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, _, store := cicdWebhookTestService(now)
	first := webhook.StoredActionsTrigger{
		ActionsTrigger: webhook.ActionsTrigger{DeliveryID: "first", EventKind: "workflow_job", Action: "completed", RepositoryID: "42", Repository: "eshu-hq/eshu", RunID: "123", JobID: "1"},
		HandoffKey:     "batch-v1:first", ReceivedAt: now, ClaimFencingToken: 1, ClaimExpiresAt: now.Add(time.Minute),
	}
	claims := make([]webhook.StoredActionsTrigger, 4)
	for index := range claims {
		claims[index] = first
		claims[index].DeliveryID = fmt.Sprintf("delivery-%d", index)
		claims[index].JobID = fmt.Sprint(index + 1)
	}
	inbox := &batchCICDWebhookInbox{claims: claims}
	service.CICDRunActionsTriggers = inbox
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueuedItems) != 1 || len(inbox.acked) != len(claims) || len(inbox.released) != 0 {
		t.Fatalf("planned=%d ACKed=%v released=%v", len(store.enqueuedItems), inbox.acked, inbox.released)
	}
	if got := store.createdRuns[0].RunID; !strings.Contains(got, "webhook-group-v1-") {
		t.Fatalf("batch run ID %q shares the legacy per-delivery namespace", got)
	}
}

type terminalCICDWebhookTestStore struct {
	exactCICDWebhookTestStore
	terminalRuns map[string]bool
}

func (f *terminalCICDWebhookTestStore) CreateRunWithWorkItemsIfNoOpenTargets(ctx context.Context, run workflow.Run, items []workflow.WorkItem) (workflow.RunAdmission, error) {
	if f.terminalRuns[run.RunID] {
		return workflow.RunAdmission{}, nil
	}
	return f.exactCICDWebhookTestStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func TestCICDWebhookHandoffRecoversMissingScopeAfterEarlierRunTerminal(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, store := cicdWebhookTestService(now)
	instance := store.instances[0]
	instance.Configuration = strings.Replace(instance.Configuration, `"max_artifacts": 10`, `"max_artifacts": 10
		},{
			"provider": "github_actions",
			"scope_id": "github-actions://eshu-hq/eshu-secondary",
			"repository": "eshu-hq/eshu",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["eshu-hq/eshu"],
			"max_runs": 5,
			"max_jobs": 20,
			"max_artifacts": 10`, 1)
	store.instances[0] = instance
	blockedScope := "github-actions://eshu-hq/eshu-secondary"
	_, unrelated, err := (cicdrun.WorkPlanner{}).PlanCICDRunWork(t.Context(), cicdrun.PlanRequest{
		Instance: instance, ObservedAt: now, PlanKey: "unrelated", TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{blockedScope},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.enqueuedItems = unrelated
	terminal := &terminalCICDWebhookTestStore{exactCICDWebhookTestStore: exactCICDWebhookTestStore{store}, terminalRuns: make(map[string]bool)}
	service.Store = terminal
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.released != "blocked" || inbox.acked != 0 || len(store.createdRuns) != 1 {
		t.Fatalf("partial admission released=%q ACK=%d runs=%d", inbox.released, inbox.acked, len(store.createdRuns))
	}
	terminal.terminalRuns[store.createdRuns[0].RunID] = true
	store.enqueuedItems = nil // both the unrelated work and first scope have finished
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 1 || len(store.createdRuns) != 2 || len(store.plannedItems) != 2 {
		t.Fatalf("recovered ACK=%d runs=%d exactItems=%d", inbox.acked, len(store.createdRuns), len(store.plannedItems))
	}
	if store.createdRuns[0].RunID == store.createdRuns[1].RunID {
		t.Fatalf("independent scopes share run %q", store.createdRuns[0].RunID)
	}
}

func TestCICDWebhookHandoffRejectsMissingPersistedGroupKey(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, store := cicdWebhookTestService(now)
	inbox.claim.HandoffKey = ""
	if err := service.runCICDRunWebhookHandoff(t.Context()); err == nil || !strings.Contains(err.Error(), "no valid handoff key") {
		t.Fatalf("missing group key error=%v", err)
	}
	if inbox.acked != 0 || len(store.enqueuedItems) != 0 || inbox.released != "admission_failed" {
		t.Fatalf("missing key ACK=%d work=%d release=%q", inbox.acked, len(store.enqueuedItems), inbox.released)
	}
}

type expiringBatchCICDWebhookInbox struct{ batchCICDWebhookInbox }

func (f *expiringBatchCICDWebhookInbox) Ack(_ context.Context, claim webhook.StoredActionsTrigger, at time.Time) (bool, error) {
	if !at.Before(claim.ClaimExpiresAt) {
		return false, nil
	}
	f.acked = append(f.acked, claim.DeliveryID)
	return true, nil
}

func TestCICDWebhookHandoffChecksFreshLeaseTimeForEachBatchACK(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, _, _ := cicdWebhookTestService(now)
	first := webhook.StoredActionsTrigger{
		ActionsTrigger: webhook.ActionsTrigger{DeliveryID: "first", EventKind: "workflow_run", Action: "completed", RepositoryID: "42", Repository: "eshu-hq/eshu", RunID: "123"},
		HandoffKey:     "batch-v1:first", ReceivedAt: now, ClaimFencingToken: 1, ClaimExpiresAt: now.Add(time.Minute),
	}
	second := first
	second.DeliveryID = "second"
	inbox := &expiringBatchCICDWebhookInbox{batchCICDWebhookInbox{claims: []webhook.StoredActionsTrigger{first, second}}}
	service.CICDRunActionsTriggers = inbox
	clockCalls := 0
	service.Clock = func() time.Time {
		clockCalls++
		switch clockCalls {
		case 1:
			return now
		case 2:
			return now.Add(10 * time.Second)
		default:
			return now.Add(2 * time.Minute)
		}
	}
	err := service.runCICDRunWebhookHandoff(t.Context())
	if err == nil || !strings.Contains(err.Error(), "expired before ACK") {
		t.Fatalf("late second ACK error=%v", err)
	}
	if len(inbox.acked) != 1 || inbox.acked[0] != "first" {
		t.Fatalf("ACKed deliveries=%v", inbox.acked)
	}
}

func TestCICDWebhookHandoffSeparatesTenantBoundaryIdentities(t *testing.T) {
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	service, inbox, store := cicdWebhookTestService(now)
	service.TenantGrantReader = &fakeTenantGrantReader{grants: []WorkflowTenantScopeGrant{{
		ScopeID: "github-actions://eshu-hq/eshu", PolicyRevisionHash: "policy-1",
	}}}
	boundary := WorkflowTenantBoundary{
		TenantID: "tenant-a", WorkspaceID: "workspace-1", SubjectClass: "collector", PolicyRevisionHash: "policy-1",
	}
	service.Config.TenantBoundary = boundary
	inbox.failACKOnce = true
	if err := service.runCICDRunWebhookHandoff(t.Context()); err == nil || !strings.Contains(err.Error(), "simulated ACK crash") {
		t.Fatalf("first tenant ACK crash error=%v", err)
	}
	if inbox.acked != 0 || len(store.createdRuns) != 1 {
		t.Fatalf("first tenant ACK=%d runs=%d", inbox.acked, len(store.createdRuns))
	}
	inbox.status = webhook.TriggerStatusQueued // expired claim recovered after the ACK crash
	store.enqueuedItems = nil                  // the first tenant's collection completed
	boundary.TenantID = "tenant-b"
	service.Config.TenantBoundary = boundary
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	if inbox.acked != 1 || len(store.createdRuns) != 2 {
		t.Fatalf("second tenant ACK=%d runs=%d", inbox.acked, len(store.createdRuns))
	}
	if store.createdRuns[0].RunID == store.createdRuns[1].RunID || store.plannedItems[0].WorkItemID == store.plannedItems[1].WorkItemID {
		t.Fatalf("tenant boundaries share run/work identity: %+v", store.createdRuns)
	}
}
