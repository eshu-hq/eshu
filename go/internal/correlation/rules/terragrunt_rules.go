package rules

// TerragruntRulePack returns the first-party correlation rules for Terragrunt
// config discovery.
func TerragruntRulePack() RulePack {
	return RulePack{
		Name:                   "terragrunt",
		MinAdmissionConfidence: 0.76,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "config-path",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "terragrunt"},
					{Field: EvidenceFieldKey, Value: "config_path"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-terragrunt-config-and-module-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-terragrunt-dependencies-with-config-assets", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-config-discovery-scope", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-config-discovery-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-terragrunt-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
