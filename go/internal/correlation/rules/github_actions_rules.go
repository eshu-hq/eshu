package rules

// GitHubActionsRulePack returns the first-party correlation rules for GitHub Actions.
func GitHubActionsRulePack() RulePack {
	return RulePack{
		Name:                   "github_actions",
		MinAdmissionConfidence: 0.82,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "workflow-repository",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "github_actions"},
					{Field: EvidenceFieldKey, Value: "repository"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-workflow-references", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-reusable-workflow-and-checkout-repositories", Kind: RuleKindMatch, Priority: 20, MaxMatches: 6},
			{Name: "derive-delivery-path", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-delivery-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-github-actions-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
