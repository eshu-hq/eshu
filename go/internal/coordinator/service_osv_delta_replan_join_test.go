// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// flapTenantGrantReader grants every queried scope on its first
// ListWorkflowScopeGrants call and denies everything after, modelling a
// grant flap between the admission authorization read and the commit
// authorization re-read (#10083).
type flapTenantGrantReader struct {
	calls   int
	queries []WorkflowTenantGrantQuery
}

func (f *flapTenantGrantReader) ListWorkflowScopeGrants(
	_ context.Context,
	query WorkflowTenantGrantQuery,
) ([]WorkflowTenantScopeGrant, error) {
	query.ScopeIDs = append([]string(nil), query.ScopeIDs...)
	f.queries = append(f.queries, query)
	f.calls++
	if f.calls > 1 {
		return nil, nil
	}
	grants := make([]WorkflowTenantScopeGrant, 0, len(query.ScopeIDs))
	for _, id := range query.ScopeIDs {
		grants = append(grants, WorkflowTenantScopeGrant{ScopeID: id, PolicyRevisionHash: "policy-a"})
	}
	return grants, nil
}

func replanJoinTenantBoundary() WorkflowTenantBoundary {
	return WorkflowTenantBoundary{
		TenantID:           "tenant-a",
		WorkspaceID:        "workspace-a",
		SubjectClass:       "collector",
		PolicyRevisionHash: "policy-a",
	}
}

func replanJoinInstance() workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "collector-vuln-intel",
		CollectorKind: scope.CollectorVulnerabilityIntelligence,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
	}
}

func replanJoinRun(now time.Time) workflow.Run {
	return workflow.Run{
		RunID:              "run-replan-join",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func replanJoinWorkItem(now time.Time, scopeID string) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "item-replan-join",
		RunID:               "run-replan-join",
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: "collector-vuln-intel",
		SourceSystem:        string(scope.CollectorVulnerabilityIntelligence),
		ScopeID:             scopeID,
		AcceptanceUnitID:    `{"service_kind":"osv"}`,
		SourceRunID:         "source-run-replan-join",
		GenerationID:        "generation-replan-join",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

// TestCommitOSVDeltaReplanSingleAuthorizationDecisionPoint pins the #10083
// join: the commit must honor the admission's authorization decision instead
// of re-reading grants. The flap reader allows the _replan scope at admission
// and denies it at any later read, so double-authz code admits the work and
// then refuses to commit its row, while single-decision-point code commits.
func TestCommitOSVDeltaReplanSingleAuthorizationDecisionPoint(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	now := feed.Add(30 * time.Minute)
	replanScope := "vuln-intel://osv/npm/_replan/7/abc123"

	store := &fakeStore{}
	grants := &flapTenantGrantReader{}
	service := Service{
		Config:            Config{TenantBoundary: replanJoinTenantBoundary()},
		Store:             store,
		TenantGrantReader: grants,
		Metrics:           &fakeMetrics{},
	}
	run := replanJoinRun(now)
	items := []workflow.WorkItem{replanJoinWorkItem(now, replanScope)}
	admitted, err := service.createWorkflowWork(context.Background(), replanJoinInstance(), run, items, nil)
	if err != nil {
		t.Fatalf("createWorkflowWork() error = %v, want nil", err)
	}
	if admitted.inserted != 1 {
		t.Fatalf("admitted.inserted = %d, want 1 (the flap allows the admission read)", admitted.inserted)
	}

	replanStore := &memoryOSVDeltaReplanStore{rows: map[int64]vulnerability.OSVDeltaQueuedRow{
		1: {RowID: 1, AdvisoryID: "GHSA-j", Ecosystem: "npm", PackageName: "left-pad", FeedTimestamp: feed},
	}}
	pass := &osvDeltaPass{
		store:      replanStore,
		instanceID: "vuln-intel-replan",
		pending: []vulnerability.OSVDeltaValidatedChange{
			{RowID: 1, AdvisoryID: "GHSA-j", Ecosystem: "npm", PackageName: "left-pad", FeedTimestamp: feed},
		},
	}
	observation := vulnerability.OSVDeltaReplanObservation{
		Enabled:          true,
		CoveredRowIDs:    []int64{1},
		CoveredRowScopes: map[int64][]string{1: {replanScope}},
		Watermark:        feed,
		WatermarkSet:     true,
	}
	if err := service.commitOSVDeltaReplan(context.Background(), pass, run, items, observation, observedAt, admitted.authorizedReplanScopes); err != nil {
		t.Fatalf("commitOSVDeltaReplan() error = %v, want nil", err)
	}
	if _, ok := replanStore.rows[1]; ok {
		t.Fatal("row 1 still pending: admission authorized its scope, so the commit must honor that decision")
	}
	if grants.calls != 1 {
		t.Fatalf("grant reads = %d, want 1 (the commit must reuse the admission decision, not re-read grants)", grants.calls)
	}
}

// TestCommitOSVDeltaReplanHoldsDeniedReplanScopes pins the denied half of the
// #10083 join: with no grant for the _replan scope the admission reports
// no-work with a nil authorized set, and the commit keeps the mapped row
// pending for the next pass.
func TestCommitOSVDeltaReplanHoldsDeniedReplanScopes(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	now := feed.Add(30 * time.Minute)
	replanScope := "vuln-intel://osv/npm/_replan/7/denied456"

	store := &fakeStore{}
	service := Service{
		Config:            Config{TenantBoundary: replanJoinTenantBoundary()},
		Store:             store,
		TenantGrantReader: &fakeTenantGrantReader{},
		Metrics:           &fakeMetrics{},
	}
	run := replanJoinRun(now)
	items := []workflow.WorkItem{replanJoinWorkItem(now, replanScope)}
	admitted, err := service.createWorkflowWork(context.Background(), replanJoinInstance(), run, items, nil)
	if err != nil {
		t.Fatalf("createWorkflowWork() error = %v, want nil", err)
	}
	if !admitted.noWork {
		t.Fatalf("admitted = %+v, want no-work (the grant reader holds no grant)", admitted)
	}
	if admitted.authorizedReplanScopes != nil {
		t.Fatalf("authorized set = %v, want nil on the no-work path", admitted.authorizedReplanScopes)
	}
	if got := len(store.enqueuedItems); got != 0 {
		t.Fatalf("enqueued items = %d, want 0 (denied work never reaches the store)", got)
	}

	replanStore := &memoryOSVDeltaReplanStore{rows: map[int64]vulnerability.OSVDeltaQueuedRow{
		1: {RowID: 1, AdvisoryID: "GHSA-d", Ecosystem: "npm", PackageName: "held-back", FeedTimestamp: feed},
	}}
	pass := &osvDeltaPass{
		store:      replanStore,
		instanceID: "vuln-intel-replan",
		pending: []vulnerability.OSVDeltaValidatedChange{
			{RowID: 1, AdvisoryID: "GHSA-d", Ecosystem: "npm", PackageName: "held-back", FeedTimestamp: feed},
		},
	}
	observation := vulnerability.OSVDeltaReplanObservation{
		Enabled:          true,
		CoveredRowIDs:    []int64{1},
		CoveredRowScopes: map[int64][]string{1: {replanScope}},
		Watermark:        feed,
		WatermarkSet:     true,
	}
	if err := service.commitOSVDeltaReplan(context.Background(), pass, run, items, observation, observedAt, admitted.authorizedReplanScopes); err != nil {
		t.Fatalf("commitOSVDeltaReplan() error = %v, want nil", err)
	}
	if _, ok := replanStore.rows[1]; !ok {
		t.Fatal("row 1 committed on a denied scope: a denied mapped row must stay pending")
	}
	if replanStore.polls != 1 {
		t.Fatalf("poll records = %d, want 1 (a held pass still records its poll)", replanStore.polls)
	}
}

// TestCommitOSVDeltaReplanDuplicateDroppedScopeStillCommits pins the deferred
// half of #10083, NOT a fix for it. TODO(eshu-hq/eshu#10149): the threaded
// set is the authorized set, which the open-target guard cannot shrink, so a
// row whose _replan scope the guard dropped as a duplicate still commits.
// The second admission under the same run id models the repeat tick: the
// guard drops the item (inserted 0) while the authorized set still holds its
// scope. Change this test only when #10149 lands per-item admission outcomes.
func TestCommitOSVDeltaReplanDuplicateDroppedScopeStillCommits(t *testing.T) {
	t.Parallel()

	feed := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	observedAt := feed.Add(time.Hour)
	now := feed.Add(30 * time.Minute)
	replanScope := "vuln-intel://osv/npm/_replan/7/dup789"

	store := &fakeStore{}
	service := Service{
		Store:   store,
		Metrics: &fakeMetrics{},
	}
	run := replanJoinRun(now)
	items := []workflow.WorkItem{replanJoinWorkItem(now, replanScope)}
	first, err := service.createWorkflowWork(context.Background(), replanJoinInstance(), run, items, nil)
	if err != nil {
		t.Fatalf("first createWorkflowWork() error = %v, want nil", err)
	}
	if first.inserted != 1 {
		t.Fatalf("first admitted.inserted = %d, want 1", first.inserted)
	}
	// TODO(eshu-hq/eshu#10149): the repeat tick's guard drop is invisible
	// to the commit, so the row below still drains. Per-item admission
	// outcomes would hold it pending instead.
	repeat, err := service.createWorkflowWork(context.Background(), replanJoinInstance(), run, items, nil)
	if err != nil {
		t.Fatalf("repeat createWorkflowWork() error = %v, want nil", err)
	}
	if repeat.inserted != 0 {
		t.Fatalf("repeat admitted.inserted = %d, want 0 (the open-target guard drops the duplicate)", repeat.inserted)
	}
	if _, ok := repeat.authorizedReplanScopes[replanScope]; !ok {
		t.Fatalf("repeat authorized set = %v, want it to still hold %q (authorized, not inserted)", repeat.authorizedReplanScopes, replanScope)
	}

	replanStore := &memoryOSVDeltaReplanStore{rows: map[int64]vulnerability.OSVDeltaQueuedRow{
		1: {RowID: 1, AdvisoryID: "GHSA-u", Ecosystem: "npm", PackageName: "dup-pkg", FeedTimestamp: feed},
	}}
	pass := &osvDeltaPass{
		store:      replanStore,
		instanceID: "vuln-intel-replan",
		pending: []vulnerability.OSVDeltaValidatedChange{
			{RowID: 1, AdvisoryID: "GHSA-u", Ecosystem: "npm", PackageName: "dup-pkg", FeedTimestamp: feed},
		},
	}
	observation := vulnerability.OSVDeltaReplanObservation{
		Enabled:          true,
		CoveredRowIDs:    []int64{1},
		CoveredRowScopes: map[int64][]string{1: {replanScope}},
		Watermark:        feed,
		WatermarkSet:     true,
	}
	if err := service.commitOSVDeltaReplan(context.Background(), pass, run, items, observation, observedAt, repeat.authorizedReplanScopes); err != nil {
		t.Fatalf("commitOSVDeltaReplan() error = %v, want nil", err)
	}
	if _, ok := replanStore.rows[1]; ok {
		t.Fatal("row 1 still pending: the deferred duplicate-dropped half still commits today (TODO eshu-hq/eshu#10149)")
	}
}
