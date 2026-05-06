package rules

import (
	"fmt"
	"strings"
)

// RuleKind identifies a supported DSL primitive.
type RuleKind string

const (
	RuleKindExtractKey RuleKind = "extract_key"
	RuleKindMatch      RuleKind = "match"
	RuleKindAdmit      RuleKind = "admit"
	RuleKindDerive     RuleKind = "derive"
	RuleKindExplain    RuleKind = "explain"
)

// EvidenceField identifies one exact-match EvidenceAtom field supported by rule packs.
type EvidenceField string

const (
	EvidenceFieldSourceSystem EvidenceField = "source_system"
	EvidenceFieldEvidenceType EvidenceField = "evidence_type"
	EvidenceFieldScopeID      EvidenceField = "scope_id"
	EvidenceFieldKey          EvidenceField = "key"
	EvidenceFieldValue        EvidenceField = "value"
)

// EvidenceSelector matches one EvidenceAtom field to one exact value.
type EvidenceSelector struct {
	Field EvidenceField
	Value string
}

// EvidenceRequirement requires a bounded number of atoms to satisfy all selectors.
type EvidenceRequirement struct {
	Name     string
	MinCount int
	MatchAll []EvidenceSelector
}

// Rule is one declarative DSL rule.
type Rule struct {
	Name       string
	Kind       RuleKind
	Priority   int
	MaxMatches int
}

// RulePack is a bounded group of correlation rules.
type RulePack struct {
	Name                   string
	MinAdmissionConfidence float64
	RequiredEvidence       []EvidenceRequirement
	Rules                  []Rule
}

func (k RuleKind) Validate() error {
	switch k {
	case RuleKindExtractKey, RuleKindMatch, RuleKindAdmit, RuleKindDerive, RuleKindExplain:
		return nil
	default:
		return fmt.Errorf("unknown rule kind %q", k)
	}
}

// Validate checks the supported EvidenceAtom fields for exact-match admission requirements.
func (f EvidenceField) Validate() error {
	switch f {
	case EvidenceFieldSourceSystem, EvidenceFieldEvidenceType, EvidenceFieldScopeID, EvidenceFieldKey, EvidenceFieldValue:
		return nil
	default:
		return fmt.Errorf("unknown evidence field %q", f)
	}
}

// Validate checks one exact-match EvidenceAtom selector.
func (s EvidenceSelector) Validate() error {
	if err := s.Field.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.Value) == "" {
		return fmt.Errorf("evidence selector value must not be blank")
	}
	return nil
}

// Validate checks one bounded structural requirement.
func (r EvidenceRequirement) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("evidence requirement name must not be blank")
	}
	if r.MinCount <= 0 {
		return fmt.Errorf("evidence requirement min count must be positive")
	}
	if len(r.MatchAll) == 0 {
		return fmt.Errorf("evidence requirement must contain at least one selector")
	}
	for _, selector := range r.MatchAll {
		if err := selector.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the rule-pack schema.
func (p RulePack) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("rule pack name must not be blank")
	}
	if p.MinAdmissionConfidence < 0 || p.MinAdmissionConfidence > 1 {
		return fmt.Errorf("min admission confidence must be within [0,1]")
	}
	if len(p.Rules) == 0 {
		return fmt.Errorf("rule pack must contain at least one rule")
	}
	for _, requirement := range p.RequiredEvidence {
		if err := requirement.Validate(); err != nil {
			return err
		}
	}
	for _, rule := range p.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule name must not be blank")
		}
		if err := rule.Kind.Validate(); err != nil {
			return err
		}
		if rule.Priority < 0 {
			return fmt.Errorf("rule priority must be non-negative")
		}
		if rule.MaxMatches < 0 {
			return fmt.Errorf("rule max matches must be non-negative")
		}
	}
	return nil
}
