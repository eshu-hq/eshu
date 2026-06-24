// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate

import "testing"

func TestScorePredictionsPerfectCorrelation(t *testing.T) {
	t.Parallel()

	metric := ScorePredictions([]LabeledPrediction{
		{Label: "case-a", Expected: "admitted", Observed: "admitted", Positive: true},
		{Label: "case-b", Expected: "admitted", Observed: "admitted", Positive: true},
		{Label: "case-c", Expected: "rejected", Observed: "rejected", Positive: false},
	})
	if metric.Precision != 1.0 || metric.Recall != 1.0 {
		t.Fatalf("precision/recall = %.3f/%.3f, want 1.0/1.0", metric.Precision, metric.Recall)
	}
	if metric.CoveredItems != 2 {
		t.Fatalf("covered = %d, want 2 admitted positives", metric.CoveredItems)
	}
}

func TestScorePredictionsFalsePositiveLowersPrecision(t *testing.T) {
	t.Parallel()

	// A rejected case wrongly observed as admitted is a false positive: it
	// inflates observed positives without a matching expected positive.
	metric := ScorePredictions([]LabeledPrediction{
		{Label: "case-a", Expected: "admitted", Observed: "admitted", Positive: true},
		{Label: "case-b", Expected: "rejected", Observed: "admitted", Positive: false},
	})
	if metric.Precision != 0.5 {
		t.Fatalf("precision = %.3f, want 0.5 (1 correct of 2 observed positives)", metric.Precision)
	}
	if metric.Recall != 1.0 {
		t.Fatalf("recall = %.3f, want 1.0 (1 correct of 1 expected positive)", metric.Recall)
	}
}

func TestScorePredictionsFalseNegativeLowersRecall(t *testing.T) {
	t.Parallel()

	// An admitted case wrongly observed as rejected is a false negative: the
	// expected positive is missed.
	metric := ScorePredictions([]LabeledPrediction{
		{Label: "case-a", Expected: "admitted", Observed: "admitted", Positive: true},
		{Label: "case-b", Expected: "admitted", Observed: "rejected", Positive: true},
	})
	if metric.Recall != 0.5 {
		t.Fatalf("recall = %.3f, want 0.5 (1 correct of 2 expected positives)", metric.Recall)
	}
	if metric.Precision != 1.0 {
		t.Fatalf("precision = %.3f, want 1.0 (1 correct of 1 observed positive)", metric.Precision)
	}
}

func TestCoverageMetricCountsScoredItems(t *testing.T) {
	t.Parallel()

	all := map[string]string{
		"go":     "scored:straight=1,branchy=5",
		"python": "scored:straight=1,branchy=4",
		"sql":    "unscored:no-function-ast",
	}
	metric := CoverageMetric([]string{"go", "python"}, all)
	if metric.CoveredItems != 2 {
		t.Fatalf("covered = %d, want 2", metric.CoveredItems)
	}
	want := float64(2) / float64(3)
	if metric.Precision != want || metric.Recall != want {
		t.Fatalf("precision/recall = %.3f/%.3f, want %.3f", metric.Precision, metric.Recall, want)
	}
	if len(metric.Labels) != 3 {
		t.Fatalf("labels = %d, want 3", len(metric.Labels))
	}
}

func TestCoverageMetricEmptyScoresPerfect(t *testing.T) {
	t.Parallel()

	metric := CoverageMetric(nil, map[string]string{})
	if metric.Precision != 1.0 || metric.Recall != 1.0 {
		t.Fatalf("empty coverage precision/recall = %.3f/%.3f, want 1.0/1.0", metric.Precision, metric.Recall)
	}
}
