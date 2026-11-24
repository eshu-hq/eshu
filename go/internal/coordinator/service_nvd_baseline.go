// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator // NVD baseline parent-service supply stays on root Service methods with the other vulnerability-intelligence scheduling; the derivation math stays in the vulnerability subpackage (#10746).

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// nvdBaselineTripSampleInterval is how often the service folds a dirty-log
// sample into the trip latch: hourly, inside the latch's 90-minute freshness
// gap, so a live sampler keeps the evidence fresh with one probe an hour.
const nvdBaselineTripSampleInterval = time.Hour

// nvdBaselineDrainLoadSpan names the span around the per-tick drain load,
// the duration signal the planner-slice ruling requires next to the timeout
// count.
const nvdBaselineDrainLoadSpan = "nvd baseline drain load"

// NVDBaselineCheckpointReader is the coordinator's port for the stored NVD
// baseline year checkpoints the planner resumes from. The #10746 service
// maps the checkpoint store rows onto NVDBaselineStoredCheckpoint, keeping
// the planner's position beside the last scope and generation the drain
// probe keys on. A query failure is returned, never answered with unset.
type NVDBaselineCheckpointReader interface {
	ReadNVDBaselineCheckpoints(ctx context.Context, baselineRunID string) (map[int]NVDBaselineStoredCheckpoint, error)
}

// NVDBaselineStoredCheckpoint is one stored year checkpoint with its drain
// key: the planner's (stamp, CVE ID) resume position and progress counters,
// plus the last planned scope and generation the checkpoint commit wrote
// with the position.
type NVDBaselineStoredCheckpoint struct {
	Checkpoint vulnerability.NVDBaselineCheckpoint
	// LastScopeID and LastGenerationID name the year's last committed chunk
	// for the drain probe. Blank means the row predates the columns that
	// carry them: the year probes nothing and holds as drain_unloaded.
	LastScopeID      string
	LastGenerationID string
	// UndrainedGenerations names the year's older undrained windows from
	// the shared undrained-generation set (eshu-hq/eshu#11380): live
	// generations in the year's baseline family beside the last chunk.
	// The drain loader probes them beside the last chunk and folds to one
	// state per year. Empty means the year has no older windows and probes
	// exactly its last chunk, as before.
	UndrainedGenerations []vulnerability.NVDBaselineDrainKey
}

// NVDBaselinePlanReader is the coordinator's port for the per-year plan rows
// the admission transaction writes beside its work items (eshu-hq/eshu#10828):
// each year's last planned scope and time, with whether that scope still
// has an open work item. It is wired in production; a nil reader is
// tolerated only in tests, and an enabled derivation without one holds
// loudly instead of planning blind.
type NVDBaselinePlanReader interface {
	ReadNVDBaselinePlans(ctx context.Context, baselineRunID string) (map[int]NVDBaselineStoredPlan, error)
}

// NVDBaselineStoredPlan is one year plan row: the last planned scope, when
// the admission stamped it, and whether the scope is still in flight.
type NVDBaselineStoredPlan struct {
	LastPlannedScopeID string
	LastPlannedAt      time.Time
	InFlight           bool
}

// NVDBaselineTripSampler is the coordinator's port for the hourly winners
// dirty-log probe the trip latch folds. It holds no state and performs no
// writes: the service samples it hourly and folds the samples through its
// own trip latch.
type NVDBaselineTripSampler interface {
	SampleNVDBaselineTrip(ctx context.Context, now time.Time) (NVDBaselineTripSample, error)
}

// NVDBaselineTripSample is one hourly trip-line observation: the NVD-proxy
// dirty-row rate and the scope-row rate beside it, and the age of the
// oldest pending dirty row. Only the ruled CVE-keyed rate and the age fold
// into the latch; the scope rows ride the sampler's log line.
type NVDBaselineTripSample struct {
	At time.Time
	// DirtyProxyRowsPerHour is the ruled trip rate: CVE-keyed dirty rows
	// captured in the trailing hour.
	DirtyProxyRowsPerHour int64
	// NVDScopeRowsPerHour counts the NVD scope rows captured in the
	// trailing hour, reported next to the ruled rate, never folded.
	NVDScopeRowsPerHour int64
	// GHSAProxyRowsPerHour is the GHSA leg of the NVD/GHSA inflow split
	// (#11078): GHSA-keyed dirty rows captured in the trailing hour,
	// reported next to the ruled rate, never folded.
	GHSAProxyRowsPerHour int64
	// OldestDirtyAge is the age of the oldest row still queued.
	OldestDirtyAge time.Duration
}

// nvdBaselineHeldCounts is one run's held-year counts by closed reason.
type nvdBaselineHeldCounts struct {
	downstream int
	unloaded   int
	retryWait  int
}

// maxNVDBaselineTickMemoryRuns bounds the per-run entries
// NVDBaselineTickMemory retains. Run IDs are operator-configured and rotate
// rarely, so a small bound keeps a rotation with headroom while keeping a
// long-lived Service bounded. An evicted run that returns reports changed
// and rebuilds its ETA instead of resuming silently.
const maxNVDBaselineTickMemoryRuns = 8

// NVDBaselineTickMemory is the service's per-run baseline memory across
// ticks: the last reported held-year counts (so the WARN fires only when
// eshu_dp_nvd_baseline_held_years{reason} moves) and the hourly progress
// snapshots the ETA divides by. It is safe for concurrent use; the Service
// holds one pointer to it, shared across the value copies the scheduler
// passes around. The zero value is usable, and a nil memory degrades
// loudly: held sets report changed and no ETA records.
//
// The memory holds one leaf mutex guarding only the in-memory held and
// progress maps. The lock is never held across I/O, awaits, or nested
// locks: every method locks, copies or stores plain values, and unlocks.
type NVDBaselineTickMemory struct {
	mu       sync.Mutex
	held     map[string]nvdBaselineHeldCounts
	progress map[string][]vulnerability.NVDBaselineProgressSnapshot
	order    []string
}

// retainRun records runID as most recently seen and evicts the oldest runs
// beyond maxNVDBaselineTickMemoryRuns from both maps. The caller must hold
// m.mu.
func (m *NVDBaselineTickMemory) retainRun(runID string) {
	kept := m.order[:0]
	for _, id := range m.order {
		if id != runID {
			kept = append(kept, id)
		}
	}
	kept = append(kept, runID)
	for len(kept) > maxNVDBaselineTickMemoryRuns {
		oldest := kept[0]
		kept = kept[1:]
		delete(m.held, oldest)
		delete(m.progress, oldest)
	}
	m.order = kept
}

// NewNVDBaselineTickMemory constructs the per-run baseline memory.
func NewNVDBaselineTickMemory() *NVDBaselineTickMemory {
	return &NVDBaselineTickMemory{}
}

// heldChanged stores the run's current held counts and reports the reasons
// whose counts moved since the last report. A nil memory reports every
// nonzero reason as changed without storing, so a missing wiring warns
// loudly instead of going quiet.
func (m *NVDBaselineTickMemory) heldChanged(runID string, counts nvdBaselineHeldCounts) []string {
	reasons := heldReasonsChanged(nvdBaselineHeldCounts{}, counts)
	if m == nil {
		return reasons
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.held[runID]
	changed := heldReasonsChanged(previous, counts)
	if m.held == nil {
		m.held = map[string]nvdBaselineHeldCounts{}
	}
	m.held[runID] = counts
	m.retainRun(runID)
	return changed
}

// heldReasonsChanged names the held reasons whose counts differ.
func heldReasonsChanged(previous, current nvdBaselineHeldCounts) []string {
	var changed []string
	if previous.downstream != current.downstream {
		changed = append(changed, telemetry.NVDBaselineHeldYearDownstreamFailed)
	}
	if previous.unloaded != current.unloaded {
		changed = append(changed, telemetry.NVDBaselineHeldYearDrainUnloaded)
	}
	if previous.retryWait != current.retryWait {
		changed = append(changed, telemetry.NVDBaselineHeldYearRetryWait)
	}
	return changed
}

// pushProgress appends one hourly progress snapshot for the run and trims
// snapshots older than the ETA window. A nil memory keeps nothing.
func (m *NVDBaselineTickMemory) pushProgress(runID string, snapshot vulnerability.NVDBaselineProgressSnapshot) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.progress == nil {
		m.progress = map[string][]vulnerability.NVDBaselineProgressSnapshot{}
	}
	kept := append(m.progress[runID], snapshot)
	cutoff := snapshot.At.UTC().Add(-vulnerability.NVDBaselineETAHistoryWindow - time.Hour)
	out := kept[:0]
	for _, keptSnapshot := range kept {
		if keptSnapshot.At.UTC().Before(cutoff) {
			continue
		}
		out = append(out, keptSnapshot)
	}
	if len(out) > 128 {
		out = out[len(out)-128:]
	}
	m.progress[runID] = out
	m.retainRun(runID)
}

// etaSeconds estimates the run's completion ETA from its stored snapshots. A
// nil memory never estimates.
func (m *NVDBaselineTickMemory) etaSeconds(runID string, now time.Time, remaining int64) (float64, bool) {
	if m == nil {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return vulnerability.NVDBaselineETASeconds(m.progress[runID], now, remaining)
}

// loadNVDBaselinePlanInputs maps live NVD baseline state onto the planner's
// NVDBaseline* request fields (eshu-hq/eshu#10746): the validated
// derivation, the stored checkpoints, the per-year in-flight verdicts and
// last-planned times from the plan rows, the trip latch (sampled hourly),
// and the per-year drain states. It is the one loader both planning sites
// (the in-tree instance and the component activation) use.
//
// It never fails the tick. An unreadable input fails closed and rides a
// WARN log naming the cause: a checkpoint or plan failure zeroes the trip
// (the lane holds as trip_unloaded instead of replaying the committed
// prefix from zero), a trip failure keeps the load's own fail-closed latch
// (the lane holds as trip_open), and a drain failure leaves the year out of
// the map (the lane holds that year as drain_unloaded). A disabled
// derivation reads nothing and returns zero inputs.
func (s Service) loadNVDBaselinePlanInputs(ctx context.Context, configDoc, instanceID string, observedAt time.Time) vulnerability.NVDBaselinePlanInputs {
	now := observedAt.UTC()
	derivation, configErr := vulnerability.NVDBaselineDerivationFromConfig(configDoc)
	if configErr != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline configuration invalid, baseline lane held",
			"collector_instance_id", instanceID, "error", configErr.Error())
		return vulnerability.NVDBaselinePlanInputs{ConfigErr: configErr}
	}
	if !derivation.Enabled {
		return vulnerability.NVDBaselinePlanInputs{}
	}
	inputs := vulnerability.NVDBaselinePlanInputs{Derivation: derivation}
	runID := strings.TrimSpace(derivation.RunID)
	if runID == "" {
		// No key for the durable reads. The planner refuses the blank run
		// id as a config-error hold downstream; the loader reads nothing.
		return inputs
	}
	if s.NVDBaselineCheckpointReader == nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline checkpoint reader required, baseline lane held",
			"collector_instance_id", instanceID)
		return inputs
	}
	stored, err := s.NVDBaselineCheckpointReader.ReadNVDBaselineCheckpoints(ctx, runID)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline checkpoints unreadable, baseline lane held",
			"collector_instance_id", instanceID)
		return inputs
	}
	if s.NVDBaselinePlanReader == nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline plan reader required, baseline lane held",
			"collector_instance_id", instanceID)
		return inputs
	}
	planRows, err := s.NVDBaselinePlanReader.ReadNVDBaselinePlans(ctx, runID)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline plan rows unreadable, baseline lane held",
			"collector_instance_id", instanceID)
		return inputs
	}
	inputs.Checkpoints = make(map[int]vulnerability.NVDBaselineCheckpoint, len(stored))
	for year, row := range stored {
		inputs.Checkpoints[year] = row.Checkpoint
	}
	inputs.InFlightYears = make(map[int]bool, len(planRows))
	inputs.LastPlanned = make(map[int]vulnerability.NVDBaselineLastPlan, len(planRows))
	for year, row := range planRows {
		if row.InFlight {
			inputs.InFlightYears[year] = true
		}
		if strings.TrimSpace(row.LastPlannedScopeID) != "" && !row.LastPlannedAt.IsZero() {
			inputs.LastPlanned[year] = vulnerability.NVDBaselineLastPlan{ScopeID: row.LastPlannedScopeID, PlannedAt: row.LastPlannedAt.UTC()}
		}
	}
	inputs.Trip = s.loadNVDBaselineTrip(ctx, instanceID, runID, len(stored) > 0, now)
	inputs.Drain = s.loadNVDBaselineDrainForPlan(ctx, stored)
	s.recordNVDBaselineRunSignals(ctx, runID, inputs, now)
	return inputs
}

// loadNVDBaselineTrip reads the run's trip latch and samples it when the
// stored sample is older than an hour (or missing, or stamped ahead of the
// clock). A sample folds through the guarded write; an applied sample
// re-loads for the planning value. Every failure warns and fails closed:
// an unreadable latch keeps the load's open verdict, a refused sample or a
// re-load failure zeroes the trip so the lane holds as unloaded.
func (s Service) loadNVDBaselineTrip(ctx context.Context, instanceID, runID string, runStarted bool, now time.Time) vulnerability.NVDBaselineTripLoad {
	load, err := vulnerability.LoadNVDBaselineTripLatch(ctx, s.NVDBaselineTripLatchStore, runID)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline trip latch unreadable, baseline lane held open",
			"collector_instance_id", instanceID)
		return load
	}
	if !nvdBaselineTripSampleDue(load, now) {
		return load
	}
	sample, err := s.sampleNVDBaselineTrip(ctx, now)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline trip sample failed, planning from the loaded latch",
			"collector_instance_id", instanceID)
		return load
	}
	observation, err := vulnerability.ObserveNVDBaselineTrip(ctx, s.NVDBaselineTripLatchStore, runID, vulnerability.NVDBaselineTripSample{
		At:                    sample.At,
		DirtyProxyRowsPerHour: sample.DirtyProxyRowsPerHour,
		OldestDirtyAge:        sample.OldestDirtyAge,
	}, runStarted, now)
	s.recordNVDBaselineTripSample(ctx, sample, observation, err == nil)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline trip sample refused, baseline lane held",
			"collector_instance_id", instanceID, "outcome", string(observation.Outcome))
		return vulnerability.NVDBaselineTripLoad{}
	}
	if observation.Outcome != vulnerability.NVDBaselineTripLatchApplied && observation.Outcome != vulnerability.NVDBaselineTripLatchAppliedAfterGap {
		return load
	}
	fresh, err := vulnerability.LoadNVDBaselineTripLatch(ctx, s.NVDBaselineTripLatchStore, runID)
	if err != nil {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline trip latch unreadable after apply, baseline lane held",
			"collector_instance_id", instanceID)
		return vulnerability.NVDBaselineTripLoad{}
	}
	return fresh
}

// nvdBaselineTripSampleDue reports whether the latch needs a new hourly
// sample: no loaded latch, a sample older than the hourly interval, or a
// stored sample stamped ahead of the clock (which the fold refuses loudly
// instead of trusting).
func nvdBaselineTripSampleDue(load vulnerability.NVDBaselineTripLoad, now time.Time) bool {
	if load.Outcome != vulnerability.NVDBaselineTripLatchLoaded {
		return true
	}
	sampled := load.Latch.SampledAt
	if sampled.After(now) {
		return true
	}
	return now.Sub(sampled) >= nvdBaselineTripSampleInterval
}

// sampleNVDBaselineTrip reads one hourly dirty-log sample for the trip
// latch. A missing sampler fails loudly: sampling nothing would read as a
// quiet hour.
func (s Service) sampleNVDBaselineTrip(ctx context.Context, now time.Time) (NVDBaselineTripSample, error) {
	if s.NVDBaselineTripSampler == nil {
		return NVDBaselineTripSample{}, fmt.Errorf("nvd baseline trip sampler is required for the hourly trip sample")
	}
	return s.NVDBaselineTripSampler.SampleNVDBaselineTrip(ctx, now)
}

// recordNVDBaselineTripSample records one folded hourly sample: the dirty
// rate and oldest age gauges on every successful probe, the latch outcome
// of every fold (including a refused one), and a transition for every
// trip-line flip a successful fold made. The caller skips it when the sample
// never probed.
func (s Service) recordNVDBaselineTripSample(ctx context.Context, sample NVDBaselineTripSample, observation vulnerability.NVDBaselineTripObservation, succeeded bool) {
	inst := s.NVDBaselineInstruments
	inst.RecordNVDBaselineDirtySample(ctx, sample.DirtyProxyRowsPerHour, sample.GHSAProxyRowsPerHour, sample.OldestDirtyAge.Seconds())
	inst.RecordNVDBaselineTripLatch(ctx, string(observation.Outcome))
	if !succeeded {
		return
	}
	if !observation.Before.Open && observation.After.Open {
		inst.RecordNVDBaselineTripTransition(ctx, telemetry.NVDBaselineTripOpened)
	}
	if !observation.Before.Page && observation.After.Page {
		inst.RecordNVDBaselineTripTransition(ctx, telemetry.NVDBaselineTripPaged)
	}
	if !observation.Before.ReEnableEligible && observation.After.ReEnableEligible {
		inst.RecordNVDBaselineTripTransition(ctx, telemetry.NVDBaselineTripEligible)
	}
}

// recordNVDBaselineRunSignals records the run-level signals the loader owns:
// the hourly progress snapshot the ETA divides by, the ETA itself once an
// hour of history exists, and the oldest not-done chunk age next to its
// phase.
func (s Service) recordNVDBaselineRunSignals(ctx context.Context, runID string, inputs vulnerability.NVDBaselinePlanInputs, now time.Time) {
	remaining, started := vulnerability.NVDBaselineETARemaining(inputs.Checkpoints)
	if started {
		var done, total int64
		for _, checkpoint := range inputs.Checkpoints {
			if checkpoint.RecordsTotal > 0 {
				done += checkpoint.RecordsDone
				total += checkpoint.RecordsTotal
			}
		}
		s.NVDBaselineTickMemory.pushProgress(runID, vulnerability.NVDBaselineProgressSnapshot{At: now, Done: done, Total: total})
		if seconds, ok := s.NVDBaselineTickMemory.etaSeconds(runID, now, remaining); ok {
			s.NVDBaselineInstruments.RecordNVDBaselineETA(ctx, seconds)
		}
	}
	if age, phase, ok := vulnerability.NVDBaselineOldestNotDone(inputs.Drain, now); ok {
		s.NVDBaselineInstruments.RecordNVDBaselineOldestNotDoneAge(ctx, string(phase), age.Seconds())
	}
}

// reportNVDBaselineHeldChanged logs a WARN when the held-year counts moved
// since the last plan for the run, so a year the stored state newly holds
// (or newly releases) wakes the operator without spamming every tick. It
// reports nothing when the plan computed no baseline years (a disabled
// derivation or a config-error plan).
func (s Service) reportNVDBaselineHeldChanged(ctx context.Context, runID string, observation vulnerability.IntelligenceDerivationObservation) {
	if observation.NVDBaselineYearsTotal == 0 {
		return
	}
	counts := nvdBaselineHeldCounts{
		downstream: observation.NVDBaselineDownstreamFailedYears,
		unloaded:   observation.NVDBaselineDrainUnloadedYears,
		retryWait:  observation.NVDBaselineRetryWaitYears,
	}
	if changed := s.NVDBaselineTickMemory.heldChanged(runID, counts); len(changed) > 0 {
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline held years changed",
			"baseline_run_id", runID,
			telemetry.NVDBaselineHeldYearDownstreamFailed, counts.downstream,
			telemetry.NVDBaselineHeldYearDrainUnloaded, counts.unloaded,
			telemetry.NVDBaselineHeldYearRetryWait, counts.retryWait)
	}
}

// warnNVDBaseline logs one baseline supply line at WARN. It skips silently
// without a logger, so tests that do not care about logs construct the
// Service without one.
func (s Service) warnNVDBaseline(ctx context.Context, message string, attrs ...any) {
	if s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, message, attrs...)
}
