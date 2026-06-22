package proofofvalue

import "fmt"

// reachabilityLabels is the closed set of valid answers and labels. Keeping it
// closed makes a typo in either ground truth or a strategy fail loudly instead
// of silently scoring as a miss.
var reachabilityLabels = map[string]struct{}{
	"used":      {},
	"unused":    {},
	"ambiguous": {},
}

// predictionKey identifies a prediction by question and strategy.
type predictionKey struct {
	questionID string
	strategy   Strategy
}

// validateQuestions checks that the question set is non-empty, has unique IDs,
// and uses only known reachability labels.
func validateQuestions(questions []Question) error {
	if len(questions) == 0 {
		return fmt.Errorf("proofofvalue: question set is empty")
	}
	seen := make(map[string]struct{}, len(questions))
	for _, q := range questions {
		if q.ID == "" {
			return fmt.Errorf("proofofvalue: question with artifact %q has empty id", q.Artifact)
		}
		if _, dup := seen[q.ID]; dup {
			return fmt.Errorf("proofofvalue: duplicate question id %q", q.ID)
		}
		seen[q.ID] = struct{}{}
		if _, ok := reachabilityLabels[q.Label]; !ok {
			return fmt.Errorf("proofofvalue: question %q has invalid label %q", q.ID, q.Label)
		}
	}
	return nil
}

// indexPredictions builds a lookup from (question, strategy) to answer. It
// requires exactly one prediction per strategy for every question and rejects
// unknown answers, duplicates, and predictions for unknown questions.
func indexPredictions(questions []Question, predictions []Prediction) (map[predictionKey]string, error) {
	questionIDs := make(map[string]struct{}, len(questions))
	for _, q := range questions {
		questionIDs[q.ID] = struct{}{}
	}

	index := make(map[predictionKey]string, len(predictions))
	for _, p := range predictions {
		if _, ok := questionIDs[p.QuestionID]; !ok {
			return nil, fmt.Errorf("proofofvalue: prediction for unknown question %q", p.QuestionID)
		}
		if p.Strategy != StrategyBaseline && p.Strategy != StrategyEshu {
			return nil, fmt.Errorf("proofofvalue: prediction for question %q has unknown strategy %q", p.QuestionID, p.Strategy)
		}
		if _, ok := reachabilityLabels[p.Answer]; !ok {
			return nil, fmt.Errorf("proofofvalue: prediction for question %q has invalid answer %q", p.QuestionID, p.Answer)
		}
		key := predictionKey{p.QuestionID, p.Strategy}
		if _, dup := index[key]; dup {
			return nil, fmt.Errorf("proofofvalue: duplicate %s prediction for question %q", p.Strategy, p.QuestionID)
		}
		index[key] = p.Answer
	}

	for _, q := range questions {
		for _, strat := range []Strategy{StrategyBaseline, StrategyEshu} {
			if _, ok := index[predictionKey{q.ID, strat}]; !ok {
				return nil, fmt.Errorf("proofofvalue: missing %s prediction for question %q", strat, q.ID)
			}
		}
	}
	return index, nil
}
