package rules

// ArgoCDRulePack returns the first-party correlation rules for ArgoCD config.
func ArgoCDRulePack() RulePack {
	return RulePack{
		Name:                   "argocd",
		MinAdmissionConfidence: 0.9,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "application-mapping",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "argocd"},
					{Field: EvidenceFieldKey, Value: "application"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-application-and-destination-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-applicationset-and-deploy-source", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-platform-placement", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-controller-backed-deployment", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-argocd-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
