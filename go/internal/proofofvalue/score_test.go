// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proofofvalue

import (
	"math"
	"testing"
)

// sampleQuestions is a small honest fixture: two dead artifacts, two live
// artifacts, one ambiguous artifact.
func sampleQuestions() []Question {
	return []Question{
		{ID: "q-used-1", Artifact: "tf/used-1", Family: "terraform", Label: "used"},
		{ID: "q-used-2", Artifact: "tf/used-2", Family: "terraform", Label: "used"},
		{ID: "q-unused-1", Artifact: "tf/unused-1", Family: "terraform", Label: "unused"},
		{ID: "q-unused-2", Artifact: "tf/unused-2", Family: "terraform", Label: "unused"},
		{ID: "q-ambiguous-1", Artifact: "tf/ambiguous-1", Family: "terraform", Label: "ambiguous"},
	}
}

func pred(id string, strat Strategy, answer string) Prediction {
	return Prediction{QuestionID: id, Strategy: strat, Answer: answer}
}

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScoreComputesHonestPerStrategyMetricsAndDelta(t *testing.T) {
	questions := sampleQuestions()
	// Baseline (grep) gets the two "used" right by coincidence, mislabels both
	// "unused" artifacts as "used" (cannot prove absence) and treats the
	// ambiguous one as "used". So baseline: 2/5 correct, 0 dead detected.
	// Eshu gets everything right: 5/5 correct, both dead detected.
	predictions := []Prediction{
		pred("q-used-1", StrategyBaseline, "used"),
		pred("q-used-2", StrategyBaseline, "used"),
		pred("q-unused-1", StrategyBaseline, "used"),
		pred("q-unused-2", StrategyBaseline, "used"),
		pred("q-ambiguous-1", StrategyBaseline, "used"),

		pred("q-used-1", StrategyEshu, "used"),
		pred("q-used-2", StrategyEshu, "used"),
		pred("q-unused-1", StrategyEshu, "unused"),
		pred("q-unused-2", StrategyEshu, "unused"),
		pred("q-ambiguous-1", StrategyEshu, "ambiguous"),
	}

	report, err := Score("sample", questions, predictions)
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}

	if report.SchemaVersion != SchemaVersion {
		t.Errorf("schema version = %q, want %q", report.SchemaVersion, SchemaVersion)
	}
	if report.QuestionCount != 5 {
		t.Errorf("question count = %d, want 5", report.QuestionCount)
	}

	if report.Baseline.Correct != 2 {
		t.Errorf("baseline correct = %d, want 2", report.Baseline.Correct)
	}
	if !approxEqual(report.Baseline.Accuracy, 0.4) {
		t.Errorf("baseline accuracy = %v, want 0.4", report.Baseline.Accuracy)
	}
	if report.Baseline.DeadTruePositive != 0 {
		t.Errorf("baseline dead TP = %d, want 0", report.Baseline.DeadTruePositive)
	}
	// Baseline missed both dead artifacts: honest miss reporting.
	if report.Baseline.DeadFalseNegative != 2 {
		t.Errorf("baseline dead FN = %d, want 2", report.Baseline.DeadFalseNegative)
	}

	if report.Eshu.Correct != 5 {
		t.Errorf("eshu correct = %d, want 5", report.Eshu.Correct)
	}
	if !approxEqual(report.Eshu.Accuracy, 1.0) {
		t.Errorf("eshu accuracy = %v, want 1.0", report.Eshu.Accuracy)
	}
	if report.Eshu.DeadTruePositive != 2 {
		t.Errorf("eshu dead TP = %d, want 2", report.Eshu.DeadTruePositive)
	}
	if !approxEqual(report.Eshu.DeadRecall, 1.0) {
		t.Errorf("eshu dead recall = %v, want 1.0", report.Eshu.DeadRecall)
	}

	if !approxEqual(report.Delta.AccuracyDelta, 0.6) {
		t.Errorf("accuracy delta = %v, want 0.6", report.Delta.AccuracyDelta)
	}
	if !approxEqual(report.Delta.DeadRecallDelta, 1.0) {
		t.Errorf("dead recall delta = %v, want 1.0", report.Delta.DeadRecallDelta)
	}
}

func TestScoreCountsDangerousFalsePositivesAndAvoidance(t *testing.T) {
	// One live artifact. Baseline wrongly calls it dead (dangerous), Eshu
	// correctly calls it used. DangerousMistakesAvoided must be 1.
	questions := []Question{
		{ID: "q1", Artifact: "tf/live", Family: "terraform", Label: "used"},
	}
	predictions := []Prediction{
		pred("q1", StrategyBaseline, "unused"),
		pred("q1", StrategyEshu, "used"),
	}

	report, err := Score("sample", questions, predictions)
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if report.Baseline.DeadFalsePositive != 1 {
		t.Errorf("baseline dead FP = %d, want 1", report.Baseline.DeadFalsePositive)
	}
	if report.Eshu.DeadFalsePositive != 0 {
		t.Errorf("eshu dead FP = %d, want 0", report.Eshu.DeadFalsePositive)
	}
	if report.Delta.DangerousMistakesAvoided != 1 {
		t.Errorf("dangerous mistakes avoided = %d, want 1", report.Delta.DangerousMistakesAvoided)
	}
}

func TestScoreDoesNotInflateWhenEshuIsWrong(t *testing.T) {
	// Guard against a scorer that always favors Eshu: if Eshu answers worse,
	// the delta must be negative.
	questions := []Question{
		{ID: "q1", Artifact: "tf/a", Family: "terraform", Label: "unused"},
	}
	predictions := []Prediction{
		pred("q1", StrategyBaseline, "unused"),
		pred("q1", StrategyEshu, "used"),
	}
	report, err := Score("sample", questions, predictions)
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if report.Delta.AccuracyDelta >= 0 {
		t.Errorf("accuracy delta = %v, want negative when Eshu is worse", report.Delta.AccuracyDelta)
	}
}

func TestScoreSortsQuestionsByID(t *testing.T) {
	questions := []Question{
		{ID: "q-z", Artifact: "tf/z", Family: "terraform", Label: "used"},
		{ID: "q-a", Artifact: "tf/a", Family: "terraform", Label: "used"},
	}
	predictions := []Prediction{
		pred("q-z", StrategyBaseline, "used"),
		pred("q-z", StrategyEshu, "used"),
		pred("q-a", StrategyBaseline, "used"),
		pred("q-a", StrategyEshu, "used"),
	}
	report, err := Score("sample", questions, predictions)
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if report.Questions[0].QuestionID != "q-a" || report.Questions[1].QuestionID != "q-z" {
		t.Errorf("questions not sorted by id: %+v", report.Questions)
	}
}

func TestScoreRejectsMalformedInput(t *testing.T) {
	good := sampleQuestions()
	tests := []struct {
		name        string
		questions   []Question
		predictions []Prediction
	}{
		{
			name:        "empty questions",
			questions:   nil,
			predictions: nil,
		},
		{
			name:        "invalid label",
			questions:   []Question{{ID: "q1", Label: "bogus"}},
			predictions: []Prediction{pred("q1", StrategyBaseline, "used"), pred("q1", StrategyEshu, "used")},
		},
		{
			name:      "missing eshu prediction",
			questions: []Question{{ID: "q1", Label: "used"}},
			predictions: []Prediction{
				pred("q1", StrategyBaseline, "used"),
			},
		},
		{
			name:      "invalid answer",
			questions: []Question{{ID: "q1", Label: "used"}},
			predictions: []Prediction{
				pred("q1", StrategyBaseline, "bogus"),
				pred("q1", StrategyEshu, "used"),
			},
		},
		{
			name:      "duplicate prediction",
			questions: []Question{{ID: "q1", Label: "used"}},
			predictions: []Prediction{
				pred("q1", StrategyBaseline, "used"),
				pred("q1", StrategyBaseline, "used"),
				pred("q1", StrategyEshu, "used"),
			},
		},
		{
			name:      "prediction for unknown question",
			questions: []Question{{ID: "q1", Label: "used"}},
			predictions: []Prediction{
				pred("q1", StrategyBaseline, "used"),
				pred("q1", StrategyEshu, "used"),
				pred("ghost", StrategyEshu, "used"),
			},
		},
		{
			name: "duplicate question id",
			questions: []Question{
				{ID: "dup", Label: "used"},
				{ID: "dup", Label: "unused"},
			},
			predictions: []Prediction{
				pred("dup", StrategyBaseline, "used"),
				pred("dup", StrategyEshu, "used"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Score("sample", tt.questions, tt.predictions); err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
		})
	}
	// Sanity: the good fixture with full predictions must still score.
	full := make([]Prediction, 0, len(good)*2)
	for _, q := range good {
		full = append(full, pred(q.ID, StrategyBaseline, q.Label), pred(q.ID, StrategyEshu, q.Label))
	}
	if _, err := Score("sample", good, full); err != nil {
		t.Fatalf("good fixture failed to score: %v", err)
	}
}
