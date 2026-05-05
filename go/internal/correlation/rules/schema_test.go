package rules

import "testing"

func TestRulePackValidateAcceptsKnownRuleKinds(t *testing.T) {
	t.Parallel()

	pack := RulePack{
		Name:                   "container_core",
		MinAdmissionConfidence: 0.75,
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
			{Name: "extract_service_name", Kind: RuleKindExtractKey},
			{Name: "match_release_to_image", Kind: RuleKindMatch},
			{Name: "admit_container_workload", Kind: RuleKindAdmit},
			{Name: "derive_workload_name", Kind: RuleKindDerive},
			{Name: "explain_join", Kind: RuleKindExplain},
		},
	}

	if err := pack.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestRulePackValidateRejectsUnknownRuleKind(t *testing.T) {
	t.Parallel()

	pack := RulePack{
		Name: "broken",
		Rules: []Rule{
			{Name: "unknown", Kind: RuleKind("mystery")},
		},
	}

	if err := pack.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestRulePackValidateRejectsOutOfRangeAdmissionThreshold(t *testing.T) {
	t.Parallel()

	pack := RulePack{
		Name:                   "too_confident",
		MinAdmissionConfidence: 1.1,
		Rules: []Rule{
			{Name: "admit_container_workload", Kind: RuleKindAdmit},
		},
	}

	if err := pack.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestRulePackValidateRejectsInvalidEvidenceRequirement(t *testing.T) {
	t.Parallel()

	pack := RulePack{
		Name:                   "broken-structure",
		MinAdmissionConfidence: 0.8,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "invalid",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceField("mystery"), Value: "value"},
				},
			},
		},
		Rules: []Rule{
			{Name: "admit_container_workload", Kind: RuleKindAdmit},
		},
	}

	if err := pack.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}
