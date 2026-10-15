// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// The #7597 own-gate producer proofs for both consumers. Each runs the real
// handler or loader with an inactive own generation and a stubbed
// deployment_mapping producer state.

// stubProducerLookup returns one fixed producer state or error.
func stubProducerLookup(state crossrepo.ProducerState, err error) crossrepo.ProducerLookup {
	return func(context.Context, string, string) (crossrepo.ProducerState, error) {
		return state, err
	}
}

// ownGateCase runs one consumer through its own gate.
type ownGateCase struct {
	name           string
	deadLetterCls  string
	isDeferral     func(error) bool
	runWithProduce func(crossrepo.ProducerLookup, time.Time) error
}

func ownGateCases() []ownGateCase {
	return []ownGateCase{
		{
			name:          "deployable_unit_correlation",
			deadLetterCls: crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass,
			isDeferral: func(err error) bool {
				var deferral deployableUnitCorrelationResolutionNotReadyError
				return errors.As(err, &deferral)
			},
			runWithProduce: func(lookup crossrepo.ProducerLookup, cycleStart time.Time) error {
				handler := DeployableUnitCorrelationHandler{
					FactLoader:               &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
					ResolvedLoader:           &stubDeployableUnitResolvedLoader{},
					ResolutionActiveLookup:   stubResolutionActiveLookup(map[string]bool{}),
					ResolutionProducerLookup: lookup,
				}
				intent := deployableUnitIntent("repo:repo-edge-api")
				intent.CycleStartedAt = cycleStart
				_, err := handler.Handle(context.Background(), intent)
				return err
			},
		},
		{
			name:          "workload_materialization",
			deadLetterCls: crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass,
			isDeferral: func(err error) bool {
				var deferral workloadMaterializationResolutionNotReadyError
				return errors.As(err, &deferral)
			},
			runWithProduce: func(lookup crossrepo.ProducerLookup, cycleStart time.Time) error {
				loader := CorrelatedWorkloadProjectionInputLoader{
					FactLoader:               &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()},
					ResolvedLoader:           &stubResolvedRelationshipLoader{},
					ResolutionActiveLookup:   stubResolutionActiveLookup(map[string]bool{}),
					ResolutionProducerLookup: lookup,
				}
				intent := dockerfileLoaderIntent()
				intent.CycleStartedAt = cycleStart
				_, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), intent)
				return err
			},
		},
	}
}

// ownGateProducer is a producer state on the intent's own generation.
func ownGateProducer(generationID, status, class string) crossrepo.ProducerState {
	return crossrepo.ProducerState{
		ScopeActiveGenerationID: generationID, WorkItemID: "dm-7597", Status: status, FailureClass: class,
	}
}

func TestOwnGateDeadLetteredProducerIsACountingErrorNotTheDeferral(t *testing.T) {
	t.Parallel()
	for _, tc := range ownGateCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gen := "generation-1"
			if tc.name == "workload_materialization" {
				gen = dockerfileLoaderIntent().GenerationID
			}
			err := tc.runWithProduce(stubProducerLookup(ownGateProducer(gen, "dead_letter", "retry_exhausted"), nil), time.Now())
			if err == nil {
				t.Fatal("error = nil, want the producer-terminal failure")
			}
			if tc.isDeferral(err) {
				t.Fatalf("error %T is errors.As-matchable to the non-counting deferral; it would never count", err)
			}
			if got := reducerExecutionFailureClass(err); got != tc.deadLetterCls {
				t.Fatalf("failure class = %q, want %q", got, tc.deadLetterCls)
			}
			if !IsRetryable(err) {
				t.Fatal("producer-terminal error is not retryable; it must spend the ordinary budget")
			}
			if !strings.Contains(err.Error(), "dm-7597") {
				t.Fatalf("error %q does not name the producer work item", err)
			}
		})
	}
}

func TestOwnGateNonTerminalProducerKeepsTheDeferral(t *testing.T) {
	t.Parallel()
	for _, tc := range ownGateCases() {
		gen := "generation-1"
		if tc.name == "workload_materialization" {
			gen = dockerfileLoaderIntent().GenerationID
		}
		stale := time.Now().Add(-crossscope.ProducerReadinessMaxWait - time.Hour)
		for name, state := range map[string]crossrepo.ProducerState{
			"retrying":    ownGateProducer(gen, "retrying", crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass),
			"superseded":  ownGateProducer(gen, "superseded", ""),
			"scope moved": {ScopeActiveGenerationID: "generation-newer", WorkItemID: "dm-7597", Status: "dead_letter"},
		} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				err := tc.runWithProduce(stubProducerLookup(state, nil), stale)
				if !tc.isDeferral(err) {
					t.Fatalf("error = %v (%T), want the unchanged non-counting deferral", err, err)
				}
			})
		}
	}
}

func TestOwnGateMissingProducerIsBoundedByElapsedTime(t *testing.T) {
	t.Parallel()
	for _, tc := range ownGateCases() {
		gen := "generation-1"
		if tc.name == "workload_materialization" {
			gen = dockerfileLoaderIntent().GenerationID
		}
		missing := crossrepo.ProducerState{ScopeActiveGenerationID: gen}
		t.Run(tc.name+"/inside the bound", func(t *testing.T) {
			t.Parallel()
			err := tc.runWithProduce(stubProducerLookup(missing, nil), time.Now().Add(-time.Minute))
			if !tc.isDeferral(err) {
				t.Fatalf("error = %v (%T), want the deferral inside the bound", err, err)
			}
		})
		t.Run(tc.name+"/past the bound", func(t *testing.T) {
			t.Parallel()
			err := tc.runWithProduce(stubProducerLookup(missing, nil),
				time.Now().Add(-crossscope.ProducerReadinessMaxWait-time.Minute))
			if err == nil || tc.isDeferral(err) {
				t.Fatalf("error = %v (%T), want a counting failure past the bound", err, err)
			}
			if !IsRetryable(err) || reducerExecutionFailureClass(err) != "reducer_failure" {
				t.Fatalf("error %v: retryable=%v class=%q, want retryable and classless",
					err, IsRetryable(err), reducerExecutionFailureClass(err))
			}
		})
	}
}

func TestOwnGateProducerLookupErrorIsClassifiedNotDeferred(t *testing.T) {
	t.Parallel()
	for _, tc := range ownGateCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.runWithProduce(stubProducerLookup(crossrepo.ProducerState{}, io.ErrUnexpectedEOF), time.Now())
			if tc.isDeferral(err) {
				t.Fatalf("lookup failure folded into the non-counting deferral: %v", err)
			}
			if got := reducerExecutionFailureClass(err); got != "fact_load_transient" {
				t.Fatalf("failure class = %q, want fact_load_transient", got)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("error %v does not carry the lookup cause", err)
			}
		})
	}
}
