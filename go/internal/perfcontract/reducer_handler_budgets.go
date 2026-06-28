// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

// reducerHandlerBudgetThresholds binds the B-9 (#3802) per-handler ns/op
// ceilings published in reducer-claim-latency-gate.md to their in-code values so
// the lockstep gate keeps the doc, the committed budget artifact
// (testdata/benchmarks/reducer-handler-budgets.txt), and the code from drifting.
//
// Each ceiling is the absolute ns/op budget for one credential-free reducer
// extractor benchmark; the numbers MUST equal the budget column of
// reducer-handler-budgets.txt, which is the single source of truth. Tokens are
// written comma-free ("16100000 ns/op") because the lockstep test's
// leadingNumber parser stops at the first non-digit/non-dot rune, so a
// comma-grouped token would parse to a wrong value.
//
// Enforcement is EnforcementOperatorGated: the gate ships advisory
// (REDUCER_PERF_ENFORCE=false) until the budgets are recaptured on the CI
// ubuntu-latest runner class, so the ceilings are not yet hermetically enforced
// in CI — the same controlled-environment semantics the claim-latency
// thresholds use. The lockstep below still keeps each documented ceiling honest.
func reducerHandlerBudgetThresholds() []Threshold {
	const doc = DocClaimLatency
	const enf = EnforcementOperatorGated
	const unit = "ns/op"
	return []Threshold{
		{Name: "handler_budget_extract_cloud_resource_node", Doc: doc, Phrase: "AWS node materialization handler stays under 16100000 ns/op", Token: "16100000 ns/op", Value: 16100000, Unit: unit, Enforcement: enf},                   // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_extract_gcp_cloud_resource_node", Doc: doc, Phrase: "GCP node materialization handler stays under 15800000 ns/op", Token: "15800000 ns/op", Value: 15800000, Unit: unit, Enforcement: enf},               // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_extract_kubernetes_workload_node", Doc: doc, Phrase: "Kubernetes workload node materialization handler stays under 8100000 ns/op", Token: "8100000 ns/op", Value: 8100000, Unit: unit, Enforcement: enf}, // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_extract_aws_relationship_edge", Doc: doc, Phrase: "AWS relationship edge handler stays under 37000000 ns/op", Token: "37000000 ns/op", Value: 37000000, Unit: unit, Enforcement: enf},                    // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_extract_gcp_relationship_edge", Doc: doc, Phrase: "GCP relationship edge handler stays under 41500000 ns/op", Token: "41500000 ns/op", Value: 41500000, Unit: unit, Enforcement: enf},                    // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_extract_kubernetes_correlation_edge", Doc: doc, Phrase: "Kubernetes correlation edge handler stays under 13800000 ns/op", Token: "13800000 ns/op", Value: 13800000, Unit: unit, Enforcement: enf},        // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_secrets_iam_gcp_grant_observations", Doc: doc, Phrase: "secrets/IAM trust-chain handler stays under 8300000 ns/op", Token: "8300000 ns/op", Value: 8300000, Unit: unit, Enforcement: enf},                // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_service_catalog_correlation", Doc: doc, Phrase: "service-catalog correlation handler stays under 9200000 ns/op", Token: "9200000 ns/op", Value: 9200000, Unit: unit, Enforcement: enf},                   // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_value_flow_fixpoint_full", Doc: doc, Phrase: "value-flow fixpoint cold handler stays under 30600000 ns/op", Token: "30600000 ns/op", Value: 30600000, Unit: unit, Enforcement: enf},                      // #nosec G101 -- benchmark documentation token, not a credential.
		{Name: "handler_budget_value_flow_fixpoint_incremental_cached", Doc: doc, Phrase: "value-flow fixpoint cached handler stays under 18000000 ns/op", Token: "18000000 ns/op", Value: 18000000, Unit: unit, Enforcement: enf},      // #nosec G101 -- benchmark documentation token, not a credential.
	}
}
