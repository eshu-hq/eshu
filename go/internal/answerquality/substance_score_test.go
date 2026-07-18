// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerquality

import "testing"

// TestScoreRejectsCircularAnswer proves the offline scorer fails a circular,
// identity-only answer even when it is supported, cited, and not flagged
// too-generic — the answer only echoes the question's entity (issue #5266).
func TestScoreRejectsCircularAnswer(t *testing.T) {
	t.Parallel()

	evidence := completeEvidence()
	// Echo the prompt back as the answer: every content token comes from the
	// question, so the answer names no operational fact.
	circularPrompt := &evidence.Prompts[0]
	for i := range circularPrompt.Results {
		circularPrompt.Results[i].AnswerSummary = circularPrompt.Prompt
		circularPrompt.Results[i].TooGeneric = false
	}

	verdict := Score(evidence)
	if verdict.Pass {
		t.Fatal("scorecard passed a circular answer, want usefulness failure")
	}
	if !hasFailingCriterion(verdict, CriterionUsefulness) {
		t.Errorf("verdict missing a usefulness failure: %+v", verdict.Criteria)
	}
}

func hasFailingCriterion(verdict Verdict, name CriterionName) bool {
	for _, c := range verdict.Criteria {
		if c.Name == name && c.Status == CriterionFail {
			return true
		}
	}
	return false
}
