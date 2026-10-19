// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// scriptedMarker is a fake tombstone mark step that records calls.
type scriptedMarker struct {
	calls []pgstorage.RetirementRequest
	order *[]string
	next  pgstorage.RetirementRecord
	err   error
}

func (m *scriptedMarker) InsertPending(_ context.Context, req pgstorage.RetirementRequest) (pgstorage.RetirementRecord, error) {
	m.calls = append(m.calls, req)
	if m.order != nil {
		*m.order = append(*m.order, "mark")
	}
	return m.next, m.err
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
