// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// failureClassOf mirrors the queue's self-classification probe.
func failureClassOf(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}

func retryableOf(err error) bool {
	var retryable interface{ Retryable() bool }
	return errors.As(err, &retryable) && retryable.Retryable()
}

func inactive(string) (bool, error) { return false, nil }

func ownGateIntent(domain reducercontract.Domain, cycleStart time.Time) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: "intent-1", ScopeID: "scope-1", GenerationID: "gen-1",
		Domain: domain, EnqueuedAt: cycleStart, CycleStartedAt: cycleStart,
	}
}

func producerAt(state ProducerState, err error) (ProducerLookup, *int) {
	calls := 0
	return func(_ context.Context, scopeID, generationID string) (ProducerState, error) {
		calls++
		if scopeID != "scope-1" || generationID != "gen-1" {
			return ProducerState{}, errors.New("lookup keyed on the wrong scope generation")
		}
		return state, err
	}, &calls
}

func TestEvaluateOwnGateProducerMatrix(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute)
	stale := now.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	duc := reducercontract.DomainDeployableUnitCorrelation
	wm := reducercontract.DomainWorkloadMaterialization
	current := func(status, class string) ProducerState {
		return ProducerState{ScopeActiveGenerationID: "gen-1", WorkItemID: "dm-1", Status: status, FailureClass: class}
	}

	tests := []struct {
		name       string
		domain     reducercontract.Domain
		cycleStart time.Time
		state      ProducerState
		lookupErr  error
		// wantClass is the counting error's class; "" with wantCount means a
		// classless counting error.
		wantCount  bool
		wantClass  string
		wantDetail []string
	}{
		{
			name: "dead letter counts on duc", domain: duc, cycleStart: fresh,
			state: current("dead_letter", "retry_exhausted"), wantCount: true,
			wantClass: DeployableUnitCorrelationProducerDeadLetteredFailureClass,
		},
		{
			name: "dead letter counts on wm", domain: wm, cycleStart: fresh,
			state: current("dead_letter", "retry_exhausted"), wantCount: true,
			wantClass: WorkloadMaterializationProducerDeadLetteredFailureClass,
		},
		{
			name: "legacy failed counts", domain: duc, cycleStart: fresh,
			state: current("failed", "reducer_failed"), wantCount: true,
			wantClass: DeployableUnitCorrelationProducerDeadLetteredFailureClass,
		},
		{
			name: "retrying producer defers without a bound", domain: duc, cycleStart: stale,
			state:      current("retrying", CrossRepoBackwardEvidenceNotReadyFailureClass),
			wantDetail: []string{"dm-1", "status=retrying", CrossRepoBackwardEvidenceNotReadyFailureClass},
		},
		{
			name: "pending producer defers", domain: wm, cycleStart: stale,
			state: current("pending", ""), wantDetail: []string{"dm-1", "status=pending"},
		},
		{
			name: "running producer defers", domain: wm, cycleStart: fresh,
			state: current("running", ""), wantDetail: []string{"status=running"},
		},
		{
			name: "superseded producer defers", domain: duc, cycleStart: stale,
			state: current("superseded", ""), wantDetail: []string{"status=superseded"},
		},
		{
			name: "scope moved on defers", domain: duc, cycleStart: stale,
			state:      ProducerState{ScopeActiveGenerationID: "gen-2", WorkItemID: "dm-1", Status: "dead_letter"},
			wantDetail: []string{"gen-2"},
		},
		{
			name: "missing producer defers inside the bound", domain: duc, cycleStart: fresh,
			state: ProducerState{ScopeActiveGenerationID: "gen-1"}, wantDetail: []string{"no deployment_mapping"},
		},
		{
			name: "missing producer with unknown anchor defers", domain: duc,
			state: ProducerState{ScopeActiveGenerationID: "gen-1"}, wantDetail: []string{"no deployment_mapping"},
		},
		{
			name: "missing producer counts past the bound", domain: wm, cycleStart: stale,
			state: ProducerState{ScopeActiveGenerationID: "gen-1"}, wantCount: true,
		},
		{
			name: "transient lookup error is classified", domain: duc, cycleStart: fresh,
			lookupErr: io.ErrUnexpectedEOF, wantCount: true, wantClass: "fact_load_transient",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lookup, _ := producerAt(tt.state, tt.lookupErr)
			got := EvaluateOwnGate(context.Background(), inactive, lookup, ownGateIntent(tt.domain, tt.cycleStart), true, now)
			if got.Ready {
				t.Fatal("Ready = true on an inactive own generation")
			}
			if !tt.wantCount {
				if got.Err != nil {
					t.Fatalf("Err = %v, want a deferral", got.Err)
				}
				for _, want := range tt.wantDetail {
					if !strings.Contains(got.ProducerDetail, want) {
						t.Fatalf("ProducerDetail = %q, want it to contain %q", got.ProducerDetail, want)
					}
				}
				return
			}
			if got.Err == nil {
				t.Fatalf("Err = nil (detail %q), want a counting error", got.ProducerDetail)
			}
			if !retryableOf(got.Err) {
				t.Fatalf("Err %v is not Retryable; the ordinary budget must dead-letter it, not the first failure", got.Err)
			}
			if class := failureClassOf(got.Err); class != tt.wantClass {
				t.Fatalf("FailureClass = %q, want %q", class, tt.wantClass)
			}
		})
	}
}

func TestEvaluateOwnGateDeadLetterNamesProducer(t *testing.T) {
	t.Parallel()
	lookup, _ := producerAt(ProducerState{
		ScopeActiveGenerationID: "gen-1", WorkItemID: "dm-7597", Status: "dead_letter", FailureClass: "retry_exhausted",
	}, nil)
	got := EvaluateOwnGate(context.Background(), inactive, lookup,
		ownGateIntent(reducercontract.DomainDeployableUnitCorrelation, time.Now()), true, time.Now())
	for _, want := range []string{"dm-7597", "retry_exhausted", "scope-1", "gen-1"} {
		if got.Err == nil || !strings.Contains(got.Err.Error(), want) {
			t.Fatalf("Err = %v, want it to name %q", got.Err, want)
		}
	}
}

func TestEvaluateOwnGateNonTransientLookupErrorIsNotADeferral(t *testing.T) {
	t.Parallel()
	cause := errors.New("relation fact_work_items does not exist")
	lookup, _ := producerAt(ProducerState{}, cause)
	got := EvaluateOwnGate(context.Background(), inactive, lookup,
		ownGateIntent(reducercontract.DomainWorkloadMaterialization, time.Now()), true, time.Now())
	if !errors.Is(got.Err, cause) {
		t.Fatalf("Err = %v, want the lookup failure, never folded into the non-counting deferral", got.Err)
	}
	if failureClassOf(got.Err) != "" {
		t.Fatalf("lookup failure carries class %q, want none", failureClassOf(got.Err))
	}
}

func TestEvaluateOwnGateSkipsProducerReadOutsideTheDeferringBranch(t *testing.T) {
	t.Parallel()
	now := time.Now()
	intent := ownGateIntent(reducercontract.DomainDeployableUnitCorrelation, now)
	cases := map[string]struct {
		active        func(string) (bool, error)
		hasCandidates bool
		wantReady     bool
	}{
		"active own generation": {active: func(string) (bool, error) { return true, nil }, hasCandidates: true, wantReady: true},
		"no candidates":         {active: inactive, hasCandidates: false, wantReady: true},
		"unwired own lookup":    {active: nil, hasCandidates: true, wantReady: true},
		"own lookup error":      {active: func(string) (bool, error) { return false, errors.New("pk read") }, hasCandidates: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lookup, calls := producerAt(ProducerState{ScopeActiveGenerationID: "gen-1", Status: "dead_letter"}, nil)
			got := EvaluateOwnGate(context.Background(), tc.active, lookup, intent, tc.hasCandidates, now)
			if got.Ready != tc.wantReady || got.Err != nil || got.ProducerDetail != "" {
				t.Fatalf("verdict = %+v, want Ready=%v with no producer evidence", got, tc.wantReady)
			}
			if *calls != 0 {
				t.Fatalf("producer lookup calls = %d, want 0 outside the deferring branch", *calls)
			}
		})
	}

	got := EvaluateOwnGate(context.Background(), inactive, nil, intent, true, now)
	if got.Ready || got.Err != nil || got.ProducerDetail != "" {
		t.Fatalf("unwired producer lookup verdict = %+v, want the plain deferral", got)
	}
}

func TestProducerDeadLetteredFailureClassesAreDistinct(t *testing.T) {
	t.Parallel()
	classes := ProducerDeadLetteredFailureClasses()
	if len(classes) != 2 || classes[0] == classes[1] {
		t.Fatalf("ProducerDeadLetteredFailureClasses() = %v, want two distinct classes", classes)
	}
	for _, class := range classes {
		if strings.HasSuffix(class, "_not_ready") {
			t.Fatalf("%q ends in _not_ready; the enrollment guard would demand it be non-counting", class)
		}
	}
}
