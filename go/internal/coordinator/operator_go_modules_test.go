// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fakeOperatorRepositoryReader serves one repository per id for the operator
// path and answers the run-exists probe for idempotent re-fires.
type fakeOperatorRepositoryReader struct {
	reads        map[string]gomodules.GoModuleRepositoryRead
	planned      map[string]bool
	listErr      error
	getErr       map[string]error
	noGeneration map[string]bool
	getCalls     []string
}

func (f *fakeOperatorRepositoryReader) ListGoModuleRepositories(
	_ context.Context,
	filter scannercomponent.GoModuleRepositoryFilter,
) (scannercomponent.GoModuleRepositoryPage, error) {
	if f.listErr != nil {
		return scannercomponent.GoModuleRepositoryPage{}, f.listErr
	}
	if f.planned[filter.RunID] {
		return scannercomponent.GoModuleRepositoryPage{AlreadyPlanned: true}, nil
	}
	return scannercomponent.GoModuleRepositoryPage{}, nil
}

func (f *fakeOperatorRepositoryReader) GetGoModuleRepository(
	_ context.Context,
	repositoryID string,
) (gomodules.GoModuleRepositoryRead, error) {
	f.getCalls = append(f.getCalls, repositoryID)
	if err := f.getErr[repositoryID]; err != nil {
		return gomodules.GoModuleRepositoryRead{}, err
	}
	read, ok := f.reads[repositoryID]
	if !ok {
		return gomodules.GoModuleRepositoryRead{}, gomodules.ErrGoModuleRepositoryNotFound
	}
	if f.noGeneration[repositoryID] {
		return gomodules.GoModuleRepositoryRead{}, gomodules.ErrGoModuleRepositoryNoActiveGeneration
	}
	return read, nil
}

// testGoStaleCommit is a commit the fixture repository moved away from: the
// evidence still stamped with it is skewed, so the fence must retract it.
const testGoStaleCommit = "fedcba9876543210fedcba9876543210fedcba98"

func operatorFleetReads() map[string]gomodules.GoModuleRepositoryRead {
	return map[string]gomodules.GoModuleRepositoryRead{
		"repository:r_00000001": {
			Repository: scannercomponent.GoModuleRepository{
				RepositoryID:    "repository:r_00000001",
				RemoteURL:       "https://github.com/eshu-hq/repo00000001",
				SourceCommitSHA: testGoCommit,
				// Stale evidence the fence must retract: a pruned history
				// (zero left-at) fences at once, so the fence-error test has
				// something to fail on and the happy path proves the fence ran.
				HasGoEvidence:        true,
				EvidenceGenerationID: "gen-evidence-00000001",
				EvidenceCommitSHA:    testGoStaleCommit,
			},
			HasGoModule:     true,
			TotalCandidates: 257,
		},
	}
}

func operatorService(
	t *testing.T,
	now time.Time,
	store *fakeStore,
	reader *fakeOperatorRepositoryReader,
	fencer *fakeGoEvidenceFencer,
	logs *bytes.Buffer,
) Service {
	t.Helper()
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour,
		},
		Store:                          store,
		GoModuleRepositoryReader:       reader,
		GoModuleSingleRepositoryReader: reader,
		GoEvidenceFencer:               fencer,
		GovernanceAudit:                &fakeGovernanceAuditAppender{},
		Clock:                          func() time.Time { return now },
	}
	service.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	return service
}

func operatorInstance(now time.Time) workflow.CollectorInstance {
	return goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
}

func operatorRequest() OperatorPlanRequest {
	return OperatorPlanRequest{
		InstanceID:   testGoInstanceID,
		RepositoryID: "repository:r_00000001",
		RequestID:    "10032-fixture-20261123",
		RequestedBy:  "operator-test",
		Reason:       "prove the out-of-rotation trigger",
		ObservedAt:   time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC),
	}
}

// TestOperatorPlanHappyPathProvesOneItemViaPlanAndAdmission is the #10032
// contract: one repository in, one run and one work item out, through the real
// reachability.Plan and the real open-target admission, with the operator run
// id and trigger kind.
func TestOperatorPlanHappyPathProvesOneItemViaPlanAndAdmission(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	fencer := &fakeGoEvidenceFencer{}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, fencer, &logs)

	result, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest())
	if err != nil {
		t.Fatalf("PlanGoReachabilityScopeNow() error = %v", err)
	}
	if result.Outcome != OperatorPlanPlanned {
		t.Fatalf("outcome = %q, want %q (reason %q)", result.Outcome, OperatorPlanPlanned, result.Reason)
	}
	wantPlanKey := "go-modules-operator-10032-fixture-20261123"
	if result.PlanKey != wantPlanKey {
		t.Fatalf("plan key = %q, want %q", result.PlanKey, wantPlanKey)
	}
	wantRunID := "scanner_worker:scanner-go-reachability:operator_recovery:derived:" + wantPlanKey
	if result.RunID != wantRunID {
		t.Fatalf("run id = %q, want %q", result.RunID, wantRunID)
	}
	if len(store.createdRuns) != 1 {
		t.Fatalf("runs = %d, want 1", len(store.createdRuns))
	}
	if got := store.createdRuns[0].TriggerKind; got != workflow.TriggerKindOperatorRecovery {
		t.Fatalf("run trigger = %q, want operator_recovery", got)
	}
	if len(store.enqueuedItems) != 1 {
		t.Fatalf("items = %d, want exactly one", len(store.enqueuedItems))
	}
	item := store.enqueuedItems[0]
	if item.ScopeID != "go-reachability://repository:r_00000001" || item.RunID != wantRunID {
		t.Fatalf("item = %+v, want the fixture scope under %q", item, wantRunID)
	}
	for _, want := range []string{`"outcome":"planned"`, `"plan_key":"` + wantPlanKey + `"`, `"trigger_kind":"operator_recovery"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %s, want %s", logs.String(), want)
		}
	}
}

// TestOperatorPlanDryRunWritesNothing proves a dry run evaluates eligibility
// and the fence decision but touches no store and no fencer.
func TestOperatorPlanDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	fencer := &fakeGoEvidenceFencer{}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, fencer, &logs)

	request := operatorRequest()
	request.DryRun = true
	result, err := service.PlanGoReachabilityScopeNow(context.Background(), request)
	if err != nil {
		t.Fatalf("PlanGoReachabilityScopeNow() error = %v", err)
	}
	if result.Outcome != OperatorPlanDryRun {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OperatorPlanDryRun)
	}
	if !result.WouldPlan {
		t.Fatalf("would-plan = false, want true for the fixture repository")
	}
	if len(store.createdRuns) != 0 || len(store.enqueuedItems) != 0 {
		t.Fatalf("dry run wrote runs=%d items=%d, want zero writes", len(store.createdRuns), len(store.enqueuedItems))
	}
	if fencer.calls != 0 {
		t.Fatalf("dry run fenced %d times, want no fence write", fencer.calls)
	}
	if len(reader.getCalls) != 1 {
		t.Fatalf("repository reads = %d, want exactly the eligibility probe", len(reader.getCalls))
	}
}

// TestOperatorPlanOpenTargetAdmitsNothing proves firing against a scope with
// an open scheduled item creates no run row and reports already_open.
func TestOperatorPlanOpenTargetAdmitsNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	instance := operatorInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	store.enqueuedItems = append(store.enqueuedItems, workflow.WorkItem{
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: testGoInstanceID,
		ScopeID:             "go-reachability://repository:r_00000001",
		AcceptanceUnitID:    "go-reachability://repository:r_00000001",
	})
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, &fakeGoEvidenceFencer{}, &logs)

	result, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest())
	if err != nil {
		t.Fatalf("PlanGoReachabilityScopeNow() error = %v", err)
	}
	if result.Outcome != OperatorPlanAlreadyOpen {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OperatorPlanAlreadyOpen)
	}
	if len(store.createdRuns) != 0 {
		t.Fatalf("runs = %d, want no run row when the target is open", len(store.createdRuns))
	}
}

// TestOperatorPlanSameRequestIDRefireIsANoop proves a re-fire under the same
// request id reports the existing run and writes nothing new: no second run,
// no second admission, no fence write.
func TestOperatorPlanSameRequestIDRefireIsANoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	fencer := &fakeGoEvidenceFencer{}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, fencer, &logs)

	first, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest())
	if err != nil {
		t.Fatalf("first fire error = %v", err)
	}
	if first.Outcome != OperatorPlanPlanned {
		t.Fatalf("first outcome = %q, want planned", first.Outcome)
	}
	reader.planned[first.RunID] = true

	fencer.calls = 0
	second, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest())
	if err != nil {
		t.Fatalf("re-fire error = %v, want the existing run reported", err)
	}
	if second.RunID != first.RunID {
		t.Fatalf("re-fire run = %q, want the existing %q", second.RunID, first.RunID)
	}
	if len(store.createdRuns) != 1 || len(store.enqueuedItems) != 1 {
		t.Fatalf("re-fire wrote runs=%d items=%d, want the first fire's rows only", len(store.createdRuns), len(store.enqueuedItems))
	}
	if fencer.calls != 0 {
		t.Fatalf("re-fire fenced %d times, want an exact no-op", fencer.calls)
	}
}

// TestOperatorPlanRefusesBadRequestIDs proves the plan-key discipline: every
// flag is required, the request id carries the safe charset, and a
// timestamp-only request id is refused because a retry would mint a second
// identity.
func TestOperatorPlanRefusesBadRequestIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	for _, mutate := range []struct {
		name   string
		mutate func(*OperatorPlanRequest)
	}{
		{"missing instance", func(r *OperatorPlanRequest) { r.InstanceID = "" }},
		{"missing repository", func(r *OperatorPlanRequest) { r.RepositoryID = "" }},
		{"missing request id", func(r *OperatorPlanRequest) { r.RequestID = "" }},
		{"missing requested-by", func(r *OperatorPlanRequest) { r.RequestedBy = "" }},
		{"missing reason", func(r *OperatorPlanRequest) { r.Reason = "" }},
		{"unsafe request id", func(r *OperatorPlanRequest) { r.RequestID = "raw/source" }},
		{"timestamp-only request id", func(r *OperatorPlanRequest) { r.RequestID = "20261123T120000Z" }},
	} {
		mutate := mutate
		t.Run(mutate.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
			reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
			var logs bytes.Buffer
			service := operatorService(t, now, store, reader, &fakeGoEvidenceFencer{}, &logs)
			request := operatorRequest()
			mutate.mutate(&request)
			if _, err := service.PlanGoReachabilityScopeNow(context.Background(), request); err == nil {
				t.Fatalf("%s = nil, want a refusal", mutate.name)
			}
			if len(store.createdRuns) != 0 {
				t.Fatalf("%s wrote a run, want nothing", mutate.name)
			}
		})
	}
}

// TestOperatorPlanKeyNeverCollidesWithScheduledKeys pins ruling 3a: the
// operator key shape can never equal a scheduled key for the same instance
// and bucket, so a one-repo fire can never suppress a rotation page.
func TestOperatorPlanKeyNeverCollidesWithScheduledKeys(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, &fakeGoEvidenceFencer{}, &logs)

	result, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest())
	if err != nil {
		t.Fatalf("PlanGoReachabilityScopeNow() error = %v", err)
	}
	if !strings.HasPrefix(result.PlanKey, "go-modules-operator-") {
		t.Fatalf("plan key = %q, want the operator prefix", result.PlanKey)
	}
	scheduled := "go-modules-" + scheduledPlanKey(operatorInstance(now), now, time.Hour)
	if result.PlanKey == scheduled {
		t.Fatalf("operator key %q equals the scheduled key", result.PlanKey)
	}
}
