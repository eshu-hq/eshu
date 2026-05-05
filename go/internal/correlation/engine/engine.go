package engine

import (
	"cmp"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/correlation/admission"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

// Result is the deterministic evaluation output for one candidate.
type Result struct {
	Candidate   model.Candidate
	MatchCounts map[string]int
}

// Evaluation records deterministic rule ordering and candidate outcomes.
type Evaluation struct {
	OrderedRuleNames []string
	Results          []Result
}

// Evaluate applies the bounded rule pack and deterministic admission behavior.
func Evaluate(pack rules.RulePack, candidates []model.Candidate) (Evaluation, error) {
	if err := pack.Validate(); err != nil {
		return Evaluation{}, err
	}

	orderedRules := slices.Clone(pack.Rules)
	slices.SortFunc(orderedRules, func(left rules.Rule, right rules.Rule) int {
		if left.Priority != right.Priority {
			return cmp.Compare(left.Priority, right.Priority)
		}
		return cmp.Compare(left.Name, right.Name)
	})

	results := make([]Result, 0, len(candidates))
	orderedRuleNames := make([]string, 0, len(orderedRules))
	for _, rule := range orderedRules {
		orderedRuleNames = append(orderedRuleNames, rule.Name)
	}

	for _, candidate := range candidates {
		evaluatedCandidate, outcome, err := admission.Evaluate(candidate, pack.MinAdmissionConfidence, pack.RequiredEvidence)
		if err != nil {
			return Evaluation{}, err
		}

		matchCounts := make(map[string]int, len(orderedRules))
		for _, rule := range orderedRules {
			if rule.Kind != rules.RuleKindMatch {
				continue
			}
			matchCounts[rule.Name] = boundedMatchCount(rule.MaxMatches, len(evaluatedCandidate.Evidence))
		}
		if evaluatedCandidate.State == model.CandidateStateRejected && !outcome.MeetsConfidence {
			evaluatedCandidate.RejectionReasons = append(evaluatedCandidate.RejectionReasons, model.RejectionReasonLowConfidence)
		}
		if evaluatedCandidate.State == model.CandidateStateRejected && !outcome.MeetsStructure {
			evaluatedCandidate.RejectionReasons = append(evaluatedCandidate.RejectionReasons, model.RejectionReasonStructuralMismatch)
		}

		results = append(results, Result{
			Candidate:   evaluatedCandidate,
			MatchCounts: matchCounts,
		})
	}

	admitWinners(results)
	slices.SortFunc(results, func(left Result, right Result) int {
		if left.Candidate.CorrelationKey != right.Candidate.CorrelationKey {
			return cmp.Compare(left.Candidate.CorrelationKey, right.Candidate.CorrelationKey)
		}
		if left.Candidate.State != right.Candidate.State {
			if left.Candidate.State == model.CandidateStateAdmitted {
				return -1
			}
			if right.Candidate.State == model.CandidateStateAdmitted {
				return 1
			}
		}
		return cmp.Compare(left.Candidate.ID, right.Candidate.ID)
	})

	return Evaluation{
		OrderedRuleNames: orderedRuleNames,
		Results:          results,
	}, nil
}

func admitWinners(results []Result) {
	winningIndexByKey := make(map[string]int)
	for idx := range results {
		candidate := results[idx].Candidate
		if candidate.State != model.CandidateStateAdmitted {
			continue
		}
		winningIndex, ok := winningIndexByKey[candidate.CorrelationKey]
		if !ok {
			winningIndexByKey[candidate.CorrelationKey] = idx
			continue
		}
		if compareCandidates(candidate, results[winningIndex].Candidate) < 0 {
			results[winningIndex].Candidate.State = model.CandidateStateRejected
			results[winningIndex].Candidate.RejectionReasons = append(results[winningIndex].Candidate.RejectionReasons, model.RejectionReasonLostTieBreak)
			winningIndexByKey[candidate.CorrelationKey] = idx
			continue
		}
		results[idx].Candidate.State = model.CandidateStateRejected
		results[idx].Candidate.RejectionReasons = append(results[idx].Candidate.RejectionReasons, model.RejectionReasonLostTieBreak)
	}
}

func compareCandidates(left model.Candidate, right model.Candidate) int {
	if left.Confidence != right.Confidence {
		return cmp.Compare(right.Confidence, left.Confidence)
	}
	return cmp.Compare(left.ID, right.ID)
}

func boundedMatchCount(maxMatches int, available int) int {
	if maxMatches <= 0 || available <= maxMatches {
		return available
	}
	return maxMatches
}
