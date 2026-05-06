package rules

// DockerfileRulePack returns the first-party correlation rules for Dockerfiles.
func DockerfileRulePack() RulePack {
	return RulePack{
		Name:                   "dockerfile",
		MinAdmissionConfidence: 0.90,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "runtime-image",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "dockerfile"},
					{Field: EvidenceFieldKey, Value: "image"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-image-source-label", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-image-source-to-repository", Kind: RuleKindMatch, Priority: 20, MaxMatches: 4},
			{Name: "derive-deployable-unit-name", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-runtime-image-candidate", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-dockerfile-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
