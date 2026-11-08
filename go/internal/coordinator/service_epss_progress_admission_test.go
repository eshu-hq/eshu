// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// epssAdmissionOpenWorkItem returns a pending work item whose guard identity
// matches a planned vulnerability-intelligence item for the scope: the
// open-target guard compares collector, instance, scope, tenant, workspace,
// subject class, policy hash, and acceptance unit, and the planner leaves
// tenant, workspace, subject class, and policy hash empty while setting the
// acceptance unit to the scope id.
func epssAdmissionOpenWorkItem(instanceID, scopeID string) workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: instanceID,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		Status:              workflow.WorkItemStatusPending,
	}
}

// epssAdmissionFindingCVEIDs returns n distinct CVE-shaped ids. The planner
// packs 100 ids per _findings chunk, so callers needing a subset drop ask
// for more than 100.
func epssAdmissionFindingCVEIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, fmt.Sprintf("CVE-2026-%04d", i))
	}
	return ids
}

// recordScopes flattens the progress store's recorded batches into a set.
func recordScopes(records [][]string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, batch := range records {
		for _, scopeID := range batch {
			out[scopeID] = struct{}{}
		}
	}
	return out
}

// TestServiceRunRecordsOnlyInsertedFindingsEPSSForActivation proves
// eshu-hq/eshu#10417 on the activation path: when the open-target guard
// drops one planned _findings chunk, the publication progress records only
// the inserted chunk, so the dropped chunk stays due instead of pacing out
// for 24h.
func TestServiceRunRecordsOnlyInsertedFindingsEPSSForActivation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
		`{"derive_from_findings":{"enabled":true,"sources":["first_epss"]}}`,
		string(scope.CollectorVulnerabilityIntelligence),
		string(scope.CollectorVulnerabilityIntelligence),
	))
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
		enqueuedItems: []workflow.WorkItem{
			epssAdmissionOpenWorkItem(instance.InstanceID, "vuln-intel://first_epss/_findings/0"),
		},
	}
	reader := &fakeOwnedPackageTargetReader{}
	findingReader := &fakeFindingCVEIDReader{ids: epssAdmissionFindingCVEIDs(101)}
	progress := &fakeTargetRefreshProgressStore{}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
	service.FindingCVEIDReader = findingReader
	service.TargetRefreshProgressStore = progress

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := recordScopes(progress.records); len(got) != 1 {
		t.Fatalf("progress records = %v, want exactly the inserted chunk", progress.records)
	} else if _, ok := got["vuln-intel://first_epss/_findings/1"]; !ok {
		t.Fatalf("progress records = %v, want only vuln-intel://first_epss/_findings/1", progress.records)
	}
}

// TestServiceRunRecordsNoFindingsEPSSWhenAllChunksDropped proves the
// zero-insert half of eshu-hq/eshu#10417 on the activation path: when every
// planned _findings chunk is already covered by an open run, the publication
// progress records nothing.
func TestServiceRunRecordsNoFindingsEPSSWhenAllChunksDropped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
		`{"derive_from_findings":{"enabled":true,"sources":["first_epss"]}}`,
		string(scope.CollectorVulnerabilityIntelligence),
		string(scope.CollectorVulnerabilityIntelligence),
	))
	store := &fakeStore{
		instances: []workflow.CollectorInstance{instance},
		enqueuedItems: []workflow.WorkItem{
			epssAdmissionOpenWorkItem(instance.InstanceID, "vuln-intel://first_epss/_findings/0"),
			epssAdmissionOpenWorkItem(instance.InstanceID, "vuln-intel://first_epss/_findings/1"),
		},
	}
	reader := &fakeOwnedPackageTargetReader{}
	findingReader := &fakeFindingCVEIDReader{ids: epssAdmissionFindingCVEIDs(101)}
	progress := &fakeTargetRefreshProgressStore{}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
	service.FindingCVEIDReader = findingReader
	service.TargetRefreshProgressStore = progress

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(progress.records) != 0 {
		t.Fatalf("progress records = %v, want no records when nothing was inserted", progress.records)
	}
}

// TestServiceRefreshCadenceRecordsOnlyInsertedScopes proves
// eshu-hq/eshu#10417 on the in-tree path: when the open-target guard drops a
// covered source, the refresh progress records only the inserted scope.
func TestServiceRefreshCadenceRecordsOnlyInsertedScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	progress := &fakeTargetRefreshProgressStore{}
	store := &fakeStore{
		enqueuedItems: []workflow.WorkItem{
			epssAdmissionOpenWorkItem("collector-vulnerability-intelligence", "vuln-intel://cisa/kev"),
		},
	}
	service := refreshCadenceService(refreshCadenceServiceConfiguration, store, progress)

	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, observedAt)}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if scopes := enqueuedScopeIDs(store.enqueuedItems); len(scopes) != 2 {
		t.Fatalf("enqueued scopes = %v, want the seed plus the inserted EPSS target", scopes)
	}
	if got := recordScopes(progress.records); len(got) != 1 {
		t.Fatalf("progress records = %v, want exactly the inserted scope", progress.records)
	} else if _, ok := got["vuln-intel://first/epss"]; !ok {
		t.Fatalf("progress records = %v, want only vuln-intel://first/epss", progress.records)
	}
}

// TestServiceRefreshCadenceRecordsNothingWhenAllScopesDropped proves the
// zero-insert half of eshu-hq/eshu#10417 on the in-tree path: when every
// covered source is already covered by an open run, the refresh progress
// records nothing.
func TestServiceRefreshCadenceRecordsNothingWhenAllScopesDropped(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	progress := &fakeTargetRefreshProgressStore{}
	store := &fakeStore{
		enqueuedItems: []workflow.WorkItem{
			epssAdmissionOpenWorkItem("collector-vulnerability-intelligence", "vuln-intel://cisa/kev"),
			epssAdmissionOpenWorkItem("collector-vulnerability-intelligence", "vuln-intel://first/epss"),
		},
	}
	service := refreshCadenceService(refreshCadenceServiceConfiguration, store, progress)

	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, observedAt)}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if len(store.enqueuedItems) != 2 {
		t.Fatalf("enqueued scopes = %v, want only the two seeds (nothing inserted)", enqueuedScopeIDs(store.enqueuedItems))
	}
	if len(progress.records) != 0 {
		t.Fatalf("progress records = %v, want no records when nothing was inserted", progress.records)
	}
}
