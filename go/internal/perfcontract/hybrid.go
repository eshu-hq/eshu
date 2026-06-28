// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

import "github.com/eshu-hq/eshu/go/internal/searchbench"

// hybridRetrievalThresholds binds the hybrid retrieval production-gate numbers to
// hybrid-retrieval-production-gate.md. The values are sourced from
// searchbench.ProductionGateThresholdsFor so there is exactly one in-code source
// of truth (already pinned by searchbench's own
// TestProductionGateThresholdsAreDocumented); this layer adds the same numbers to
// the unified performance contract and re-binds them to the doc phrases.
//
// Unlike the envelope and claim-latency numbers, the hybrid accuracy and latency
// bars are measured by a hermetic local-deterministic gate, so they are
// EnforcementHermeticGate.
func hybridRetrievalThresholds() []Threshold {
	const doc = DocHybridRetrieval
	local := searchbench.ProductionGateThresholdsFor(searchbench.GateProfileLocalDeterministic)
	prod := searchbench.ProductionGateThresholdsFor(searchbench.GateProfileProductionProvider)
	return []Threshold{
		{Name: "local_min_recall", Doc: doc, Phrase: "| Min recall | 0.60 | 0.80 |", Token: "0.60", Value: local.MinRecall, Unit: "ratio", Enforcement: EnforcementHermeticGate},
		{Name: "prod_min_recall", Doc: doc, Phrase: "| Min recall | 0.60 | 0.80 |", Token: "0.80", Value: prod.MinRecall, Unit: "ratio", Enforcement: EnforcementOperatorGated},
		{Name: "local_min_precision", Doc: doc, Phrase: "| Min precision | 0.50 | 0.70 |", Token: "0.50", Value: local.MinPrecision, Unit: "ratio", Enforcement: EnforcementHermeticGate},
		{Name: "prod_min_precision", Doc: doc, Phrase: "| Min precision | 0.50 | 0.70 |", Token: "0.70", Value: prod.MinPrecision, Unit: "ratio", Enforcement: EnforcementOperatorGated},
		{Name: "local_min_ndcg", Doc: doc, Phrase: "| Min nDCG | 0.60 | 0.80 |", Token: "0.60", Value: local.MinNDCG, Unit: "ratio", Enforcement: EnforcementHermeticGate},
		{Name: "prod_min_ndcg", Doc: doc, Phrase: "| Min nDCG | 0.60 | 0.80 |", Token: "0.80", Value: prod.MinNDCG, Unit: "ratio", Enforcement: EnforcementOperatorGated},
		{Name: "local_max_p95_ms", Doc: doc, Phrase: "| Max p95 latency | 50 ms | 150 ms |", Token: "50 ms", Value: float64(local.MaxP95.Milliseconds()), Unit: "ms", Enforcement: EnforcementHermeticGate},
		{Name: "prod_max_p95_ms", Doc: doc, Phrase: "| Max p95 latency | 50 ms | 150 ms |", Token: "150 ms", Value: float64(prod.MaxP95.Milliseconds()), Unit: "ms", Enforcement: EnforcementOperatorGated},
		{Name: "local_min_vector_coverage", Doc: doc, Phrase: "| Min vector coverage | 0.95 | 0.98 |", Token: "0.95", Value: local.MinVectorCoverage, Unit: "ratio", Enforcement: EnforcementHermeticGate},
		{Name: "prod_min_vector_coverage", Doc: doc, Phrase: "| Min vector coverage | 0.95 | 0.98 |", Token: "0.98", Value: prod.MinVectorCoverage, Unit: "ratio", Enforcement: EnforcementOperatorGated},
		{Name: "false_canonical_claims", Doc: doc, Phrase: "| False canonical claims | 0 | 0 |", Token: "0", Value: 0, Unit: "count", Enforcement: EnforcementHermeticGate},
	}
}
