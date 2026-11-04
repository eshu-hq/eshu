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

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const refreshCadenceServiceConfiguration = `{"targets":[
	{"source":"cisa_kev","scope_id":"vuln-intel://cisa/kev","refresh_interval":"1h"},
	{"source":"first_epss","scope_id":"vuln-intel://first/epss","cve_ids":["CVE-2026-0001"]}
]}`

// fakeTargetRefreshProgressStore is a TargetRefreshProgressStore that
// applies records to its in-memory map with keep-max semantics, so
// consecutive ticks observe each other like the real table.
type fakeTargetRefreshProgressStore struct {
	progress  map[string]time.Time
	reads     int
	records   [][]string
	readErr   error
	recordErr error
}

func (f *fakeTargetRefreshProgressStore) ReadTargetRefreshProgress(_ context.Context, _ string) (map[string]time.Time, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := make(map[string]time.Time, len(f.progress))
	for scopeID, at := range f.progress {
		out[scopeID] = at
	}
	return out, nil
}

func (f *fakeTargetRefreshProgressStore) RecordTargetsRefreshPlanned(_ context.Context, _ string, scopeIDs []string, plannedAt time.Time) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	if f.progress == nil {
		f.progress = make(map[string]time.Time)
	}
	recorded := append([]string(nil), scopeIDs...)
	f.records = append(f.records, recorded)
	for _, scopeID := range recorded {
		if at, ok := f.progress[scopeID]; !ok || plannedAt.After(at) {
			f.progress[scopeID] = plannedAt
		}
	}
	return nil
}

func refreshCadenceService(configuration string, store *fakeStore, progress TargetRefreshProgressStore) Service {
	instance := testServiceVulnerabilityIntelligenceInstance(time.Now().UTC())
	return Service{
		Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, configuration),
		Store:                            store,
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		TargetRefreshProgressStore:       progress,
		Metrics:                          &fakeMetrics{},
	}
}

func refreshCadenceInstance(configuration string, observedAt time.Time) workflow.CollectorInstance {
	instance := testServiceVulnerabilityIntelligenceInstance(observedAt)
	instance.Configuration = configuration
	return instance
}

func enqueuedScopeIDs(items []workflow.WorkItem) []string {
	scopes := make([]string, 0, len(items))
	for _, item := range items {
		scopes = append(scopes, item.ScopeID)
	}
	return scopes
}

// TestServiceRefreshCadencePacesSourcesAcrossTicks proves the whole
// service loop for eshu-hq/eshu#9879: the pre-plan probe flows into the
// planner, not-due targets plan nothing, and the post-admission record
// feeds the next tick's probe.
func TestServiceRefreshCadencePacesSourcesAcrossTicks(t *testing.T) {
	t.Parallel()

	observedT0 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	progress := &fakeTargetRefreshProgressStore{}
	ctx := context.Background()

	tick1Store := &fakeStore{}
	tick1 := refreshCadenceService(refreshCadenceServiceConfiguration, tick1Store, progress)
	if err := tick1.scheduleVulnerabilityIntelligenceWork(ctx, observedT0,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, observedT0)}); err != nil {
		t.Fatalf("tick1 scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if len(tick1Store.createdRuns) != 1 {
		t.Fatalf("tick1 created runs = %d, want 1", len(tick1Store.createdRuns))
	}
	if scopes := enqueuedScopeIDs(tick1Store.enqueuedItems); len(scopes) != 2 {
		t.Fatalf("tick1 enqueued scopes = %v, want both sources (nothing planned yet)", scopes)
	}
	if len(progress.records) != 1 || len(progress.records[0]) != 2 {
		t.Fatalf("tick1 records = %v, want one record covering both scopes", progress.records)
	}

	halfHour := observedT0.Add(30 * time.Minute)
	tick2Store := &fakeStore{}
	tick2 := refreshCadenceService(refreshCadenceServiceConfiguration, tick2Store, progress)
	if err := tick2.scheduleVulnerabilityIntelligenceWork(ctx, halfHour,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, halfHour)}); err != nil {
		t.Fatalf("tick2 scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if scopes := enqueuedScopeIDs(tick2Store.enqueuedItems); len(scopes) != 1 || scopes[0] != "vuln-intel://first/epss" {
		t.Fatalf("tick2 enqueued scopes = %v, want only the every-tick EPSS target", scopes)
	}
	if len(progress.records) != 2 || len(progress.records[1]) != 1 || progress.records[1][0] != "vuln-intel://first/epss" {
		t.Fatalf("tick2 records = %v, want a second record covering only EPSS", progress.records)
	}
	if got := tick2Store.createdRuns[0].RequestedScopeSet; !strings.Contains(got, "refresh_cadence_not_due") {
		t.Fatalf("tick2 requested scope set lacks the cadence skip reason: %s", got)
	}

	pastHour := observedT0.Add(61 * time.Minute)
	tick3Store := &fakeStore{}
	tick3 := refreshCadenceService(refreshCadenceServiceConfiguration, tick3Store, progress)
	if err := tick3.scheduleVulnerabilityIntelligenceWork(ctx, pastHour,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, pastHour)}); err != nil {
		t.Fatalf("tick3 scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if scopes := enqueuedScopeIDs(tick3Store.enqueuedItems); len(scopes) != 2 {
		t.Fatalf("tick3 enqueued scopes = %v, want both sources (KEV interval elapsed)", scopes)
	}
}

// TestServiceRefreshCadenceRequiresStoreWhenOptedIn fails loudly, not
// silently back to every-tick planning, when a target sets
// refresh_interval but no progress store is wired.
func TestServiceRefreshCadenceRequiresStoreWhenOptedIn(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	service := refreshCadenceService(refreshCadenceServiceConfiguration, &fakeStore{}, nil)

	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt,
		[]workflow.CollectorInstance{refreshCadenceInstance(refreshCadenceServiceConfiguration, observedAt)})
	if err == nil || !strings.Contains(err.Error(), "target refresh progress store is required") {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want the missing-store failure", err)
	}
}

// TestServiceRefreshCadenceAllSkippedPersistsNoRun pins the documented
// all-skipped behavior for eshu-hq/eshu#9879: when every target is inside
// its interval the tick persists no run (so no requested_scope_set skip
// rows exist) and records nothing; the plan-span cadence_skipped_targets
// count is the signal.
func TestServiceRefreshCadenceAllSkippedPersistsNoRun(t *testing.T) {
	t.Parallel()

	configuration := `{"targets":[
	{"source":"cisa_kev","scope_id":"vuln-intel://cisa/kev","refresh_interval":"1h"},
	{"source":"first_epss","scope_id":"vuln-intel://first/epss","refresh_interval":"1h","cve_ids":["CVE-2026-0001"]}
]}`
	observedT0 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	progress := &fakeTargetRefreshProgressStore{}
	ctx := context.Background()

	seedStore := &fakeStore{}
	seed := refreshCadenceService(configuration, seedStore, progress)
	if err := seed.scheduleVulnerabilityIntelligenceWork(ctx, observedT0,
		[]workflow.CollectorInstance{refreshCadenceInstance(configuration, observedT0)}); err != nil {
		t.Fatalf("seed scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if len(seedStore.createdRuns) != 1 {
		t.Fatalf("seed created runs = %d, want 1", len(seedStore.createdRuns))
	}
	recordsAfterSeed := len(progress.records)

	halfHour := observedT0.Add(30 * time.Minute)
	tickStore := &fakeStore{}
	tick := refreshCadenceService(configuration, tickStore, progress)
	var tickLogs bytes.Buffer
	tick.Logger = slog.New(slog.NewTextHandler(&tickLogs, nil))
	if err := tick.scheduleVulnerabilityIntelligenceWork(ctx, halfHour,
		[]workflow.CollectorInstance{refreshCadenceInstance(configuration, halfHour)}); err != nil {
		t.Fatalf("all-skipped scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if logged := tickLogs.String(); !strings.Contains(logged, "skipped all vulnerability targets by refresh cadence") ||
		!strings.Contains(logged, "cadence_skipped_targets=2") {
		t.Fatalf("all-skipped tick logs = %q, want the cadence-skip line with cadence_skipped_targets=2", logged)
	}
	if len(tickStore.createdRuns) != 0 {
		t.Fatalf("all-skipped created runs = %d, want 0 (no run persists skip-only ticks)", len(tickStore.createdRuns))
	}
	if len(tickStore.enqueuedItems) != 0 {
		t.Fatalf("all-skipped enqueued items = %d, want 0", len(tickStore.enqueuedItems))
	}
	if len(progress.records) != recordsAfterSeed {
		t.Fatalf("all-skipped records = %v, want no new record (nothing covered)", progress.records)
	}
}

// TestServiceRefreshCadenceSkipsProbeWithoutOptIn proves the zero-coupling
// rule: instances without a refresh_interval never touch the progress
// store, so they need none wired and pay no probe per tick.
func TestServiceRefreshCadenceSkipsProbeWithoutOptIn(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	configuration := testVulnerabilityIntelligenceConfiguration()
	progress := &fakeTargetRefreshProgressStore{}
	service := refreshCadenceService(configuration, &fakeStore{}, progress)

	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), observedAt,
		[]workflow.CollectorInstance{refreshCadenceInstance(configuration, observedAt)}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if progress.reads != 0 {
		t.Fatalf("progress reads = %d without any refresh_interval, want 0", progress.reads)
	}
	if len(progress.records) != 0 {
		t.Fatalf("progress records = %v without any refresh_interval, want none", progress.records)
	}
}
