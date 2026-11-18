// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// errFakeNVDWindowCursorRead stands in for a cursor store read failure: the
// schedule must surface it, never plan from zero over committed windows.
var errFakeNVDWindowCursorRead = errors.New("fake nvd window cursor read failure")

// fakeNVDWindowCursorReader is the #9971 review F-5 fake-reader double for
// every vulnerabilityNVDWindowCursor branch at the service seam.
type fakeNVDWindowCursorReader struct {
	cursor vulnerability.NVDWindowCursor
	err    error
	calls  int
}

func (f *fakeNVDWindowCursorReader) ReadNVDWindowCursor(context.Context, string) (vulnerability.NVDWindowCursor, error) {
	f.calls++
	return f.cursor, f.err
}

func testNVDWindowServiceInstance(configuration string, now time.Time) workflow.CollectorInstance {
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

func testNVDWindowService(planner *fakeVulnerabilityIntelligencePlanner, reader NVDWindowCursorReader) Service {
	return Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		VulnerabilityIntelligencePlanner: planner,
		NVDWindowCursorReader:            reader,
	}
}

const testNVDWindowEnabledConfiguration = `{"targets":[
{"source":"nvd","scope_id":"vuln-intel://nvd/anchor","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-04-01T00:00:00Z","results_per_page":50}
],"derive_nvd_rolling_windows":{"enabled":true}}`

// testNVDWindowInvalidConfiguration is the enabled derivation with a
// nvd_feed_transport value that is neither on nor off: planning continues
// with the feed read as off, and the tick warns with the closed reason.
const testNVDWindowInvalidConfiguration = `{"targets":[
{"source":"nvd","scope_id":"vuln-intel://nvd/anchor","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-04-01T00:00:00Z","results_per_page":50}
],"derive_nvd_rolling_windows":{"enabled":true},"nvd_feed_transport":"auto"}`

// TestServiceNVDWindowCursorDisabledSkipsReader pins branch 1: a disabled
// rolling derivation never reads the cursor, and the planner request
// carries the derivation off.
func TestServiceNVDWindowCursorDisabledSkipsReader(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	reader := &fakeNVDWindowCursorReader{err: errFakeNVDWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testVulnerabilityIntelligenceConfiguration(), now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if reader.calls != 0 {
		t.Fatalf("cursor reader calls = %d, want 0 with the derivation disabled", reader.calls)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1", len(planner.requests))
	}
	if planner.requests[0].NVDWindows.Enabled || planner.requests[0].NVDWindowCursorSet {
		t.Fatalf("planner request = %+v, want the rolling derivation off", planner.requests[0])
	}
}

// TestServiceNVDWindowCursorMissingReaderFails pins branch 2: an enabled
// rolling derivation without a wired reader fails loudly before planning
// instead of silently planning every window from zero.
func TestServiceNVDWindowCursorMissingReaderFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, nil)
	instance := testNVDWindowServiceInstance(testNVDWindowEnabledConfiguration, now)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance})
	if err == nil || !strings.Contains(err.Error(), "nvd window cursor reader is required") {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want the missing-reader failure", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 (no planning without a cursor reader)", len(planner.requests))
	}
}

// TestServiceNVDWindowCursorReadErrorFails pins branch 3: a cursor read
// failure surfaces wrapped and blocks planning, never answered with unset.
func TestServiceNVDWindowCursorReadErrorFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	reader := &fakeNVDWindowCursorReader{err: errFakeNVDWindowCursorRead}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testNVDWindowEnabledConfiguration, now)
	err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance})
	if !errors.Is(err, errFakeNVDWindowCursorRead) {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want it to wrap the reader failure", err)
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 (no planning from zero on a read error)", len(planner.requests))
	}
}

// TestServiceNVDWindowCursorFlowsToPlanner pins the success branch: the
// stored cursor reaches the planner request unchanged.
func TestServiceNVDWindowCursorFlowsToPlanner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	cursor := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).UnixMicro()
	reader := &fakeNVDWindowCursorReader{cursor: vulnerability.NVDWindowCursor{CursorUnixMicro: cursor, Set: true}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	instance := testNVDWindowServiceInstance(testNVDWindowEnabledConfiguration, now)
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
	if !request.NVDWindows.Enabled || !request.NVDWindowCursorSet || request.NVDWindowCursorUnixMicro != cursor {
		t.Fatalf("planner request cursor = (enabled=%v, set=%v, cursor=%d), want (true, true, %d)",
			request.NVDWindows.Enabled, request.NVDWindowCursorSet, request.NVDWindowCursorUnixMicro, cursor)
	}
}

// TestServiceNVDWindowCursorInvalidSettingWarns pins the kill-switch
// fail-safe (eshu-hq/eshu#10716): a nvd_feed_transport value that is neither
// on nor off does not fail the tick. Planning continues with the feed read
// as off, and the tick warns with the closed reason instead of the
// configured value.
func TestServiceNVDWindowCursorInvalidSettingWarns(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	cursor := now.Add(-time.Hour).UnixMicro()
	reader := &fakeNVDWindowCursorReader{cursor: vulnerability.NVDWindowCursor{CursorUnixMicro: cursor, Set: true}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	var logs bytes.Buffer
	service.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	instance := testNVDWindowServiceInstance(testNVDWindowInvalidConfiguration, now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil: a bad kill-switch value must not abort planning", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %d, want 1 (planning continues with the feed off)", len(planner.requests))
	}
	out := logs.String()
	if !strings.Contains(out, "invalid_value") || !strings.Contains(out, "nvd_feed_transport=off") {
		t.Fatalf("tick logs = %q, want the invalid_value warning reading the feed as off", out)
	}
	if strings.Contains(out, "auto") {
		t.Fatalf("tick logs = %q, want the closed reason only, never the configured value", out)
	}
}

// TestServiceNVDWindowCursorValidSettingStaysQuiet pins the other half of
// the warning: a valid (or absent) kill-switch value logs no invalid_value
// line, so the warning cannot be mistaken for a per-tick heartbeat.
func TestServiceNVDWindowCursorValidSettingStaysQuiet(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	cursor := now.Add(-time.Hour).UnixMicro()
	reader := &fakeNVDWindowCursorReader{cursor: vulnerability.NVDWindowCursor{CursorUnixMicro: cursor, Set: true}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	var logs bytes.Buffer
	service.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	instance := testNVDWindowServiceInstance(testNVDWindowEnabledConfiguration, now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	if strings.Contains(logs.String(), "invalid_value") {
		t.Fatalf("tick logs = %q, want no invalid_value warning for a valid setting", logs.String())
	}
}

// TestServiceNVDWindowCursorReportLogsLagAndAge pins the widened-reader
// report (eshu-hq/eshu#10716): the tick logs one "read NVD window cursor"
// line carrying the server-stamped lag (head minus cursor) and the cursor
// row age. Counts and timestamps only, never a scope id.
func TestServiceNVDWindowCursorReportLogsLagAndAge(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	cursor := now.Add(-time.Hour).UnixMicro()
	reader := &fakeNVDWindowCursorReader{cursor: vulnerability.NVDWindowCursor{
		CursorUnixMicro: cursor,
		Set:             true,
		HeadUnixMicro:   cursor + 6_000_000,
		UpdatedAt:       now.Add(-90 * time.Second),
	}}
	planner := &fakeVulnerabilityIntelligencePlanner{}
	service := testNVDWindowService(planner, reader)
	var logs bytes.Buffer
	service.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	instance := testNVDWindowServiceInstance(testNVDWindowEnabledConfiguration, now)
	if err := service.scheduleVulnerabilityIntelligenceWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleVulnerabilityIntelligenceWork() error = %v, want nil", err)
	}
	out := logs.String()
	if !strings.Contains(out, "read NVD window cursor") {
		t.Fatalf("tick logs = %q, want the cursor report line", out)
	}
	if !strings.Contains(out, "cursor_lag_seconds=6") {
		t.Fatalf("tick logs = %q, want the 6-second head-minus-cursor lag", out)
	}
	if !strings.Contains(out, "cursor_updated_age_seconds=90") {
		t.Fatalf("tick logs = %q, want the 90-second cursor row age", out)
	}
}
