package rules

// TerraformConfigRulePack returns the first-party correlation rules for Terraform config.
func TerraformConfigRulePack() RulePack {
	return RulePack{
		Name:                   "terraform_config",
		MinAdmissionConfidence: 0.91,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "module-mapping",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "terraform_config"},
					{Field: EvidenceFieldKey, Value: "module"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-module-and-repository-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-app-repo-module-and-config-paths", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-provisioning-ownership", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-provisioning-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-terraform-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
