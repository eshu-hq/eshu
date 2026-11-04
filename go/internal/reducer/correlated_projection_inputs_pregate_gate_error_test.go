// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// candidateFreeWorkflowLookalikeEnvelopes is a real parser shape that the
// candidate probe answers true for and ExtractWorkloadCandidates rejects:
// parser/content/infer.go tags any path holding ".github" and "workflows" as
// github_actions_workflow, while the extractor requires the
// ".github/workflows/" prefix. The probe is a superset by design, so the
// pre-gate must hold for this shape (#9986 review F1).
func candidateFreeWorkflowLookalikeEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"graph_id": "r1", "name": "R"}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "r1",
			"language":      "yaml",
			"relative_path": "templates/.github/workflows/ci.yml",
			"parsed_file_data": map[string]any{
				"artifact_type": "github_actions_workflow",
			},
		}},
	}
}

// deadLetteredProducer reports the scope's own deployment_mapping producer as
// terminal for the intent's generation.
func deadLetteredProducer(intent reducercontract.Intent) crossrepo.ProducerLookup {
	return func(context.Context, string, string) (crossrepo.ProducerState, error) {
		return crossrepo.ProducerState{
			ScopeActiveGenerationID: intent.GenerationID,
			WorkItemID:              "producer-1",
			Status:                  "dead_letter",
			FailureClass:            "test_terminal",
		}, nil
	}
}

// missingProducer reports no producer row and no relationship generation, the
// state a counted ProducerMissingError is derived from once the intent's cycle
// anchor is older than crossscope.ProducerReadinessMaxWait.
func missingProducer(intent reducercontract.Intent) crossrepo.ProducerLookup {
	return func(context.Context, string, string) (crossrepo.ProducerState, error) {
		return crossrepo.ProducerState{ScopeActiveGenerationID: intent.GenerationID}, nil
	}
}

func agedPastProducerBound(intent reducercontract.Intent) reducercontract.Intent {
	intent.EnqueuedAt = time.Now().UTC().Add(-(crossscope.ProducerReadinessMaxWait + 5*time.Minute))
	return intent
}

func loaderWithTerminalProducer(
	stub *stubFactLoader,
	probe bool,
	producer crossrepo.ProducerLookup,
) CorrelatedWorkloadProjectionInputLoader {
	stub.mayHoldCandidates = probe
	return CorrelatedWorkloadProjectionInputLoader{
		FactLoader:               probeStubLoader{stubFactLoader: stub},
		ResolvedLoader:           &stubResolvedRelationshipLoader{},
		ResolutionActiveLookup:   func(string) (bool, error) { return false, nil },
		ResolutionProducerLookup: producer,
	}
}

// TestPregateProbeFalsePositiveNeverCountsDeadLetteredProducer pins F1: a
// probe false positive on a candidate-free generation must reach the same
// outcome as a false answer. The pre-gate assumes candidates, so it must never
// return a counting verdict; the post-load gate counts on real candidates and
// a candidate-free generation succeeds vacuously.
func TestPregateProbeFalsePositiveNeverCountsDeadLetteredProducer(t *testing.T) {
	t.Parallel()

	if c, _ := ExtractWorkloadCandidates(candidateFreeWorkflowLookalikeEnvelopes()); len(c) != 0 {
		t.Fatalf("fixture is not candidate-free: %v", c)
	}
	intent := dockerfileLoaderIntent()
	for _, probe := range []bool{false, true} {
		stub := &stubFactLoader{envelopes: candidateFreeWorkflowLookalikeEnvelopes()}
		loader := loaderWithTerminalProducer(stub, probe, deadLetteredProducer(intent))

		_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), intent)
		if err != nil {
			t.Fatalf("probe=%v: error = %v, want nil: a candidate-free generation must succeed vacuously", probe, err)
		}
		if stub.calls != 1 {
			t.Fatalf("probe=%v: bulk loads = %d, want 1", probe, stub.calls)
		}
	}
}

// TestPregateProbeFalsePositiveNeverCountsMissingProducerPastBound is the F1
// pin for the other counting verdict: a producer that is missing past
// ProducerReadinessMaxWait.
func TestPregateProbeFalsePositiveNeverCountsMissingProducerPastBound(t *testing.T) {
	t.Parallel()

	intent := agedPastProducerBound(dockerfileLoaderIntent())
	for _, probe := range []bool{false, true} {
		stub := &stubFactLoader{envelopes: candidateFreeWorkflowLookalikeEnvelopes()}
		loader := loaderWithTerminalProducer(stub, probe, missingProducer(intent))

		_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), intent)
		if err != nil {
			t.Fatalf("probe=%v: error = %v, want nil: a candidate-free generation must succeed vacuously", probe, err)
		}
		if stub.calls != 1 {
			t.Fatalf("probe=%v: bulk loads = %d, want 1", probe, stub.calls)
		}
	}
}

// TestPregateCandidateBearingTerminalProducerStillCounts keeps #7597 pinned:
// moving the count to the post-load gate must not stop a candidate-bearing
// scope from counting. With probe=true the load now happens once before the
// count.
func TestPregateCandidateBearingTerminalProducerStillCounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		intent    reducercontract.Intent
		producer  func(reducercontract.Intent) crossrepo.ProducerLookup
		wantClass string
	}{
		{
			name:      "dead_lettered",
			intent:    dockerfileLoaderIntent(),
			producer:  deadLetteredProducer,
			wantClass: crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass,
		},
		{
			name:      "missing_past_bound",
			intent:    agedPastProducerBound(dockerfileLoaderIntent()),
			producer:  missingProducer,
			wantClass: crossrepo.WorkloadMaterializationProducerMissingFailureClass,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, probe := range []bool{false, true} {
				stub := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()}
				loader := loaderWithTerminalProducer(stub, probe, tc.producer(tc.intent))

				_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), tc.intent)
				var classed interface{ FailureClass() string }
				if !errors.As(err, &classed) || classed.FailureClass() != tc.wantClass {
					t.Fatalf("probe=%v: error = %v, want failure class %q", probe, err, tc.wantClass)
				}
				if stub.calls != 1 {
					t.Fatalf("probe=%v: bulk loads = %d, want 1: the count happens after the load", probe, stub.calls)
				}
			}
		})
	}
}

// TestPregateActivationLookupErrorCarriesCause pins the #6730 rule on the
// pre-gate: a failed activation read defers non-counting, and the failure
// travels on the deferral so an outage does not read as healthy backpressure.
func TestPregateActivationLookupErrorCarriesCause(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("activation read failed")
	stub := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes(), mayHoldCandidates: true}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             probeStubLoader{stubFactLoader: stub},
		ResolvedLoader:         &stubResolvedRelationshipLoader{},
		ResolutionActiveLookup: func(string) (bool, error) { return false, lookupErr },
	}

	_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent())
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("error = %v, want workloadMaterializationResolutionNotReadyError", err)
	}
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error = %v, want it to wrap the activation lookup failure", err)
	}
	if stub.calls != 0 {
		t.Fatalf("bulk loads = %d, want 0: an outage defers before the load", stub.calls)
	}
}

// fusedStubResolvedLoader is the production shape: a resolved loader that is a
// CorpusFencedResolvedRelationshipLoader, so the pre-gate does not read the
// corpus fence before the load.
type fusedStubResolvedLoader struct {
	stubResolvedRelationshipLoader
	complete bool
}

func (f *fusedStubResolvedLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	context.Context,
	[]string,
) ([]relationships.ResolvedRelationship, bool, error) {
	return nil, f.complete, nil
}

// TestPregateFusedLoaderClosedCorpusFenceDoesNotShed pins the F2 limit: with
// the fused production loader a closed corpus fence is not read before the
// load. The intent loads once, then defers non-counting on the fused
// statement's own snapshot. Adding a pre-load fence read is tracked in a
// follow-up and must change this test deliberately.
func TestPregateFusedLoaderClosedCorpusFenceDoesNotShed(t *testing.T) {
	t.Parallel()

	stub := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes(), mayHoldCandidates: true}
	fenceLookupReads := 0
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:             probeStubLoader{stubFactLoader: stub},
		ResolvedLoader:         &fusedStubResolvedLoader{complete: false},
		ResolutionActiveLookup: func(string) (bool, error) { return true, nil },
		ResolutionsCompleteLookup: func(context.Context) (bool, error) {
			fenceLookupReads++
			return false, nil
		},
	}

	_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent())
	var deferral workloadMaterializationResolutionNotReadyError
	if !errors.As(err, &deferral) {
		t.Fatalf("error = %v, want the post-load workloadMaterializationResolutionNotReadyError", err)
	}
	if stub.calls != 1 {
		t.Fatalf("bulk loads = %d, want 1: the fused loader does not shed on the corpus fence", stub.calls)
	}
	if fenceLookupReads != 0 {
		t.Fatalf("fence lookup reads = %d, want 0: the fused verdict comes from its own statement", fenceLookupReads)
	}
}
