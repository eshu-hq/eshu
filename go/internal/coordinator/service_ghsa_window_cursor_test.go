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
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// errFakeGHSAWindowCursorRead stands in for a cursor store read failure: the
// schedule must surface it, never plan from zero over committed windows.
var errFakeGHSAWindowCursorRead = errors.New("fake ghsa window cursor read failure")

// fakeGHSAWindowCursorReader is the #10517 fake-reader double for every
// vulnerabilityGHSAWindowCursors branch at the service seam.
type fakeGHSAWindowCursorReader struct {
	cursors map[string]vulnerability.GHSAWindowCursor
	err     error
	calls   int
}

func (f *fakeGHSAWindowCursorReader) ReadGHSAWindowCursors(context.Context, string) (map[string]vulnerability.GHSAWindowCursor, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]vulnerability.GHSAWindowCursor, len(f.cursors))
	for advisoryType, cursor := range f.cursors {
		out[advisoryType] = cursor
	}
	return out, nil
}

func testGHSAWindowServiceInstance(configuration string, now time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-vulnerability-intelligence",
		CollectorKind:  scope.CollectorVulnerabilityIntelligence,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func testGHSAWindowService(planner *fakeVulnerabilityIntelligencePlanner, reader GHSAWindowCursorReader) Service {
	return Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		VulnerabilityIntelligencePlanner: planner,
		GHSAWindowCursorReader:           reader,
		// GHSA rolling anchors require refresh_interval (the 1h floor),
		// so every GHSA-enabled instance opts into the progress probe.
		TargetRefreshProgressStore: &fakeTargetRefreshProgressStore{},
	}
}

const testGHSAWindowEnabledConfiguration = `{"targets":[
	{"source":"ghsa","scope_id":"vuln-intel://ghsa/reviewed","advisory_type":"reviewed","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-01-02T00:00:00Z","results_per_page":30,"max_requests":10,"refresh_interval":"2h"},
	{"source":"ghsa","scope_id":"vuln-intel://ghsa/malware","advisory_type":"malware","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-01-02T00:00:00Z","results_per_page":30,"max_requests":10,"refresh_interval":"2h"}
],"derive_ghsa_rolling_windows":{"enabled":true}}`

// TestServiceGHSAWindowCursorDisabledSkipsReader pins branch 1: a disabled
// rolling derivation never reads the cursors, and the planner request
// carries the derivation off.
func TestServiceGHSAWindowCursorDisabledSkipsReader(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	reader := &fakeGHSAWindowCursorReader{err: errFakeGHSAWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testGHSAWindowServiceInstance(testVulnerabilityIntelligenceConfiguration(), now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("cursor reader calls = %d, want 0 with the derivation disabled", reader.calls)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1", len(planner.requests))
	}
	if planner.requests[0].GHSAWindows.Enabled || len(planner.requests[0].GHSAWindowCursors) != 0 {
		t.Fatalf("planner request = %+v, want the rolling derivation off", planner.requests[0])
	}
}

// TestServiceGHSAWindowCursorMissingReaderFails pins branch 2: an enabled
// rolling derivation without a wired reader fails loudly before planning
// instead of silently planning every window from zero.
func TestServiceGHSAWindowCursorMissingReaderFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, nil)
	instance := testGHSAWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
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
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	reader := &fakeGHSAWindowCursorReader{err: errFakeGHSAWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testGHSAWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance})
	if !errors.Is(err, errFakeGHSAWindowCursorRead) {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want it to wrap the reader failure", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 (no planning from zero on a read error)", len(planner.requests))
	}
}

// TestServiceGHSAWindowCursorFlowsToPlanner pins the success branch: the
// stored per-type cursors reach the planner request unchanged.
func TestServiceGHSAWindowCursorFlowsToPlanner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	reviewed := now.Add(-48 * time.Hour).UTC().UnixMicro()
	malware := now.Add(-72 * time.Hour).UTC().UnixMicro()
	reader := &fakeGHSAWindowCursorReader{cursors: map[string]vulnerability.GHSAWindowCursor{
		"reviewed": {UnixMicro: reviewed, Set: true},
		"malware":  {UnixMicro: malware, Set: true},
	}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testGHSAWindowService(planner, reader)
	instance := testGHSAWindowServiceInstance(testGHSAWindowEnabledConfiguration, now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if reader.calls != 1 {
		t.Fatalf("cursor reader calls = %d, want 1", reader.calls)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1", len(planner.requests))
	}
	request := planner.requests[0]
	if !request.GHSAWindows.Enabled {
		t.Fatalf("planner request derivation = %+v, want enabled", request.GHSAWindows)
	}
	if got := request.GHSAWindowCursors["reviewed"]; !got.Set || got.UnixMicro != reviewed {
		t.Fatalf("planner request reviewed cursor = %+v, want (%d, set)", got, reviewed)
	}
	if got := request.GHSAWindowCursors["malware"]; !got.Set || got.UnixMicro != malware {
		t.Fatalf("planner request malware cursor = %+v, want (%d, set)", got, malware)
	}
}
