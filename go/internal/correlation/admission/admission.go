package admission

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

// Outcome records which generic admission gates a candidate satisfied.
type Outcome struct {
	MeetsConfidence bool
	MeetsStructure  bool
}

// Evaluate applies bounded confidence and exact-match evidence requirements without altering identity.
func Evaluate(candidate model.Candidate, threshold float64, requiredEvidence []rules.EvidenceRequirement) (model.Candidate, Outcome, error) {
	if threshold < 0 || threshold > 1 {
		return model.Candidate{}, Outcome{}, fmt.Errorf("threshold must be within [0,1]")
	}
	if err := candidate.Validate(); err != nil {
		return model.Candidate{}, Outcome{}, err
	}
	for _, requirement := range requiredEvidence {
		if err := requirement.Validate(); err != nil {
			return model.Candidate{}, Outcome{}, err
		}
	}

	evaluated := candidate
	outcome := Outcome{
		MeetsConfidence: evaluated.Confidence >= threshold,
		MeetsStructure:  satisfiesRequirements(evaluated.Evidence, requiredEvidence),
	}
	if outcome.MeetsConfidence && outcome.MeetsStructure {
		evaluated.State = model.CandidateStateAdmitted
		return evaluated, outcome, nil
	}

	evaluated.State = model.CandidateStateRejected
	return evaluated, outcome, nil
}

func satisfiesRequirements(evidence []model.EvidenceAtom, requirements []rules.EvidenceRequirement) bool {
	for _, requirement := range requirements {
		matches := 0
		for _, atom := range evidence {
			if matchesRequirement(atom, requirement) {
				matches++
			}
		}
		if matches < requirement.MinCount {
			return false
		}
	}
	return true
}

func matchesRequirement(atom model.EvidenceAtom, requirement rules.EvidenceRequirement) bool {
	for _, selector := range requirement.MatchAll {
		if evidenceFieldValue(atom, selector.Field) != selector.Value {
			return false
		}
	}
	return true
}

func evidenceFieldValue(atom model.EvidenceAtom, field rules.EvidenceField) string {
	switch field {
	case rules.EvidenceFieldSourceSystem:
		return atom.SourceSystem
	case rules.EvidenceFieldEvidenceType:
		return atom.EvidenceType
	case rules.EvidenceFieldScopeID:
		return atom.ScopeID
	case rules.EvidenceFieldKey:
		return atom.Key
	case rules.EvidenceFieldValue:
		return atom.Value
	default:
		return ""
	}
}
