// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// scriptedMarker is a fake tombstone mark step that records calls.
type scriptedMarker struct {
	calls []pgstorage.RetirementRequest
	order *[]string
	next  pgstorage.RetirementRecord
	err   error
	// failRepos, when non-nil, fails the mark for exactly those repo ids
	// while the other repositories mark normally: the partial-failure
	// path a multi-repository trigger hits mid-loop.
	failRepos map[string]error
	// records, when non-nil, answers the mark per repo id instead of the
	// single next record: a same-key retry resumes an existing row id for
	// an already-marked repository.
	records map[string]pgstorage.RetirementRecord
}

func (m *scriptedMarker) InsertPending(_ context.Context, req pgstorage.RetirementRequest) (pgstorage.RetirementRecord, error) {
	m.calls = append(m.calls, req)
	if m.order != nil {
		*m.order = append(*m.order, "mark")
	}
	if err, ok := m.failRepos[req.RepoID]; ok {
		return pgstorage.RetirementRecord{}, err
	}
	if rec, ok := m.records[req.RepoID]; ok {
		return rec, nil
	}
	return m.next, m.err
}

// countingInstruments wires the trigger outcome counter to a manual reader
// so funnel tests assert per-outcome counts instead of counter silence.
func countingInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("trigger-funnel-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// outcomeCounts collects eshu_dp_repository_retirements_total by outcome.
func outcomeCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_repository_retirements_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, ok := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				if !ok {
					t.Fatalf("datapoint missing %q attribute", telemetry.MetricDimensionOutcome)
				}
				counts[outcome.AsString()] += dp.Value
			}
		}
	}
	return counts
}

// terminalCounts sums the terminal outcomes: every marked repository
// carries exactly one, so the funnel holds when terminals equal accepted.
func terminalCounts(counts map[string]int64) int64 {
	return counts[TriggerOutcomeCompleted] + counts[TriggerOutcomeBlocked] +
		counts[TriggerOutcomeRefused] + counts[TriggerOutcomeFailed]
}

// scriptedFencer is a fake phase-1 fence that records calls.
type scriptedFencer struct {
	calls []pgstorage.FenceRequest
	order *[]string
	next  pgstorage.FenceResult
	err   error
}

func (f *scriptedFencer) FenceRepository(_ context.Context, req pgstorage.FenceRequest) (pgstorage.FenceResult, error) {
	f.calls = append(f.calls, req)
	if f.order != nil {
		*f.order = append(*f.order, "fence")
	}
	return f.next, f.err
}

// scriptedPurger is a fake narrowed purge that records calls.
type scriptedPurger struct {
	calls []pgstorage.PurgeRequest
	order *[]string
	next  pgstorage.PurgeResult
	err   error
}

func (p *scriptedPurger) PurgeRetired(_ context.Context, req pgstorage.PurgeRequest) (pgstorage.PurgeResult, error) {
	p.calls = append(p.calls, req)
	if p.order != nil {
		*p.order = append(*p.order, "purge")
	}
	return p.next, p.err
}

func triggerFixture() (*Trigger, *scriptedMarker, *scriptedFencer, *scriptedPurger) {
	var order []string
	marker := &scriptedMarker{order: &order, next: pgstorage.RetirementRecord{
		RetirementID: "rr_trigger01", RepoID: "PARTITION:trigger-repo",
		ScopeID: "trigger-scope-main", State: pgstorage.RetirementStatePending,
		Phase: pgstorage.RetirementPhaseFenced,
	}}
	fencer := &scriptedFencer{order: &order, next: pgstorage.FenceResult{
		RetirementIDs: []string{"rr_trigger01"}, RepoIDs: []string{"PARTITION:trigger-repo"},
	}}
	purger := &scriptedPurger{order: &order, next: pgstorage.PurgeResult{
		Completed: []string{"PARTITION:trigger-repo"},
	}}
	trigger := &Trigger{Marker: marker, Fencer: fencer, Purger: purger}
	return trigger, marker, fencer, purger
}

func triggerInput() TriggerRequest {
	return TriggerRequest{
		Retirements: []TriggerRetirement{{
			RetirementID: "rr_trigger01", RepoID: "PARTITION:trigger-repo", ScopeID: "trigger-scope-main",
		}},
		Reason:         "deselected repository",
		ActorClass:     "operator",
		IdempotencyKey: "key-8602-01",
	}
}

// TestTriggerRunsMarkFencePurgeInOrder is the #8602 acceptance core: one
// operator trigger marks, then fences, then purges, and the purge sees the
// fenced repositories.
func TestTriggerRunsMarkFencePurgeInOrder(t *testing.T) {
	trigger, marker, fencer, purger := triggerFixture()
	result, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if err != nil {
		t.Fatalf("TriggerRetirement = %v, want nil", err)
	}
	if len(marker.calls) != 1 {
		t.Fatalf("mark calls = %d, want 1", len(marker.calls))
	}
	if len(fencer.calls) != 1 {
		t.Fatalf("fence calls = %d, want 1", len(fencer.calls))
	}
	if len(purger.calls) != 1 {
		t.Fatalf("purge calls = %d, want 1", len(purger.calls))
	}
	if got, want := *marker.order, []string{"mark", "fence", "purge"}; !slices.Equal(got, want) {
		t.Fatalf("step order = %v, want %v", got, want)
	}
	if got := purger.calls[0].RepoIDs; !slices.Equal(got, []string{"PARTITION:trigger-repo"}) {
		t.Fatalf("purge repos = %v, want the fenced repository", got)
	}
	if len(result.Repos) != 1 || result.Repos[0].Outcome != TriggerOutcomeCompleted {
		t.Fatalf("result = %+v, want one completed repository", result)
	}
	if result.Repos[0].RetirementID != "rr_trigger01" {
		t.Fatalf("retirement id = %q, want rr_trigger01", result.Repos[0].RetirementID)
	}
}

// TestTriggerStopsBeforeFenceWhenMarkFails proves the fence never runs on an
// unmarked repository: a mark error returns before any fence or purge call.
func TestTriggerStopsBeforeFenceWhenMarkFails(t *testing.T) {
	trigger, _, fencer, purger := triggerFixture()
	boom := errors.New("retirement insert: connection refused")
	trigger.Marker = &scriptedMarker{err: boom}
	_, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the mark error", err)
	}
	if len(fencer.calls) != 0 {
		t.Fatalf("fence calls = %d, want 0", len(fencer.calls))
	}
	if len(purger.calls) != 0 {
		t.Fatalf("purge calls = %d, want 0", len(purger.calls))
	}
}

// TestTriggerStopsBeforePurgeWhenFenceBlocked proves a fenced-before-purge
// order: a fence refusal reports every repository refused and never purges.
func TestTriggerStopsBeforePurgeWhenFenceBlocked(t *testing.T) {
	trigger, _, _, purger := triggerFixture()
	trigger.Fencer = &scriptedFencer{err: &pgstorage.FenceBlockedError{
		Reason: pgstorage.FenceReasonProjectorLeaseLive, RepoIDs: []string{"PARTITION:trigger-repo"},
	}}
	result, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if err == nil {
		t.Fatal("err = nil, want the fence refusal")
	}
	if len(purger.calls) != 0 {
		t.Fatalf("purge calls = %d, want 0", len(purger.calls))
	}
	if len(result.Repos) != 1 || result.Repos[0].Outcome != TriggerOutcomeRefused {
		t.Fatalf("result = %+v, want one refused repository", result)
	}
}

// TestTriggerReportsBlockedPurgeWithoutError proves the live-generation
// refusal: a terminal_not_reached purge exit is a blocked outcome on the
// result, not an error, and the fenced tombstone stays open for a later pass.
func TestTriggerReportsBlockedPurgeWithoutError(t *testing.T) {
	trigger, _, _, _ := triggerFixture()
	trigger.Purger = &scriptedPurger{next: pgstorage.PurgeResult{
		Blocked: []pgstorage.PurgeBlocked{{
			RepoID: "PARTITION:trigger-repo", RetirementID: "rr_trigger01",
			Reason: pgstorage.PurgeReasonTerminalNotReached, Statuses: "active:1",
		}},
	}}
	result, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if err != nil {
		t.Fatalf("TriggerRetirement = %v, want nil for a designed blocked exit", err)
	}
	if len(result.Repos) != 1 || result.Repos[0].Outcome != TriggerOutcomeBlocked {
		t.Fatalf("result = %+v, want one blocked repository", result)
	}
	if result.Repos[0].BlockedReason != pgstorage.PurgeReasonTerminalNotReached {
		t.Fatalf("blocked reason = %q, want terminal_not_reached", result.Repos[0].BlockedReason)
	}
}

// TestTriggerSurfacesUnfencedPurgeRefusal proves an unfenced repository
// still refuses: a purge with no open tombstone is a failed outcome plus the
// purger's error, never a silent completion.
func TestTriggerSurfacesUnfencedPurgeRefusal(t *testing.T) {
	trigger, marker, _, _ := triggerFixture()
	marker.next = pgstorage.RetirementRecord{}
	trigger.Purger = &scriptedPurger{err: fmt.Errorf("retirement purge %s: no open tombstone", "PARTITION:trigger-repo")}
	result, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if err == nil {
		t.Fatal("err = nil, want the unfenced purge refusal")
	}
	if len(result.Repos) != 1 || result.Repos[0].Outcome != TriggerOutcomeFailed {
		t.Fatalf("result = %+v, want one failed repository", result)
	}
}

// TestTriggerValidatesRequest proves malformed operator input never reaches
// the store: every invalid shape returns before any mark, fence, or purge.
func TestTriggerValidatesRequest(t *testing.T) {
	cases := map[string]func(*TriggerRequest){
		"no repositories": func(r *TriggerRequest) { r.Retirements = nil },
		"blank reason":    func(r *TriggerRequest) { r.Reason = "  " },
		"blank key":       func(r *TriggerRequest) { r.IdempotencyKey = "" },
		"blank repo id":   func(r *TriggerRequest) { r.Retirements[0].RepoID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			trigger, marker, fencer, purger := triggerFixture()
			in := triggerInput()
			mutate(&in)
			if _, err := trigger.TriggerRetirement(context.Background(), in); err == nil {
				t.Fatal("err = nil, want a validation error")
			}
			if len(marker.calls)+len(fencer.calls)+len(purger.calls) != 0 {
				t.Fatal("store saw calls for invalid input")
			}
		})
	}
}

// TestTriggerMarksDuplicateOnResumedRow proves same-key retry resumes: when
// the mark returns an existing row id instead of the requested one, the
// result reports the retry as a duplicate of the open retirement.
func TestTriggerMarksDuplicateOnResumedRow(t *testing.T) {
	trigger, marker, fencer, _ := triggerFixture()
	marker.next.RetirementID = "rr_existing09"
	// A same-key retry resumes the open row at every step: the fence
	// marker shares the tombstone insert, so it returns the resumed row
	// too.
	fencer.next.RetirementIDs = []string{"rr_existing09"}
	result, err := trigger.TriggerRetirement(context.Background(), triggerInput())
	if err != nil {
		t.Fatalf("TriggerRetirement = %v, want nil", err)
	}
	if !result.Duplicate {
		t.Fatal("Duplicate = false, want true for a resumed open row")
	}
	if result.Repos[0].RetirementID != "rr_existing09" {
		t.Fatalf("retirement id = %q, want the resumed row", result.Repos[0].RetirementID)
	}
}

// TestTriggerValidationFailureCountsNothing pins the line-147 shape of the
// #8626 P2: a request refused before any mark attempted no repository, so
// no repository gains a terminal count.
func TestTriggerValidationFailureCountsNothing(t *testing.T) {
	inst, reader := countingInstruments(t)
	trigger, _, _, _ := triggerFixture()
	trigger.Instruments = inst
	in := triggerInput()
	in.Reason = "  "
	if _, err := trigger.TriggerRetirement(context.Background(), in); err == nil {
		t.Fatal("err = nil, want a validation error")
	}
	if counts := outcomeCounts(t, reader); len(counts) != 0 {
		t.Fatalf("counts = %v, want no counts: validation marked nothing", counts)
	}
}

// TestTriggerSameKeyRetryPreservesFunnel drives the partial failure and its
// same-key retry through one reader: the first attempt marks repository A
// and fails on B (accepted 1, failed 1); the retry resumes A's open row,
// marks B, and completes both. Cumulative accepted must equal cumulative
// terminals, or the funnel observably diverges across the retry.
func TestTriggerSameKeyRetryPreservesFunnel(t *testing.T) {
	inst, reader := countingInstruments(t)
	boom := errors.New("retirement insert: connection refused")

	// Attempt one: A marks, B fails. Fence and purge must not run.
	first := &Trigger{
		Marker: &scriptedMarker{
			next:      pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{"PARTITION:part-repo-b": boom},
		},
		Fencer:      &scriptedFencer{},
		Purger:      &scriptedPurger{},
		Instruments: inst,
	}
	if _, err := first.TriggerRetirement(context.Background(), twoRepoInput()); !errors.Is(err, boom) {
		t.Fatalf("first err = %v, want the mark error", err)
	}

	// Attempt two (same key): A resumes its open row, B marks, then fence
	// and purge complete both repositories.
	retry := &Trigger{
		Marker: &scriptedMarker{records: map[string]pgstorage.RetirementRecord{
			"PARTITION:part-repo-a": {RetirementID: "rr_existing_a", State: pgstorage.RetirementStatePending},
			"PARTITION:part-repo-b": {RetirementID: "rr_part02", State: pgstorage.RetirementStatePending},
		}},
		Fencer: &scriptedFencer{next: pgstorage.FenceResult{
			RetirementIDs: []string{"rr_existing_a", "rr_part02"},
			RepoIDs:       []string{"PARTITION:part-repo-a", "PARTITION:part-repo-b"},
		}},
		Purger: &scriptedPurger{next: pgstorage.PurgeResult{
			Completed: []string{"PARTITION:part-repo-a", "PARTITION:part-repo-b"},
		}},
		Instruments: inst,
	}
	result, err := retry.TriggerRetirement(context.Background(), twoRepoInput())
	if err != nil {
		t.Fatalf("retry TriggerRetirement = %v, want nil", err)
	}
	if !result.Duplicate {
		t.Fatal("Duplicate = false, want true for the resumed open row")
	}
	if len(result.Repos) != 2 {
		t.Fatalf("repos = %+v, want both repositories completed", result.Repos)
	}

	counts := outcomeCounts(t, reader)
	if got, want := counts[TriggerOutcomeAccepted], int64(3); got != want {
		t.Fatalf("accepted = %d, want %d (first attempt plus both retry marks)", got, want)
	}
	if got, want := counts[TriggerOutcomeCompleted], int64(2); got != want {
		t.Fatalf("completed = %d, want %d", got, want)
	}
	if got, want := counts[TriggerOutcomeFailed], int64(1); got != want {
		t.Fatalf("failed = %d, want %d (only the first attempt's marked repository)", got, want)
	}
	if got, want := terminalCounts(counts), counts[TriggerOutcomeAccepted]; got != want {
		t.Fatalf("terminals = %d, accepted = %d: same-key retry broke the funnel", got, want)
	}

	// Conflict arm (#8614): the first attempt marks repository A and meets
	// B's foreign open row under a different key, so both entries report
	// refused. The retry runs after that foreign row resolves: A resumes
	// its open row, B marks fresh, and fence plus purge complete both.
	conflictInst, conflictReader := countingInstruments(t)
	conflictFirst := &Trigger{
		Marker: &scriptedMarker{
			next:      pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{"PARTITION:part-repo-b": pgstorage.ErrRetirementAlreadyOpen},
		},
		Fencer:      &scriptedFencer{},
		Purger:      &scriptedPurger{},
		Instruments: conflictInst,
	}
	conflictPartial, err := conflictFirst.TriggerRetirement(context.Background(), twoRepoInput())
	if !errors.Is(err, pgstorage.ErrRetirementAlreadyOpen) {
		t.Fatalf("conflict first err = %v, want the mark conflict", err)
	}
	if len(conflictPartial.Repos) != 2 ||
		conflictPartial.Repos[0].Outcome != TriggerOutcomeRefused ||
		conflictPartial.Repos[1].Outcome != TriggerOutcomeRefused {
		t.Fatalf("conflict partial = %+v, want both repositories refused", conflictPartial.Repos)
	}
	conflictRetry := &Trigger{
		Marker: &scriptedMarker{records: map[string]pgstorage.RetirementRecord{
			"PARTITION:part-repo-a": {RetirementID: "rr_existing_a", State: pgstorage.RetirementStatePending},
		}, next: pgstorage.RetirementRecord{RetirementID: "rr_part02", State: pgstorage.RetirementStatePending}},
		Fencer: &scriptedFencer{next: pgstorage.FenceResult{
			RetirementIDs: []string{"rr_existing_a", "rr_part02"},
			RepoIDs:       []string{"PARTITION:part-repo-a", "PARTITION:part-repo-b"},
		}},
		Purger: &scriptedPurger{next: pgstorage.PurgeResult{
			Completed: []string{"PARTITION:part-repo-a", "PARTITION:part-repo-b"},
		}},
		Instruments: conflictInst,
	}
	conflictResult, err := conflictRetry.TriggerRetirement(context.Background(), twoRepoInput())
	if err != nil {
		t.Fatalf("conflict retry TriggerRetirement = %v, want nil", err)
	}
	if !conflictResult.Duplicate {
		t.Fatal("conflict retry Duplicate = false, want true for the resumed open row")
	}
	if len(conflictResult.Repos) != 2 ||
		conflictResult.Repos[0].Outcome != TriggerOutcomeCompleted ||
		conflictResult.Repos[1].Outcome != TriggerOutcomeCompleted {
		t.Fatalf("conflict retry repos = %+v, want both repositories completed", conflictResult.Repos)
	}
	conflictCounts := outcomeCounts(t, conflictReader)
	if got, want := conflictCounts[TriggerOutcomeAccepted], int64(3); got != want {
		t.Fatalf("conflict accepted = %d, want %d (first attempt plus both retry marks)", got, want)
	}
	if got, want := conflictCounts[TriggerOutcomeRefused], int64(1); got != want {
		t.Fatalf("conflict refused = %d, want %d (only the first attempt's marked repository)", got, want)
	}
	if got, want := conflictCounts[TriggerOutcomeCompleted], int64(2); got != want {
		t.Fatalf("conflict completed = %d, want %d", got, want)
	}
	if got, want := terminalCounts(conflictCounts), conflictCounts[TriggerOutcomeAccepted]; got != want {
		t.Fatalf("conflict terminals = %d, accepted = %d: same-key retry broke the funnel", got, want)
	}
}
