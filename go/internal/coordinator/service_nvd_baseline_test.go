// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fakeNVDBaselineCheckpointReader serves stored year checkpoints from memory.
type fakeNVDBaselineCheckpointReader struct {
	checkpoints map[int]NVDBaselineStoredCheckpoint
	err         error
	calls       int
}

func (f *fakeNVDBaselineCheckpointReader) ReadNVDBaselineCheckpoints(_ context.Context, _ string) (map[int]NVDBaselineStoredCheckpoint, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.checkpoints, nil
}

// fakeNVDBaselinePlanReader serves per-year plan rows from memory.
type fakeNVDBaselinePlanReader struct {
	plans map[int]NVDBaselineStoredPlan
	err   error
	calls int
}

func (f *fakeNVDBaselinePlanReader) ReadNVDBaselinePlans(_ context.Context, _ string) (map[int]NVDBaselineStoredPlan, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.plans, nil
}

// fakeNVDBaselineTripLatchStore is an in-memory trip latch with the
// production revision guard: a write applies only at the read revision.
type fakeNVDBaselineTripLatchStore struct {
	mu       sync.Mutex
	latch    vulnerability.NVDBaselineTripLatch
	found    bool
	readErr  error
	writeErr error
	writes   int
}

func (f *fakeNVDBaselineTripLatchStore) ReadNVDBaselineTripLatch(_ context.Context, _ string) (vulnerability.NVDBaselineTripLatch, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return vulnerability.NVDBaselineTripLatch{}, false, f.readErr
	}
	return f.latch, f.found, nil
}

func (f *fakeNVDBaselineTripLatchStore) WriteNVDBaselineTripLatch(_ context.Context, _ string, next vulnerability.NVDBaselineTripLatch, expectedRevision int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return false, f.writeErr
	}
	if expectedRevision != f.latch.Revision {
		return false, nil
	}
	f.writes++
	f.latch = next
	f.latch.Revision++
	f.found = true
	return true, nil
}

// fakeNVDBaselineDrainStore serves drain states by key from memory.
type fakeNVDBaselineDrainStore struct {
	mu     sync.Mutex
	states map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState
	err    error
	keys   []vulnerability.NVDBaselineDrainKey
}

func (f *fakeNVDBaselineDrainStore) ReadNVDBaselineDrain(_ context.Context, key vulnerability.NVDBaselineDrainKey) (vulnerability.NVDBaselineDrainState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, key)
	if f.err != nil {
		return vulnerability.NVDBaselineDrainState{}, f.err
	}
	state, ok := f.states[key]
	if !ok {
		return vulnerability.NVDBaselineDrainState{}, fmt.Errorf("no drain for %+v", key)
	}
	return state, nil
}

// fakeNVDBaselineTripSampler serves one canned hourly sample.
type fakeNVDBaselineTripSampler struct {
	mu     sync.Mutex
	sample NVDBaselineTripSample
	err    error
	calls  int
}

func (f *fakeNVDBaselineTripSampler) SampleNVDBaselineTrip(_ context.Context, _ time.Time) (NVDBaselineTripSample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return NVDBaselineTripSample{}, f.err
	}
	return f.sample, nil
}

// baselineServiceTestFixtures bundles the fakes and the service under test.
type baselineServiceTestFixtures struct {
	service     Service
	checkpoints *fakeNVDBaselineCheckpointReader
	plans       *fakeNVDBaselinePlanReader
	latch       *fakeNVDBaselineTripLatchStore
	drain       *fakeNVDBaselineDrainStore
	sampler     *fakeNVDBaselineTripSampler
	messages    *[]string
	reader      *sdkmetric.ManualReader
}

func newBaselineServiceTestFixtures(now time.Time, config string) *baselineServiceTestFixtures {
	var messages []string
	fx := &baselineServiceTestFixtures{
		checkpoints: &fakeNVDBaselineCheckpointReader{},
		plans:       &fakeNVDBaselinePlanReader{},
		latch:       &fakeNVDBaselineTripLatchStore{},
		drain:       &fakeNVDBaselineDrainStore{states: map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{}},
		sampler: &fakeNVDBaselineTripSampler{sample: NVDBaselineTripSample{
			At:                    now,
			DirtyProxyRowsPerHour: 100,
			OldestDirtyAge:        time.Hour,
		}},
		messages: &messages,
		reader:   sdkmetric.NewManualReader(),
	}
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(fx.reader)).Meter("test"))
	if err != nil {
		panic(err)
	}
	fx.service = Service{
		Logger:                      slog.New(recordingLogHandler{messages: fx.messages}),
		NVDBaselineCheckpointReader: fx.checkpoints,
		NVDBaselinePlanReader:       fx.plans,
		NVDBaselineTripLatchStore:   fx.latch,
		NVDBaselineDrainStore:       fx.drain,
		NVDBaselineTripSampler:      fx.sampler,
		NVDBaselineInstruments:      instruments,
		NVDBaselineTickMemory:       NewNVDBaselineTickMemory(),
		Clock:                       func() time.Time { return now },
	}
	_ = config
	return fx
}

func baselineTestConfig(runID string, lastYear int) string {
	return fmt.Sprintf(`{"derive_nvd_baseline":{"enabled":true,"baseline_run_id":%q,"last_year":%d}}`, runID, lastYear)
}

// collectBaselineCounter sums one int64 counter across its label sets.
func collectBaselineCounter(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != name {
				continue
			}
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	return total
}

// collectBaselineGauge reads one float64 gauge value, optionally filtered to
// one label value.
func collectBaselineGauge(t *testing.T, reader *sdkmetric.ManualReader, name, label, value string) (float64, bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			gauge, ok := m.Data.(metricdata.Gauge[float64])
			if !ok || m.Name != name {
				continue
			}
			for _, point := range gauge.DataPoints {
				if label == "" {
					return point.Value, true
				}
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == label && attr.Value.AsInterface() == value {
						return point.Value, true
					}
				}
			}
		}
	}
	return 0, false
}

func baselineWarned(messages []string) bool {
	for _, message := range messages {
		if strings.Contains(message, "baseline") {
			return true
		}
	}
	return false
}

// TestLoadNVDBaselinePlanInputsDisabledReadsNothing proves a disabled
// derivation returns zero inputs without touching any store: the quiet lane
// costs no reads.
func TestLoadNVDBaselinePlanInputsDisabledReadsNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, `{"derive_nvd_baseline":{"enabled":false}}`)
	fx.checkpoints.err = errBaselineStoreUnreachable
	fx.plans.err = errBaselineStoreUnreachable
	fx.latch.readErr = errBaselineStoreUnreachable

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), `{"derive_nvd_baseline":{"enabled":false}}`, "run-1", now)

	if inputs.Derivation.Enabled {
		t.Fatal("loadNVDBaselinePlanInputs() derivation enabled, want disabled")
	}
	if fx.checkpoints.calls != 0 || fx.plans.calls != 0 {
		t.Fatalf("loadNVDBaselinePlanInputs() store calls = %d/%d, want none", fx.checkpoints.calls, fx.plans.calls)
	}
	if baselineWarned(*fx.messages) {
		t.Fatalf("loadNVDBaselinePlanInputs() logged %v, want silence on a disabled lane", *fx.messages)
	}
}

// TestLoadNVDBaselinePlanInputsConfigErrorHoldsLoud proves a section that
// fails the strict decode carries its config error and logs it at WARN
// without touching any store.
func TestLoadNVDBaselinePlanInputsConfigErrorHoldsLoud(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	config := `{"derive_nvd_baseline":{"enabled":true,"baseline_run_id":"run-1","last_year":2002,"bogus":true}}`

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if inputs.ConfigErr == nil {
		t.Fatal("loadNVDBaselinePlanInputs() ConfigErr = nil, want the decode failure")
	}
	if fx.checkpoints.calls != 0 || fx.plans.calls != 0 {
		t.Fatalf("loadNVDBaselinePlanInputs() store calls = %d/%d, want none on a config error", fx.checkpoints.calls, fx.plans.calls)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the config error")
	}
}

// TestLoadNVDBaselinePlanInputsCheckpointFailureFailsClosed proves an
// unreadable checkpoint store zeroes the trip so the planner holds instead
// of replaying the committed prefix from zero, and logs the cause at WARN.
func TestLoadNVDBaselinePlanInputsCheckpointFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.err = errBaselineStoreUnreachable
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if inputs.Trip.Outcome != "" {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want never-loaded on a checkpoint failure", inputs.Trip.Outcome)
	}
	if inputs.Derivation.RunID != "run-1" {
		t.Fatalf("loadNVDBaselinePlanInputs() run id = %q, want run-1", inputs.Derivation.RunID)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the checkpoint failure")
	}
	if fx.sampler.calls != 0 {
		t.Fatalf("sampler calls = %d, want none when the checkpoints never loaded", fx.sampler.calls)
	}
}

// TestLoadNVDBaselinePlanInputsPlanFailureFailsClosed proves an unreadable
// plan store zeroes the trip so the planner holds instead of planning blind
// from checkpoints with no pace or in-flight proof, and logs the cause at
// WARN. A dropped chunk writes no row and a failed read returns none: both
// read as unprovable, never as "never planned".
func TestLoadNVDBaselinePlanInputsPlanFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			Checkpoint:       vulnerability.NVDBaselineCheckpoint{ResumeUnixMicro: 946684800000000, ResumeCVEID: "CVE-2002-0001", RecordsDone: 10, RecordsTotal: 100},
			LastScopeID:      "scope-2002",
			LastGenerationID: "gen-2002",
		},
	}
	fx.plans.err = errBaselineStoreUnreachable
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-30 * time.Minute)}
	fx.latch.found = true
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if inputs.Trip.Outcome != "" {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want never-loaded on a plan failure", inputs.Trip.Outcome)
	}
	if len(inputs.InFlightYears) != 0 || len(inputs.LastPlanned) != 0 {
		t.Fatalf("loadNVDBaselinePlanInputs() plan state = (%v, %v), want no pace or in-flight proof from an unreadable read", inputs.InFlightYears, inputs.LastPlanned)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the plan failure")
	}
	if fx.sampler.calls != 0 {
		t.Fatalf("sampler calls = %d, want none when the plan rows never loaded", fx.sampler.calls)
	}
}

// TestLoadNVDBaselinePlanInputsMissingReaderHoldsLoud proves a missing plan
// reader holds the lane the same loud way as an unreadable store: the trip
// stays never-loaded so an enabled derivation never plans blind.
func TestLoadNVDBaselinePlanInputsMissingReaderHoldsLoud(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			Checkpoint:       vulnerability.NVDBaselineCheckpoint{ResumeUnixMicro: 946684800000000, ResumeCVEID: "CVE-2002-0001"},
			LastScopeID:      "scope-2002",
			LastGenerationID: "gen-2002",
		},
	}
	fx.service.NVDBaselinePlanReader = nil
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if inputs.Trip.Outcome != "" {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want never-loaded without a plan reader", inputs.Trip.Outcome)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the missing plan reader")
	}
	if fx.sampler.calls != 0 {
		t.Fatalf("sampler calls = %d, want none without a plan reader", fx.sampler.calls)
	}
}

// TestLoadNVDBaselinePlanInputsMapsLiveState proves the happy path maps every
// request field: the derivation, the checkpoint positions, the in-flight set
// and last-planned times from the plan rows, the loaded trip, and the drain
// states keyed by the checkpoints' last scope and generation.
func TestLoadNVDBaselinePlanInputsMapsLiveState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			Checkpoint:       vulnerability.NVDBaselineCheckpoint{ResumeUnixMicro: 946684800000000, ResumeCVEID: "CVE-2002-0001", RecordsDone: 10, RecordsTotal: 100},
			LastScopeID:      "scope-2002",
			LastGenerationID: "gen-2002",
		},
	}
	plannedAt := now.Add(-time.Hour)
	fx.plans.plans = map[int]NVDBaselineStoredPlan{
		2002: {LastPlannedScopeID: "scope-2002", LastPlannedAt: plannedAt, InFlight: true},
	}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-30 * time.Minute)}
	fx.latch.found = true
	fx.drain.states = map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
		{ScopeID: "scope-2002", GenerationID: "gen-2002"}: {Phase: vulnerability.NVDBaselineDrainClaimOpen, NotDoneSince: now.Add(-time.Hour)},
	}
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if !inputs.Derivation.Enabled || inputs.Derivation.RunID != "run-1" {
		t.Fatalf("loadNVDBaselinePlanInputs() derivation = %+v, want enabled run-1", inputs.Derivation)
	}
	got, ok := inputs.Checkpoints[2002]
	if !ok || got.ResumeCVEID != "CVE-2002-0001" || got.RecordsTotal != 100 {
		t.Fatalf("loadNVDBaselinePlanInputs() checkpoints = %+v, want the stored 2002 position", inputs.Checkpoints)
	}
	if !inputs.InFlightYears[2002] {
		t.Fatalf("loadNVDBaselinePlanInputs() in-flight = %v, want 2002 set", inputs.InFlightYears)
	}
	last, ok := inputs.LastPlanned[2002]
	if !ok || last.ScopeID != "scope-2002" || !last.PlannedAt.Equal(plannedAt) {
		t.Fatalf("loadNVDBaselinePlanInputs() last planned = %+v, want scope-2002 at %v", inputs.LastPlanned, plannedAt)
	}
	if inputs.Trip.Outcome != vulnerability.NVDBaselineTripLatchLoaded || inputs.Trip.Open {
		t.Fatalf("loadNVDBaselinePlanInputs() trip = %+v, want a loaded shut latch", inputs.Trip)
	}
	drain, ok := inputs.Drain[2002]
	if !ok || drain.Phase != vulnerability.NVDBaselineDrainClaimOpen {
		t.Fatalf("loadNVDBaselinePlanInputs() drain = %+v, want the 2002 claim_open state", inputs.Drain)
	}
	if len(fx.drain.keys) != 1 || fx.drain.keys[0].ScopeID != "scope-2002" {
		t.Fatalf("drain probe keys = %+v, want the checkpoint's last scope and generation", fx.drain.keys)
	}
	if fx.sampler.calls != 0 {
		t.Fatalf("sampler calls = %d, want none on a fresh latch", fx.sampler.calls)
	}
	if baselineWarned(*fx.messages) {
		t.Fatalf("loadNVDBaselinePlanInputs() logged %v, want silence on a clean load", *fx.messages)
	}
}

// TestLoadNVDBaselinePlanInputsSamplesHourly proves a missing latch row runs
// one hourly sample, folds it through the guarded write, and plans from the
// freshly loaded latch.
func TestLoadNVDBaselinePlanInputsSamplesHourly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if fx.sampler.calls != 1 {
		t.Fatalf("sampler calls = %d, want exactly one hourly sample", fx.sampler.calls)
	}
	if inputs.Trip.Outcome != vulnerability.NVDBaselineTripLatchLoaded || inputs.Trip.Open {
		t.Fatalf("loadNVDBaselinePlanInputs() trip = %+v, want a loaded shut latch after the sample", inputs.Trip)
	}
	if got := collectBaselineCounter(t, fx.reader, "eshu_dp_nvd_baseline_dirty_proxy_rows_per_hour"); got != 0 {
		t.Fatalf("dirty proxy counter = %d, want no counter (the rate is a gauge)", got)
	}
	rate, ok := collectBaselineGauge(t, fx.reader, "eshu_dp_nvd_baseline_dirty_proxy_rows_per_hour", "", "")
	if !ok || rate != 100 {
		t.Fatalf("dirty proxy gauge = %v, %v, want 100 from the hourly sample", rate, ok)
	}
	if got := collectBaselineCounter(t, fx.reader, "eshu_dp_nvd_baseline_trip_latch_total"); got != 1 {
		t.Fatalf("trip latch outcomes = %d, want the one applied sample", got)
	}
}

// TestLoadNVDBaselinePlanInputsStaleLatchResamples proves a latch older than
// an hour runs the sampler again, so a quiet hour refreshes the evidence the
// planner paces from.
func TestLoadNVDBaselinePlanInputsStaleLatchResamples(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-2 * time.Hour)}
	fx.latch.found = true
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if fx.sampler.calls != 1 {
		t.Fatalf("sampler calls = %d, want one resample on a stale latch", fx.sampler.calls)
	}
	if inputs.Trip.Outcome != vulnerability.NVDBaselineTripLatchLoaded {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want loaded after the resample", inputs.Trip.Outcome)
	}
}

// TestLoadNVDBaselinePlanInputsSamplerFailureKeepsLoadedTrip proves a broken
// dirty-log probe warns and plans from the loaded latch instead of failing
// the tick: the planner holds stale when the latch is old, plans when it is
// fresh.
func TestLoadNVDBaselinePlanInputsSamplerFailureKeepsLoadedTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-30 * time.Minute)}
	fx.latch.found = true
	fx.sampler.err = errBaselineStoreUnreachable
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if fx.sampler.calls != 0 {
		t.Fatalf("sampler calls = %d, want none on a fresh latch", fx.sampler.calls)
	}
	if inputs.Trip.Outcome != vulnerability.NVDBaselineTripLatchLoaded {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want the loaded latch kept", inputs.Trip.Outcome)
	}

	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-2 * time.Hour)}
	inputs = fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)
	if inputs.Trip.Outcome != vulnerability.NVDBaselineTripLatchLoaded {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want the stale loaded latch kept for the planner to hold on", inputs.Trip.Outcome)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the sampler failure")
	}
}

// TestLoadNVDBaselinePlanInputsStoredAheadHolds proves a latch row stamped
// past the clock refuses loudly: the outcome is recorded, a WARN names it,
// and the trip zeroes so planning holds instead of trusting a deaf latch.
func TestLoadNVDBaselinePlanInputsStoredAheadHolds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(time.Hour)}
	fx.latch.found = true
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if fx.sampler.calls != 1 {
		t.Fatalf("sampler calls = %d, want the future-stamped sample to run the loud refusal", fx.sampler.calls)
	}
	if inputs.Trip.Outcome != "" {
		t.Fatalf("loadNVDBaselinePlanInputs() trip outcome = %q, want never-loaded so planning holds", inputs.Trip.Outcome)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the stored_ahead latch")
	}
	if got := collectBaselineCounter(t, fx.reader, "eshu_dp_nvd_baseline_trip_latch_total"); got != 1 {
		t.Fatalf("trip latch outcomes = %d, want the one stored_ahead refusal", got)
	}
}

// TestLoadNVDBaselinePlanInputsDrainFailureCountsTimeouts proves a timed-out
// probe warns, counts the timeout apart from a broken query, and leaves the
// year out of the map so the planner holds it as drain_unloaded.
func TestLoadNVDBaselinePlanInputsDrainFailureCountsTimeouts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			Checkpoint:       vulnerability.NVDBaselineCheckpoint{},
			LastScopeID:      "scope-2002",
			LastGenerationID: "gen-2002",
		},
	}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-30 * time.Minute)}
	fx.latch.found = true
	fx.drain.err = &vulnerability.NVDBaselineDrainTimeoutError{Err: errBaselineStoreUnreachable}
	config := baselineTestConfig("run-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	if _, ok := inputs.Drain[2002]; ok {
		t.Fatalf("loadNVDBaselinePlanInputs() drain = %+v, want no entry for the timed-out year", inputs.Drain)
	}
	if !baselineWarned(*fx.messages) {
		t.Fatal("loadNVDBaselinePlanInputs() logged nothing, want a WARN naming the drain failure")
	}
	if got := collectBaselineCounter(t, fx.reader, "eshu_dp_nvd_baseline_drain_probe_timeouts_total"); got != 1 {
		t.Fatalf("drain probe timeouts = %d, want 1", got)
	}
}

// TestReportNVDBaselineHeldChangedWarnsOnce proves the held-years WARN fires
// when the held set moves and stays quiet when it does not.
func TestReportNVDBaselineHeldChangedWarnsOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	observation := vulnerability.IntelligenceDerivationObservation{
		NVDBaselineYearsTotal:         1,
		NVDBaselineDrainUnloadedYears: 1,
	}

	fx.service.reportNVDBaselineHeldChanged(context.Background(), "run-1", observation)
	fx.service.reportNVDBaselineHeldChanged(context.Background(), "run-1", observation)
	if got := len(*fx.messages); got != 1 {
		t.Fatalf("WARN count = %d, want 1 for an unchanged held set, messages = %v", got, *fx.messages)
	}

	observation.NVDBaselineDownstreamFailedYears = 1
	fx.service.reportNVDBaselineHeldChanged(context.Background(), "run-1", observation)
	if got := len(*fx.messages); got != 2 {
		t.Fatalf("WARN count = %d, want 2 after the held set moved, messages = %v", got, *fx.messages)
	}
}

// TestNVDBaselineTickMemoryEvictsOldestRuns proves the per-run memory stays
// bounded: beyond maxNVDBaselineTickMemoryRuns retained run IDs the oldest
// run's held counts and progress snapshots are evicted, and an evicted run
// that returns reports changed instead of resuming silently.
func TestNVDBaselineTickMemoryEvictsOldestRuns(t *testing.T) {
	t.Parallel()

	memory := NewNVDBaselineTickMemory()
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	counts := nvdBaselineHeldCounts{downstream: 1}
	total := maxNVDBaselineTickMemoryRuns + 2
	for i := 0; i < total; i++ {
		runID := fmt.Sprintf("run-%d", i)
		memory.heldChanged(runID, counts)
		memory.pushProgress(runID, vulnerability.NVDBaselineProgressSnapshot{At: now, Done: 1, Total: 2})
	}

	newest := fmt.Sprintf("run-%d", total-1)
	memory.mu.Lock()
	heldLen, progressLen := len(memory.held), len(memory.progress)
	_, evictedHeld := memory.held["run-0"]
	_, keptHeld := memory.held[newest]
	_, evictedProgress := memory.progress["run-0"]
	_, keptProgress := memory.progress[newest]
	memory.mu.Unlock()
	if heldLen != maxNVDBaselineTickMemoryRuns || progressLen != maxNVDBaselineTickMemoryRuns {
		t.Fatalf("retained runs = %d held / %d progress, want %d each", heldLen, progressLen, maxNVDBaselineTickMemoryRuns)
	}
	if evictedHeld || evictedProgress {
		t.Fatal("run-0 still retained, want the oldest run evicted from both maps")
	}
	if !keptHeld || !keptProgress {
		t.Fatalf("newest run %q missing, want it retained in both maps", newest)
	}

	if changed := memory.heldChanged("run-0", counts); len(changed) == 0 {
		t.Fatal("evicted run re-stored identical counts reported nothing, want changed (fail loud, not silent resume)")
	}
	if changed := memory.heldChanged(newest, counts); len(changed) != 0 {
		t.Fatalf("retained run re-stored identical counts reported %v, want nothing", changed)
	}
}

// TestLoadNVDBaselinePlanInputsRecordsETAAndNotDoneAge proves the loader
// records the run-completion ETA from checkpoint progress and the oldest
// not-done chunk age next to its phase.
func TestLoadNVDBaselinePlanInputsRecordsETAAndNotDoneAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{
		2002: {
			Checkpoint:       vulnerability.NVDBaselineCheckpoint{RecordsDone: 100, RecordsTotal: 200},
			LastScopeID:      "scope-2002",
			LastGenerationID: "gen-2002",
		},
	}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: now.Add(-30 * time.Minute)}
	fx.latch.found = true
	fx.drain.states = map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
		{ScopeID: "scope-2002", GenerationID: "gen-2002"}: {Phase: vulnerability.NVDBaselineDrainProjecting, NotDoneSince: now.Add(-2 * time.Hour)},
	}
	config := baselineTestConfig("run-1", 2002)

	fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)
	if _, ok := collectBaselineGauge(t, fx.reader, "eshu_dp_nvd_baseline_eta_seconds", "", ""); ok {
		t.Fatal("ETA recorded on one snapshot, want silence until an hour of history exists")
	}

	later := now.Add(6 * time.Hour)
	fx.sampler.sample = NVDBaselineTripSample{At: later, DirtyProxyRowsPerHour: 100, OldestDirtyAge: time.Hour}
	fx.latch.latch = vulnerability.NVDBaselineTripLatch{SampledAt: later.Add(-30 * time.Minute)}
	fx.drain.states = map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{
		{ScopeID: "scope-2002", GenerationID: "gen-2002"}: {Phase: vulnerability.NVDBaselineDrainProjecting, NotDoneSince: later.Add(-2 * time.Hour)},
	}
	fx.checkpoints.checkpoints[2002] = NVDBaselineStoredCheckpoint{
		Checkpoint:       vulnerability.NVDBaselineCheckpoint{RecordsDone: 160, RecordsTotal: 200},
		LastScopeID:      "scope-2002",
		LastGenerationID: "gen-2002",
	}
	fx.service.Clock = func() time.Time { return later }
	fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", later)

	eta, ok := collectBaselineGauge(t, fx.reader, "eshu_dp_nvd_baseline_eta_seconds", "", "")
	if !ok || eta != 14400 {
		t.Fatalf("ETA = %v, %v, want 14400 (40 remaining at 10 an hour)", eta, ok)
	}
	age, ok := collectBaselineGauge(t, fx.reader, "eshu_dp_nvd_baseline_oldest_not_done_age_seconds", "phase", "projecting")
	if !ok || age != float64(2*time.Hour/time.Second) {
		t.Fatalf("not-done age = %v, %v, want 7200 under phase projecting", age, ok)
	}
}

// errBaselineStoreUnreachable stands in for any unreadable durable probe:
// the loader must fail closed, never plan from the gap.
var errBaselineStoreUnreachable = fmt.Errorf("baseline store unreachable")

// TestServiceTickMapsBaselineStateMintsOneChunk is the loop-level proof: one
// scheduler tick maps live state through the loader, the real planner mints
// at most one chunk, and the plan telemetry records the planned outcome.
func TestServiceTickMapsBaselineStateMintsOneChunk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	instance := testServiceVulnerabilityIntelligenceInstance(now)
	instance.Configuration = `{"targets":[
		{"source":"first_epss","scope_id":"vuln-intel://first/epss","cve_ids":["CVE-2026-0001"]},
		{"source":"nvd","scope_id":"vuln-intel://nvd/cve","cve_ids":["CVE-2026-0001"],"results_per_page":100}
	],"derive_nvd_baseline":{"enabled":true,"baseline_run_id":"loop-1","last_year":2002}}`

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	checkpoints := &fakeNVDBaselineCheckpointReader{checkpoints: map[int]NVDBaselineStoredCheckpoint{}}
	plans := &fakeNVDBaselinePlanReader{plans: map[int]NVDBaselineStoredPlan{}}
	latch := &fakeNVDBaselineTripLatchStore{}
	drain := &fakeNVDBaselineDrainStore{states: map[vulnerability.NVDBaselineDrainKey]vulnerability.NVDBaselineDrainState{}}
	sampler := &fakeNVDBaselineTripSampler{sample: NVDBaselineTripSample{At: now, DirtyProxyRowsPerHour: 100, OldestDirtyAge: time.Hour}}
	var messages []string
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: scope.CollectorVulnerabilityIntelligence,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                            &fakeStore{instances: []workflow.CollectorInstance{instance}},
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{Instruments: instruments},
		NVDBaselineCheckpointReader:      checkpoints,
		NVDBaselinePlanReader:            plans,
		NVDBaselineTripLatchStore:        latch,
		NVDBaselineDrainStore:            drain,
		NVDBaselineTripSampler:           sampler,
		NVDBaselineInstruments:           instruments,
		NVDBaselineTickMemory:            NewNVDBaselineTickMemory(),
		Logger:                           slog.New(recordingLogHandler{messages: &messages}),
		Clock:                            func() time.Time { return now },
	}

	// The minted chunk carries a plan, so the admission takes the
	// plan-recording entry point (#10828): the store fake records plans
	// the way the real one does, and the test pins the handoff.
	store := &baselinePlanFakeStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}, recorded: -1}
	service.Store = store

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if sampler.calls != 1 {
		t.Fatalf("sampler calls = %d, want one hourly sample on the first tick", sampler.calls)
	}
	var baselines []workflow.WorkItem
	for _, item := range store.enqueuedItems {
		if strings.HasPrefix(item.ScopeID, "vuln-intel://nvd/_baseline/loop-1/2002/") {
			baselines = append(baselines, item)
		}
	}
	if got := len(baselines); got != 1 {
		t.Fatalf("baseline items = %d, want the one minted chunk, enqueued = %+v", got, store.enqueuedItems)
	}
	if len(store.calls) != 1 || len(store.calls[0].plans) != 1 || store.calls[0].plans[0].Year != 2002 {
		t.Fatalf("plan calls = %+v, want the one plan-carrying admission for the minted 2002 chunk", store.calls)
	}
	if got := collectBaselineCounter(t, reader, "eshu_dp_nvd_baseline_plans_total"); got != 1 {
		t.Fatalf("baseline plans = %d, want the one planned outcome", got)
	}
	if got := collectBaselineCounter(t, reader, "eshu_dp_nvd_baseline_trip_latch_total"); got != 1 {
		t.Fatalf("trip latch outcomes = %d, want the one applied hourly sample", got)
	}
}

// TestActivationTickMapsBaselineStateMintsOneChunk proves the activation
// path plans from the same loader output: the mapped inputs fed to
// PlanActivationDerivedWork mint the same chunk the in-tree tick mints.
func TestActivationTickMapsBaselineStateMintsOneChunk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	fx := newBaselineServiceTestFixtures(now, "")
	fx.checkpoints.checkpoints = map[int]NVDBaselineStoredCheckpoint{}
	fx.plans.plans = map[int]NVDBaselineStoredPlan{}
	config := baselineTestConfig("loop-1", 2002)

	inputs := fx.service.loadNVDBaselinePlanInputs(context.Background(), config, "instance-1", now)

	request := vulnerability.ActivationDerivedPlanRequest{
		Instance:     workflow.CollectorInstance{InstanceID: "instance-1", CollectorKind: scope.CollectorVulnerabilityIntelligence, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: testVulnerabilityIntelligenceConfiguration(), LastObservedAt: now, CreatedAt: now, UpdatedAt: now},
		ObservedAt:   now,
		PlanKey:      "activation-1",
		SourceSystem: string(scope.CollectorVulnerabilityIntelligence),
		HostScopeID:  "host-scope",
		NVDBaseline:  inputs,
	}
	_, items, _, err := vulnerability.PlanActivationDerivedWork(request)
	if err != nil {
		t.Fatalf("PlanActivationDerivedWork() error = %v, want nil", err)
	}
	if got := len(items); got != 1 {
		t.Fatalf("activation items = %d, want the one minted baseline chunk", got)
	}
	if got := items[0].ScopeID; !strings.HasPrefix(got, "vuln-intel://nvd/_baseline/loop-1/2002/") {
		t.Fatalf("activation scope = %q, want the loop-1 year-2002 chunk", got)
	}
}
