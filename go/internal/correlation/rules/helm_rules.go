package rules

// HelmRulePack returns the first-party correlation rules for Helm config.
func HelmRulePack() RulePack {
	return RulePack{
		Name:                   "helm",
		MinAdmissionConfidence: 0.86,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "release-mapping",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "helm"},
					{Field: EvidenceFieldKey, Value: "release"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-release-and-image-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-helm-chart-and-values", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-deployment-ownership-signal", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-deployment-mapping-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-helm-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
