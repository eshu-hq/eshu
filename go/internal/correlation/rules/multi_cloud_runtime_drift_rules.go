// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rules

// MultiCloudRuntimeDriftPackName is the stable rule-pack identifier for the
// provider-neutral cloud-runtime drift join. It mirrors the AWS pack but keys on
// the canonical cloud_resource_uid keyspace so AWS, GCP, and Azure share one
// drift path instead of one pack per provider.
const MultiCloudRuntimeDriftPackName = "multi_cloud_runtime_drift"

// Multi-cloud runtime drift rule names referenced by telemetry and tests. The
// names mirror the AWS pack so operators read one drift vocabulary across
// providers; only the join key differs.
const (
	MultiCloudRuntimeDriftRuleExtractUIDKey      = "extract-cloud-resource-uid-key"
	MultiCloudRuntimeDriftRuleMatchCloudState    = "match-cloud-state-config-by-uid"
	MultiCloudRuntimeDriftRuleDeriveFinding      = "derive-cloud-runtime-finding"
	MultiCloudRuntimeDriftRuleAdmitFinding       = "admit-cloud-runtime-finding"
	MultiCloudRuntimeDriftRuleExplainFinding     = "explain-cloud-runtime-finding"
	multiCloudRuntimeDriftMinAdmissionConfidence = 0.85
)

// MultiCloudRuntimeDriftEvidenceType is the provider-neutral key atom the pack
// requires. Candidates carry one canonical cloud_resource_uid per finding so the
// engine joins observed, state, and config evidence without provider-specific
// identity shapes leaking into admission.
const MultiCloudRuntimeDriftEvidenceType = "cloud_resource_uid"

// MultiCloudRuntimeDriftRulePack returns the first-party rule pack for joining
// provider-neutral cloud runtime observations with Terraform state and config by
// canonical cloud_resource_uid.
func MultiCloudRuntimeDriftRulePack() RulePack {
	return RulePack{
		Name:                   MultiCloudRuntimeDriftPackName,
		MinAdmissionConfidence: multiCloudRuntimeDriftMinAdmissionConfidence,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "cloud-resource-uid",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: MultiCloudRuntimeDriftEvidenceType},
				},
			},
		},
		Rules: []Rule{
			{Name: MultiCloudRuntimeDriftRuleExtractUIDKey, Kind: RuleKindExtractKey, Priority: 10},
			{Name: MultiCloudRuntimeDriftRuleMatchCloudState, Kind: RuleKindMatch, Priority: 20, MaxMatches: 1},
			{Name: MultiCloudRuntimeDriftRuleDeriveFinding, Kind: RuleKindDerive, Priority: 30},
			{Name: MultiCloudRuntimeDriftRuleAdmitFinding, Kind: RuleKindAdmit, Priority: 40},
			{Name: MultiCloudRuntimeDriftRuleExplainFinding, Kind: RuleKindExplain, Priority: 50},
		},
	}
}
