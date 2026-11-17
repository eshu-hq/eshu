// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const baselinePlanTestRun = "baseline-2026-11"

var baselinePlanTestStart = time.UnixMilli(1_700_000_000_000).UTC()

// baselinePlanCall is one admission the plan-carrying store entry point saw.
type baselinePlanCall struct {
	items   []workflow.WorkItem
	plans   []workflow.NVDBaselinePlan
	advance *workflow.OwnedTargetSlotAdvance
}

// baselinePlanFakeStore is a Store that also records NVD baseline plans
// (#10828). It counts the plain admission separately, so a test can tell which
// entry point the coordinator chose, and reports recorded plans the way the
// real store does: all of them when recorded is negative.
type baselinePlanFakeStore struct {
	*fakeStore
	calls      []baselinePlanCall
	plainCalls int
	recorded   int
}

func (s *baselinePlanFakeStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	s.plainCalls++
	return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func (s *baselinePlanFakeStore) CreateRunWithWorkItemsRecordingBaselinePlans(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
	plans []workflow.NVDBaselinePlan,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.RunAdmission, workflow.SlotAdvanceResult, error) {
	s.calls = append(s.calls, baselinePlanCall{items: items, plans: plans, advance: advance})
	admission, err := s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	admission.BaselinePlansRecorded = s.recorded
	if s.recorded < 0 {
		admission.BaselinePlansRecorded = len(plans)
	}
	var moved workflow.SlotAdvanceResult
	if advance != nil {
		moved = workflow.SlotAdvanceResult{Requested: len(advance.Slots), Moved: len(advance.Slots)}
	}
	return admission, moved, err
}

func baselinePlanInstance() workflow.CollectorInstance { return replanJoinInstance() }

func baselinePlanRun(now time.Time) workflow.Run { return replanJoinRun(now) }

func baselinePlanItem(instanceID, scopeID string, now time.Time) workflow.WorkItem {
	generationID := "vulnerability_intelligence:" + scopeID
	return workflow.WorkItem{
		WorkItemID:          "vulnerability_intelligence:" + instanceID + ":" + scopeID,
		RunID:               "run-replan-join",
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorVulnerabilityIntelligence),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func baselineScope(year int) string {
	return vulnsource.NVDBaselineScopeID(baselinePlanTestRun, year, baselinePlanTestStart, "")
}

const baselinePlanOSVScope = "vuln-intel://osv/npm/left-pad"

// TestCreateWorkflowWorkRecordsBaselinePlansThroughTheStore pins the shared
// admission step: baseline chunks among the items are handed to the store as
// plans, in item order, with the run and year the scope id carries, and the
// whole admission (the baseline chunks and the ordinary target) goes through the
// one plan-carrying call, not the plain one.
func TestCreateWorkflowWorkRecordsBaselinePlansThroughTheStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}
	service := Service{Store: store}
	instance := baselinePlanInstance()
	items := []workflow.WorkItem{
		baselinePlanItem(instance.InstanceID, baselineScope(2026), now),
		baselinePlanItem(instance.InstanceID, baselinePlanOSVScope, now),
		baselinePlanItem(instance.InstanceID, baselineScope(2025), now),
	}

	admitted, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now), items, nil)
	if err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	if store.plainCalls != 0 || len(store.calls) != 1 {
		t.Fatalf("plain calls = %d, plan-carrying calls = %d, want the one plan-carrying call", store.plainCalls, len(store.calls))
	}
	call := store.calls[0]
	want := []workflow.NVDBaselinePlan{
		{BaselineRunID: baselinePlanTestRun, Year: 2026, ScopeID: baselineScope(2026)},
		{BaselineRunID: baselinePlanTestRun, Year: 2025, ScopeID: baselineScope(2025)},
	}
	if len(call.plans) != len(want) || call.plans[0] != want[0] || call.plans[1] != want[1] {
		t.Fatalf("plans = %+v, want %+v", call.plans, want)
	}
	if len(call.items) != 3 || call.advance != nil {
		t.Fatalf("items = %d advance = %v, want all 3 items and no slot advance", len(call.items), call.advance)
	}
	if admitted.inserted != 3 {
		t.Fatalf("inserted = %d, want all 3 work items admitted through the plan-carrying call", admitted.inserted)
	}
}

// TestCreateWorkflowWorkWithoutBaselineChunksKeepsThePlainPath proves an
// ordinary admission is untouched: no baseline chunk, no plan, the plain store
// call, even against a store that can record plans.
func TestCreateWorkflowWorkWithoutBaselineChunksKeepsThePlainPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}
	service := Service{Store: store}
	instance := baselinePlanInstance()

	_, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now),
		[]workflow.WorkItem{baselinePlanItem(instance.InstanceID, baselinePlanOSVScope, now)}, nil)
	if err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	if store.plainCalls != 1 || len(store.calls) != 0 {
		t.Fatalf("plain calls = %d, plan-carrying calls = %d, want the plain path only", store.plainCalls, len(store.calls))
	}
}

// TestCreateWorkflowWorkRefusesBaselineChunksAgainstAStoreThatCannotRecordThem
// is the fail-closed half: a baseline chunk admitted without its plan row would
// leave the planner nothing to pace from and mint a chunk every tick, so a store
// without the plan write refuses the admission and nothing is enqueued.
func TestCreateWorkflowWorkRefusesBaselineChunksAgainstAStoreThatCannotRecordThem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	service := Service{Store: store}
	instance := baselinePlanInstance()

	_, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now),
		[]workflow.WorkItem{baselinePlanItem(instance.InstanceID, baselineScope(2026), now)}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot record NVD baseline plans") {
		t.Fatalf("error = %v, want the store-cannot-record-plans refusal", err)
	}
	if len(store.enqueuedItems) != 0 {
		t.Fatalf("enqueued %d items despite the refusal, want none", len(store.enqueuedItems))
	}
}

// TestCreateWorkflowWorkSendsOnlyAuthorizedBaselinePlans proves a chunk a tenant
// grant denied never reaches the store, so it never gets a plan row: the plans
// are derived from the authorized items, not the planned ones.
func TestCreateWorkflowWorkSendsOnlyAuthorizedBaselinePlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}
	service := Service{
		Config: Config{TenantBoundary: replanJoinTenantBoundary()},
		Store:  store,
		TenantGrantReader: &fakeTenantGrantReader{grants: []WorkflowTenantScopeGrant{
			{ScopeID: baselineScope(2025), PolicyRevisionHash: "policy-a"},
			{ScopeID: baselinePlanOSVScope, PolicyRevisionHash: "policy-a"},
		}},
	}
	instance := baselinePlanInstance()
	items := []workflow.WorkItem{
		baselinePlanItem(instance.InstanceID, baselineScope(2026), now),
		baselinePlanItem(instance.InstanceID, baselineScope(2025), now),
		baselinePlanItem(instance.InstanceID, baselinePlanOSVScope, now),
	}

	if _, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now), items, nil); err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	if len(store.calls) != 1 || len(store.calls[0].plans) != 1 || store.calls[0].plans[0].Year != 2025 {
		t.Fatalf("calls = %+v, want one call carrying only the granted 2025 plan", store.calls)
	}
	for _, item := range store.calls[0].items {
		if item.ScopeID == baselineScope(2026) {
			t.Fatal("the denied 2026 chunk reached the store")
		}
	}
}

// TestCreateWorkflowWorkKeepsTheSlotAdvanceWithBaselinePlans covers an
// owned-package pick and a baseline chunk planned in one tick: both ride the one
// plan-carrying call, so the slot move and the plan row share a transaction.
func TestCreateWorkflowWorkKeepsTheSlotAdvanceWithBaselinePlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}
	service := Service{Store: store}
	instance := baselinePlanInstance()
	advance := &workflow.OwnedTargetSlotAdvance{
		CollectorKind:       instance.CollectorKind,
		CollectorInstanceID: instance.InstanceID,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		WindowStartUnix:     1_700_000_000,
		PlannedAt:           now,
		Slots:               []int{2, 5},
	}

	admitted, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now),
		[]workflow.WorkItem{baselinePlanItem(instance.InstanceID, baselineScope(2026), now)}, advance)
	if err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	if len(store.calls) != 1 || store.calls[0].advance != advance {
		t.Fatalf("calls = %+v, want the advance carried through the plan-carrying call", store.calls)
	}
	if admitted.advance.Requested != 2 || admitted.advance.Moved != 2 {
		t.Fatalf("advance result = %+v, want the store's result returned", admitted.advance)
	}
}

// TestCreateWorkflowWorkIfNoOpenTargetsAlsoRecordsBaselinePlans proves the
// derivation lives in the shared admission step and not at one call site: the
// satellite wrapper reaches it too.
func TestCreateWorkflowWorkIfNoOpenTargetsAlsoRecordsBaselinePlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}
	service := Service{Store: store}
	instance := baselinePlanInstance()

	inserted, err := service.createWorkflowWorkIfNoOpenTargets(context.Background(), instance, baselinePlanRun(now),
		[]workflow.WorkItem{baselinePlanItem(instance.InstanceID, baselineScope(2026), now)})
	if err != nil || inserted != 1 {
		t.Fatalf("inserted = %d error = %v, want one admitted chunk", inserted, err)
	}
	if len(store.calls) != 1 || len(store.calls[0].plans) != 1 {
		t.Fatalf("calls = %+v, want the wrapper to record the plan", store.calls)
	}
}

// TestServiceRunRecordsBaselinePlansForTheInTreeInstance drives the in-tree
// vulnerability instance end to end: a planner that mints a baseline chunk, the
// service tick, and the store seeing the plan beside the work item.
func TestServiceRunRecordsBaselinePlansForTheInTreeInstance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 30, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	run := workflow.Run{
		RunID:              "vulnerability_intelligence:" + instance.InstanceID + ":schedule:continuous-20261117T090000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := baselinePlanItem(instance.InstanceID, baselineScope(2026), now)
	item.RunID = run.RunID
	planner := &fakeVulnerabilityIntelligencePlanner{run: run, items: []workflow.WorkItem{item}}
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}, recorded: -1}
	service := Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
		Store:                            store,
		VulnerabilityIntelligencePlanner: planner,
		Clock:                            func() time.Time { return now },
	}

	runOneReconcile(t, service)

	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1", len(planner.requests))
	}
	if len(store.calls) != 1 || len(store.calls[0].plans) != 1 || store.calls[0].plans[0].ScopeID != item.ScopeID {
		t.Fatalf("calls = %+v, want the in-tree tick to hand the baseline plan to the store", store.calls)
	}
	if len(store.enqueuedItems) != 1 {
		t.Fatalf("enqueued items = %d, want the chunk admitted with its plan", len(store.enqueuedItems))
	}
}

// TestBothAdmissionSitesShareOneAdmissionFunction is the structural guard for
// "one shared function for the in-tree and activation sites": the in-tree
// instance and a vulnerability activation both admit through createWorkflowWork,
// and neither calls a store admission method itself, so a plan derived there
// cannot be skipped at one site.
func TestBothAdmissionSitesShareOneAdmissionFunction(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"vulnerability_intelligence_service.go", "component_extension_service.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(source)
		if !strings.Contains(text, "s.createWorkflowWork(") {
			t.Errorf("%s no longer admits through createWorkflowWork", file)
		}
		for _, banned := range []string{
			".CreateRunWithWorkItemsIfNoOpenTargets(",
			".CreateRunWithWorkItemsAdvancingSlotProgress(",
			".CreateRunWithWorkItemsRecordingBaselinePlans(",
		} {
			if strings.Contains(text, banned) {
				t.Errorf("%s calls the store directly with %s, skipping the shared admission step", file, banned)
			}
		}
	}
}

// TestCreateWorkflowWorkLogsTheBaselinePlanOutcome gives an operator the
// admission outcome without a database: how many baseline plans the tick
// carried, how many landed, and how many the guard or the insert dropped. A tick
// with no baseline chunk logs nothing extra.
func TestCreateWorkflowWorkLogsTheBaselinePlanOutcome(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	instance := baselinePlanInstance()

	var logs bytes.Buffer
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: 1}
	service := Service{Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	items := []workflow.WorkItem{
		baselinePlanItem(instance.InstanceID, baselineScope(2026), now),
		baselinePlanItem(instance.InstanceID, baselineScope(2025), now),
	}
	if _, err := service.createWorkflowWork(context.Background(), instance, baselinePlanRun(now), items, nil); err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	for _, field := range []string{
		`"msg":"workflow coordinator admitted NVD baseline chunks"`,
		`"baseline_plans":2`,
		`"baseline_plans_recorded":1`,
		`"baseline_plans_not_inserted":1`,
	} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("log %s is missing %s", logs.String(), field)
		}
	}

	logs.Reset()
	quiet := Service{Store: &baselinePlanFakeStore{fakeStore: &fakeStore{}, recorded: -1}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	if _, err := quiet.createWorkflowWork(context.Background(), instance, baselinePlanRun(now),
		[]workflow.WorkItem{baselinePlanItem(instance.InstanceID, baselinePlanOSVScope, now)}, nil); err != nil {
		t.Fatalf("createWorkflowWork() error = %v", err)
	}
	if strings.Contains(logs.String(), "NVD baseline chunks") {
		t.Fatalf("log %s mentions baseline chunks for an admission with none", logs.String())
	}
}
