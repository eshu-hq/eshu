// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// The per-producer decision rule: which declared producers a pass is still
// waiting on, given the pre-load readiness answers and the post-load evidence.
// The rest of the floor -- ordering, the elapsed bound, the disabled cases --
// lives in cross_scope_readiness_floor_test.go.

import (
	"slices"
	"testing"
)

// TestCrossScopeUnreadyProducersEvaluatesEachProducerSeparately drives the
// production decision rule over a two-producer consumer, which is where the
// aggregate version was wrong.
//
// The load-bearing row is "one producer's evidence twice": that is what an
// aggregate count could not tell apart from both producers answering once, and
// it is how supply_chain_impact came to commit findings with no deployment
// context (found reviewing #6093).
func TestCrossScopeUnreadyProducersEvaluatesEachProducerSeparately(t *testing.T) {
	t.Parallel()

	producers := []Domain{DomainContainerImageIdentity, DomainCICDRunCorrelation}
	for _, testCase := range []struct {
		name     string
		ready    CrossScopeProducerReadinessByDomain
		resolved map[Domain]int
		want     []Domain
	}{
		{
			name:     "one producer's evidence twice leaves the other one waiting",
			ready:    CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: false, DomainCICDRunCorrelation: false},
			resolved: map[Domain]int{DomainContainerImageIdentity: 2},
			want:     []Domain{DomainCICDRunCorrelation},
		},
		{
			name:     "each producer answered once",
			ready:    CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: false, DomainCICDRunCorrelation: false},
			resolved: map[Domain]int{DomainContainerImageIdentity: 1, DomainCICDRunCorrelation: 1},
			want:     nil,
		},
		{
			name:  "a ready producer needs no evidence",
			ready: CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: true, DomainCICDRunCorrelation: true},
			want:  nil,
		},
		{
			name:     "readiness covers one producer, evidence covers the other",
			ready:    CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: true, DomainCICDRunCorrelation: false},
			resolved: map[Domain]int{DomainCICDRunCorrelation: 1},
			want:     nil,
		},
		{
			name:  "neither ready nor resolved reports both",
			ready: CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: false, DomainCICDRunCorrelation: false},
			want:  []Domain{DomainContainerImageIdentity, DomainCICDRunCorrelation},
		},
		{
			// A store that forgets a declared producer must cost a bounded
			// deferral, never a wrong answer. The pass still commits if that
			// producer demonstrably wrote output.
			name:  "a producer the store said nothing about reads as unready",
			ready: CrossScopeProducerReadinessByDomain{DomainContainerImageIdentity: true},
			want:  []Domain{DomainCICDRunCorrelation},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := crossScopeUnreadyProducers(crossScopeProducerReadinessSignal{
				readyByProducer: testCase.ready,
				producerDomains: producers,
			}, testCase.resolved)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("crossScopeUnreadyProducers() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestCrossScopeUnreadyProducersStaysSilentWhenTheGateIsDisabled keeps the
// disabled cases — unwired seam, unregistered domain, nothing to look up,
// elapsed bound reached — deciding nothing, whatever the counts say.
func TestCrossScopeUnreadyProducersStaysSilentWhenTheGateIsDisabled(t *testing.T) {
	t.Parallel()

	got := crossScopeUnreadyProducers(crossScopeProducerReadinessSignal{
		gateDisabled:    true,
		producerDomains: []Domain{DomainContainerImageIdentity},
	}, nil)
	if len(got) != 0 {
		t.Fatalf("crossScopeUnreadyProducers() = %v, want none when the gate is disabled", got)
	}
}

// TestCICDRunCorrelationDeclaresExactlyOneProducer is the guard
// singleProducerResolvedCounts depends on. That adapter credits a whole-batch
// count to one producer, which is only sound because this consumer reads a
// dedicated producer reader. A second declared producer makes the count
// ambiguous again.
func TestCICDRunCorrelationDeclaresExactlyOneProducer(t *testing.T) {
	t.Parallel()

	dependencies := crossScopeDependenciesForRegistration(DomainCICDRunCorrelation)
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

	got := singleProducerResolvedCounts(
		[]Domain{DomainContainerImageIdentity, DomainCICDRunCorrelation}, 5,
	)
	if len(got) != 0 {
		t.Fatalf("singleProducerResolvedCounts() = %v, want nothing credited for a two-producer consumer", got)
	}
	if credited := singleProducerResolvedCounts([]Domain{DomainContainerImageIdentity}, 5); credited[DomainContainerImageIdentity] != 5 {
		t.Fatalf("singleProducerResolvedCounts() = %v, want the batch count credited to the sole producer", credited)
	}
}
