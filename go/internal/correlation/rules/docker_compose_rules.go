package rules

// DockerComposeRulePack returns the first-party correlation rules for Docker
// Compose services.
func DockerComposeRulePack() RulePack {
	return RulePack{
		Name:                   "docker_compose",
		MinAdmissionConfidence: 0.8,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "compose-service",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "docker_compose"},
					{Field: EvidenceFieldKey, Value: "service"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-compose-service-and-image-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-compose-build-context-and-image-references", Kind: RuleKindMatch, Priority: 20, MaxMatches: 6},
			{Name: "derive-compose-deployable-unit", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-compose-runtime-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-compose-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
