// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

// The per-producer decision rule: which declared producers a pass is still
// waiting on, given the pre-load readiness answers and the post-load evidence.
// The rest of the floor -- ordering, the elapsed bound, the disabled cases --
// lives in readiness_floor_test.go.

import (
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestUnreadyProducersEvaluatesEachProducerSeparately drives the production
// decision rule over a two-producer consumer, which is where the aggregate
// version was wrong.
//
// The load-bearing row is "one producer's evidence twice": that is what an
// aggregate count could not tell apart from both producers answering once, and
// it is how supply_chain_impact came to commit findings with no deployment
// context (found reviewing #6093).
func TestUnreadyProducersEvaluatesEachProducerSeparately(t *testing.T) {
	t.Parallel()

	producers := []reducercontract.Domain{
		reducercontract.DomainContainerImageIdentity, reducercontract.DomainCICDRunCorrelation,
	}
	for _, testCase := range []struct {
		name     string
		ready    ProducerReadinessByDomain
		resolved map[reducercontract.Domain]int
		want     []reducercontract.Domain
	}{
		{
			name: "one producer's evidence twice leaves the other one waiting",
			ready: ProducerReadinessByDomain{
				reducercontract.DomainContainerImageIdentity: false, reducercontract.DomainCICDRunCorrelation: false,
			},
			resolved: map[reducercontract.Domain]int{reducercontract.DomainContainerImageIdentity: 2},
			want:     []reducercontract.Domain{reducercontract.DomainCICDRunCorrelation},
		},
		{
			name: "each producer answered once",
			ready: ProducerReadinessByDomain{
				reducercontract.DomainContainerImageIdentity: false, reducercontract.DomainCICDRunCorrelation: false,
			},
			resolved: map[reducercontract.Domain]int{
				reducercontract.DomainContainerImageIdentity: 1, reducercontract.DomainCICDRunCorrelation: 1,
			},
			want: nil,
		},
		{
			name: "a ready producer needs no evidence",
			ready: ProducerReadinessByDomain{
				reducercontract.DomainContainerImageIdentity: true, reducercontract.DomainCICDRunCorrelation: true,
			},
			want: nil,
		},
		{
			name: "readiness covers one producer, evidence covers the other",
			ready: ProducerReadinessByDomain{
				reducercontract.DomainContainerImageIdentity: true, reducercontract.DomainCICDRunCorrelation: false,
			},
			resolved: map[reducercontract.Domain]int{reducercontract.DomainCICDRunCorrelation: 1},
			want:     nil,
		},
		{
			name: "neither ready nor resolved reports both",
			ready: ProducerReadinessByDomain{
				reducercontract.DomainContainerImageIdentity: false, reducercontract.DomainCICDRunCorrelation: false,
			},
			want: []reducercontract.Domain{
				reducercontract.DomainContainerImageIdentity, reducercontract.DomainCICDRunCorrelation,
			},
		},
		{
			// A store that forgets a declared producer must cost a bounded
			// deferral, never a wrong answer. The pass still commits if that
			// producer demonstrably wrote output.
			name:  "a producer the store said nothing about reads as unready",
			ready: ProducerReadinessByDomain{reducercontract.DomainContainerImageIdentity: true},
			want:  []reducercontract.Domain{reducercontract.DomainCICDRunCorrelation},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := UnreadyProducers(ProducerReadinessSignal{
				readyByProducer: testCase.ready,
				ProducerDomains: producers,
			}, testCase.resolved)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("UnreadyProducers() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestUnreadyProducersStaysSilentWhenTheGateIsDisabled keeps the disabled
// cases — unwired seam, unregistered domain, nothing to look up, elapsed
// bound reached — deciding nothing, whatever the counts say.
func TestUnreadyProducersStaysSilentWhenTheGateIsDisabled(t *testing.T) {
	t.Parallel()

	got := UnreadyProducers(ProducerReadinessSignal{
		gateDisabled:    true,
		ProducerDomains: []reducercontract.Domain{reducercontract.DomainContainerImageIdentity},
	}, nil)
	if len(got) != 0 {
		t.Fatalf("UnreadyProducers() = %v, want none when the gate is disabled", got)
	}
}

// TestCICDRunCorrelationDeclaresExactlyOneProducer is the guard
// SingleProducerResolvedCounts depends on. That adapter credits a whole-batch
// count to one producer, which is only sound because this consumer reads a
// dedicated producer reader. A second declared producer makes the count
// ambiguous again.
func TestCICDRunCorrelationDeclaresExactlyOneProducer(t *testing.T) {
	t.Parallel()

	dependencies := DependenciesForRegistration(reducercontract.DomainCICDRunCorrelation)
	if len(dependencies) != 1 || len(dependencies[0].ProducerDomains) != 1 {
		t.Fatalf(
			"ci_cd_run_correlation producers = %v, want exactly one: its whole-batch count cannot be split otherwise",
			dependencies,
		)
	}
}

// TestSingleProducerResolvedCountsRefusesAMultiProducerConsumer pins the safe
// direction on catalog drift. Spreading one count across producers it may not
// have come from is the defect this rule removes, so the adapter credits
// nothing instead and the consumer takes a bounded deferral.
func TestSingleProducerResolvedCountsRefusesAMultiProducerConsumer(t *testing.T) {
	t.Parallel()

	got := SingleProducerResolvedCounts(
		[]reducercontract.Domain{reducercontract.DomainContainerImageIdentity, reducercontract.DomainCICDRunCorrelation}, 5,
	)
	if len(got) != 0 {
		t.Fatalf("SingleProducerResolvedCounts() = %v, want nothing credited for a two-producer consumer", got)
	}
	if credited := SingleProducerResolvedCounts(
		[]reducercontract.Domain{reducercontract.DomainContainerImageIdentity}, 5,
	); credited[reducercontract.DomainContainerImageIdentity] != 5 {
		t.Fatalf("SingleProducerResolvedCounts() = %v, want the batch count credited to the sole producer", credited)
	}
}
