// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import "sort"

// FixtureReadiness is the aggregated parity verdict for one collector family
// across its scenarios. It is the bridge artifact that feeds the per-collector
// promotion proof report: a family whose fixture lane did not reach readback, or
// whose contract was not met, must not be promoted to live readiness on fixture
// evidence alone.
type FixtureReadiness struct {
	// CollectorKind is the collector family.
	CollectorKind string
	// Scenarios is the number of scenarios evaluated for the family.
	Scenarios int
	// ContractMet is true only when every scenario met its expectation.
	ContractMet bool
	// ReadbackReached is true when at least one scenario made facts readable.
	ReadbackReached bool
	// FailedScenarios names scenarios whose contract was not met, sorted.
	FailedScenarios []string
}

// Summarize aggregates scenario results into one readiness verdict per collector
// family, in collector-kind order. It is deterministic and credential-free.
func Summarize(results ...Result) []FixtureReadiness {
	type accumulator struct {
		scenarios   int
		contractMet bool
		readback    bool
		failed      []string
	}
	byKind := map[string]*accumulator{}
	order := make([]string, 0)
	for _, result := range results {
		acc, ok := byKind[result.CollectorKind]
		if !ok {
			acc = &accumulator{contractMet: true}
			byKind[result.CollectorKind] = acc
			order = append(order, result.CollectorKind)
		}
		acc.scenarios++
		if !result.ContractMet {
			acc.contractMet = false
			acc.failed = append(acc.failed, result.Scenario)
		}
		// Use the per-scenario reach, not the cumulative ReadableFactKinds, so a
		// collector whose own facts were all withheld does not inherit a true
		// readiness signal from an earlier collector on the same harness.
		if result.ReadbackReached {
			acc.readback = true
		}
	}

	sort.Strings(order)
	summaries := make([]FixtureReadiness, 0, len(order))
	for _, kind := range order {
		acc := byKind[kind]
		sort.Strings(acc.failed)
		summaries = append(summaries, FixtureReadiness{
			CollectorKind:   kind,
			Scenarios:       acc.scenarios,
			ContractMet:     acc.contractMet,
			ReadbackReached: acc.readback,
			FailedScenarios: acc.failed,
		})
	}
	return summaries
}
