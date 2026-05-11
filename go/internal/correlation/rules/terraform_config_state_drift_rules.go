package rules

// Design contract: docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md
// Tracking issue: #43 (epic #50).
//
// The five drift kinds emitted under
// eshu_dp_correlation_drift_detected_total{drift_kind}:
//   - added_in_state         state has the resource, config does not
//   - added_in_config        config has the resource, state does not
//   - attribute_drift        both sides exist, an allowlisted attribute differs
//   - removed_from_state     prior state generation had the resource, current does not
//   - removed_from_config    state has the resource, latest config no longer declares it
//
// Candidates produced for this pack carry EvidenceAtoms from two ScopeIDs
// (repo scope for config, state_snapshot scope for state). This pattern is
// structurally permitted by go/internal/correlation/model/types.go;
// Candidate.Validate does not enforce uniform ScopeID across atoms. This is
// the first first-party pack to rely on it.
//
// The DSL does not compare evidence values — engine.Evaluate sorts and counts
// rules. Drift comparison runs in helper Go (Phase 1; planned location
// go/internal/correlation/drift/) which constructs one candidate per drifted
// address before calling engine.Evaluate(TerraformConfigStateDriftRulePack(), ...).

// TerraformConfigStateDriftPackName is the stable rule-pack identifier
// emitted as the `pack` metric label and recorded in correlation explain
// traces.
const TerraformConfigStateDriftPackName = "terraform_config_state_drift"

// Drift rule names referenced by the reducer handler and telemetry tests.
const (
	TerraformConfigStateDriftRuleExtractAddressKey    = "extract-resource-address-key"
	TerraformConfigStateDriftRuleMatchConfigToState   = "match-config-against-state"
	TerraformConfigStateDriftRuleDeriveDriftKind      = "derive-drift-classification"
	TerraformConfigStateDriftRuleAdmitDriftEvidence   = "admit-drift-evidence"
	TerraformConfigStateDriftRuleExplainDriftDecision = "explain-drift-classification"
)

// terraformConfigStateDriftMinAdmissionConfidence holds the structural
// admission floor for drift candidates. Comparison logic does the semantic
// work; the threshold guards against atoms that lack the joined-evidence
// shape this pack requires. Must stay >= 0.75 to satisfy the schema test.
const terraformConfigStateDriftMinAdmissionConfidence = 0.80

// TerraformConfigStateDriftRulePack returns the declarative rule pack that
// records the five-stage drift pipeline for the correlation engine's explain
// trace. The actual config-vs-state comparison runs in helper Go before
// engine.Evaluate is called; this pack carries the admission threshold and
// the deterministic rule ordering.
func TerraformConfigStateDriftRulePack() RulePack {
	return RulePack{
		Name:                   TerraformConfigStateDriftPackName,
		MinAdmissionConfidence: terraformConfigStateDriftMinAdmissionConfidence,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "drift-candidate-address",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "terraform_drift_address"},
				},
			},
		},
		Rules: []Rule{
			{Name: TerraformConfigStateDriftRuleExtractAddressKey, Kind: RuleKindExtractKey, Priority: 10},
			{Name: TerraformConfigStateDriftRuleMatchConfigToState, Kind: RuleKindMatch, Priority: 20, MaxMatches: 1},
			{Name: TerraformConfigStateDriftRuleDeriveDriftKind, Kind: RuleKindDerive, Priority: 30},
			{Name: TerraformConfigStateDriftRuleAdmitDriftEvidence, Kind: RuleKindAdmit, Priority: 40},
			{Name: TerraformConfigStateDriftRuleExplainDriftDecision, Kind: RuleKindExplain, Priority: 50},
		},
	}
}
