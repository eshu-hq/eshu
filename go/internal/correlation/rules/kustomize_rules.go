package rules

// KustomizeRulePack returns the first-party correlation rules for Kustomize
// deployment config.
func KustomizeRulePack() RulePack {
	return RulePack{
		Name:                   "kustomize",
		MinAdmissionConfidence: 0.83,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "resource-mapping",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "kustomize"},
					{Field: EvidenceFieldKey, Value: "resource"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-kustomize-resource-and-image-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-kustomize-resources-with-app-references", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-kustomize-deployment-source", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-kustomize-deployment-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-kustomize-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
