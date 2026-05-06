package rules

// AnsibleRulePack returns the first-party correlation rules for Ansible role
// discovery.
func AnsibleRulePack() RulePack {
	return RulePack{
		Name:                   "ansible",
		MinAdmissionConfidence: 0.76,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "role-reference",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "ansible"},
					{Field: EvidenceFieldKey, Value: "role"},
				},
			},
		},
		Rules: []Rule{
			{Name: "extract-ansible-role-and-inventory-keys", Kind: RuleKindExtractKey, Priority: 10},
			{Name: "match-ansible-roles-with-config-targets", Kind: RuleKindMatch, Priority: 20, MaxMatches: 8},
			{Name: "derive-config-discovery-scope", Kind: RuleKindDerive, Priority: 30},
			{Name: "admit-config-discovery-evidence", Kind: RuleKindAdmit, Priority: 40},
			{Name: "explain-ansible-correlation", Kind: RuleKindExplain, Priority: 50},
		},
	}
}
