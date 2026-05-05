package rules

// CloudFormationRulePack returns the first-party correlation rules for
// CloudFormation template evidence.
func CloudFormationRulePack() RulePack {
	return RulePack{
		Name:                   "cloudformation",
		MinAdmissionConfidence: 0.79,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "stack-template",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "cloudformation"},
					{Field: EvidenceFieldKey, Value: "stack"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-stack-and-resource-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-stack-outputs-and-runtime-identifiers", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-cloudformation-deployable-unit", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-cloudformation-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-cloudformation-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
