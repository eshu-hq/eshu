// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// errFakeGHSAWindowCursorRead stands in for a cursor store read failure:
// the schedule must surface it, never plan from zero over committed windows.
var errFakeGHSAWindowCursorRead = errors.New("fake ghsa window cursor read failure")

// fakeGHSAWindowCursorReader is the fake-reader double for every
// vulnerabilityGHSAWindowCursors branch at the service seam (#10037).
type fakeGHSAWindowCursorReader struct {
	cursors map[string]int64
	err     error
	calls   int
}

func (f *fakeGHSAWindowCursorReader) ReadGHSAWindowCursor(_ context.Context, _ string, advisoryType string) (int64, bool, error) {
	f.calls++
	if f.err != nil {
		return 0, false, f.err
	}
	cursor, set := f.cursors[advisoryType]
	return cursor, set, nil
}

const testGHSAWindowEnabledConfiguration = `{"targets":[
	{"source":"ghsa","scope_id":"vuln-intel://ghsa/reviewed","advisory_type":"reviewed","modified_start":"2026-11-08T00:00:00Z","modified_end":"2026-11-09T00:00:00Z","results_per_page":100,"max_requests":50,"refresh_interval":"2h"}
],"derive_ghsa_rolling_windows":{"enabled":true}}`

func testGHSAWindowService(planner *fakeVulnerabilityIntelligencePlanner, reader GHSAWindowCursorReader) Service {
	service := testNVDWindowService(planner, nil)
	service.GHSAWindowCursorReader = reader
	// The anchor carries refresh_interval (the rolling-anchor cadence), so
	// the schedule reads refresh progress before planning.
	service.TargetRefreshProgressStore = &fakeTargetRefreshProgressStore{}
	return service
}

// TestServiceGHSAWindowCursorDisabledSkipsReader pins branch 1: a disabled
// rolling derivation never reads GHSA cursors.
func TestServiceGHSAWindowCursorDisabledSkipsReader(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reader := &fakeGHSAWindowCursorReader{err: errFakeGHSAWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testVulnerabilityIntelligenceConfiguration(), now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("cursor reader calls = %d, want 0 with the derivation disabled", reader.calls)
	}
	if len(planner.requests) != 1 || planner.requests[0].GHSAWindows.Enabled {
		t.Fatalf("planner requests = %+v, want one request with GHSA rolling off", planner.requests)
	}
}

// TestServiceGHSAWindowCursorMissingReaderFails pins branch 2: an enabled
// rolling derivation without a wired reader fails loudly before planning.
func TestServiceGHSAWindowCursorMissingReaderFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, nil)
	instance := testNVDWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance})
	if err == nil || !strings.Contains(err.Error(), "ghsa window cursor reader is required") {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want the missing-reader failure", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 (no planning without a cursor reader)", len(planner.requests))
	}
}

// TestServiceGHSAWindowCursorReadErrorFails pins branch 3: a cursor read
// failure surfaces wrapped and blocks planning, never answered with unset.
func TestServiceGHSAWindowCursorReadErrorFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reader := &fakeGHSAWindowCursorReader{err: errFakeGHSAWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance})
	if !errors.Is(err, errFakeGHSAWindowCursorRead) {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want it to wrap the reader failure", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 (no planning from zero on a read error)", len(planner.requests))
	}
}

// TestServiceGHSAWindowCursorsFlowToPlanner pins the success branch: both
// per-type stored cursors reach the planner request unchanged.
func TestServiceGHSAWindowCursorsFlowToPlanner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	reviewedCursor := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).UnixMicro()
	reader := &fakeGHSAWindowCursorReader{cursors: map[string]int64{"reviewed": reviewedCursor}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if reader.calls != 2 {
		t.Fatalf("cursor reader calls = %d, want 2 (one per advisory type)", reader.calls)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1", len(planner.requests))
	}
	request := planner.requests[0]
	if !request.GHSAWindows.Enabled {
		t.Fatal("planner request GHSAWindows.Enabled = false, want true")
	}
	want := vulnerability.GHSAWindowCursor{UnixMicro: reviewedCursor, Set: true}
	if got := request.GHSAWindowCursors["reviewed"]; got != want {
		t.Fatalf("reviewed cursor = %+v, want %+v", got, want)
	}
	if got := request.GHSAWindowCursors["malware"]; got.Set {
		t.Fatalf("malware cursor = %+v, want unset (bootstrap)", got)
	}
}

// TestServiceRunPlansGHSAWindowsForVulnerabilityActivation pins the
// activation service lane (#10037): a GHSA-only activation enqueues one
// rolling window item per anchor type next to its host item, planned from
// the stored cursors, and never a static anchor item.
func TestServiceRunPlansGHSAWindowsForVulnerabilityActivation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	componentConfig := `{"targets":[` +
		`{"source":"ghsa","scope_id":"vuln-intel://ghsa/reviewed","advisory_type":"reviewed","modified_start":"2026-11-08T00:00:00Z","modified_end":"2026-11-09T00:00:00Z","results_per_page":100,"max_requests":50,"refresh_interval":"2h"},` +
		`{"source":"ghsa","scope_id":"vuln-intel://ghsa/malware","advisory_type":"malware","modified_start":"2026-11-08T00:00:00Z","modified_end":"2026-11-09T00:00:00Z","results_per_page":100,"max_requests":50,"refresh_interval":"2h"}` +
		`],"derive_ghsa_rolling_windows":{"enabled":true}}`
	instance := vulnerabilityActivationWithDerive(now, componentConfig)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reviewedCursor := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).UnixMicro()
	service := testVulnerabilityActivationService(t, now, instance, store, &fakeOwnedPackageTargetReader{}, `{"mode":"broad"}`)
	service.GHSAWindowCursorReader = &fakeGHSAWindowCursorReader{cursors: map[string]int64{"reviewed": reviewedCursor}}

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	windows := 0
	for _, item := range store.enqueuedItems {
		if item.ScopeID == "vuln-intel://ghsa/reviewed" || item.ScopeID == "vuln-intel://ghsa/malware" {
			t.Fatalf("enqueued static anchor %q with rolling enabled, want window scopes only", item.ScopeID)
		}
		advisoryType, start, _, ok := vulnsource.ParseGHSAWindowScope(item.ScopeID)
		if !ok {
			continue
		}
		windows++
		if advisoryType == "reviewed" && start.UnixMicro() != reviewedCursor {
			t.Fatalf("reviewed window start = %v, want stored cursor", start)
		}
	}
	if windows != 2 {
		t.Fatalf("enqueued GHSA windows = %d, want 2 (one per anchor type)", windows)
	}
}
