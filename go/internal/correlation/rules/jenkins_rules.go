package rules

// JenkinsRulePack returns the first-party correlation rules for Jenkins delivery.
func JenkinsRulePack() RulePack {
	return RulePack{
		Name:                   "jenkins",
		MinAdmissionConfidence: 0.84,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "pipeline-repository",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "jenkins"},
					{Field: EvidenceFieldKey, Value: "repository"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-jenkins-shared-libraries", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-jenkins-repositories-and-pipelines", Kind: RuleKindMatch, Priority: 20, MaxMatches: 6},
			{Name: "derive-controller-or-service-role", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-delivery-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-jenkins-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
