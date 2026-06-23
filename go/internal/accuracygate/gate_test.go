package accuracygate

import (
	"strings"
	"testing"
)

func baselineForTest() Baseline {
	return Baseline{
		SchemaVersion: schemaVersion,
		Thresholds: map[Dimension]Threshold{
			DimensionComplexity:  {MinPrecision: 1.0, MinRecall: 1.0, MinCoveredItems: 8},
			DimensionResolvers:   {MinPrecision: 0.9, MinRecall: 0.9, MinCoveredItems: 13},
			DimensionCorrelation: {MinPrecision: 1.0, MinRecall: 1.0, MinCoveredItems: 3},
		},
	}
}

func TestEvaluatePassesWhenAllDimensionsMeetFloor(t *testing.T) {
	t.Parallel()

	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity:  {Precision: 1.0, Recall: 1.0, CoveredItems: 10},
		DimensionResolvers:   {Precision: 1.0, Recall: 0.95, CoveredItems: 14},
		DimensionCorrelation: {Precision: 1.0, Recall: 1.0, CoveredItems: 6},
	}}

	result := Evaluate(baselineForTest(), measurement)
	if !result.Pass() {
		t.Fatalf("gate failed unexpectedly: %s\n%s", result.Summary(), result.FailureMessage())
	}
	if len(result.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(result.Results))
	}
	if result.Results[0].Dimension != DimensionComplexity {
		t.Fatalf("first dimension = %q, want complexity", result.Results[0].Dimension)
	}
}

func TestEvaluateFailsWhenPrecisionRegresses(t *testing.T) {
	t.Parallel()

	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity:  {Precision: 1.0, Recall: 1.0, CoveredItems: 10},
		DimensionResolvers:   {Precision: 0.5, Recall: 0.95, CoveredItems: 14},
		DimensionCorrelation: {Precision: 1.0, Recall: 1.0, CoveredItems: 6},
	}}

	result := Evaluate(baselineForTest(), measurement)
	if result.Pass() {
		t.Fatalf("gate passed despite resolver precision regression: %s", result.Summary())
	}
	msg := result.FailureMessage()
	if !strings.Contains(msg, "resolvers") || !strings.Contains(msg, "precision=0.500") {
		t.Fatalf("failure message missing resolver precision detail: %q", msg)
	}
	if strings.Contains(msg, "complexity") || strings.Contains(msg, "correlation") {
		t.Fatalf("failure message should only name the regressed dimension: %q", msg)
	}
}

func TestEvaluateFailsWhenCoverageDrops(t *testing.T) {
	t.Parallel()

	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity:  {Precision: 1.0, Recall: 1.0, CoveredItems: 5},
		DimensionResolvers:   {Precision: 1.0, Recall: 1.0, CoveredItems: 14},
		DimensionCorrelation: {Precision: 1.0, Recall: 1.0, CoveredItems: 6},
	}}

	result := Evaluate(baselineForTest(), measurement)
	if result.Pass() {
		t.Fatalf("gate passed despite complexity coverage drop: %s", result.Summary())
	}
	if !strings.Contains(result.FailureMessage(), "covered=5 below min 8") {
		t.Fatalf("failure message missing coverage detail: %q", result.FailureMessage())
	}
}

func TestEvaluateFailsWhenGatedDimensionHasNoMeasurement(t *testing.T) {
	t.Parallel()

	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity: {Precision: 1.0, Recall: 1.0, CoveredItems: 10},
		DimensionResolvers:  {Precision: 1.0, Recall: 1.0, CoveredItems: 14},
		// correlation deliberately absent
	}}

	result := Evaluate(baselineForTest(), measurement)
	if result.Pass() {
		t.Fatalf("gate passed despite missing correlation measurement: %s", result.Summary())
	}
	if !strings.Contains(result.FailureMessage(), "no measured metric") {
		t.Fatalf("failure message should flag missing measurement: %q", result.FailureMessage())
	}
}

func TestEvaluateSurfacesUngatedMeasuredDimension(t *testing.T) {
	t.Parallel()

	baseline := baselineForTest()
	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity:  {Precision: 1.0, Recall: 1.0, CoveredItems: 10},
		DimensionResolvers:   {Precision: 1.0, Recall: 1.0, CoveredItems: 14},
		DimensionCorrelation: {Precision: 1.0, Recall: 1.0, CoveredItems: 6},
		Dimension("future"):  {Precision: 0.1, Recall: 0.1, CoveredItems: 0},
	}}

	result := Evaluate(baseline, measurement)
	if !result.Pass() {
		t.Fatalf("ungated dimension must not fail the gate: %s\n%s", result.Summary(), result.FailureMessage())
	}
	var found bool
	for _, dimensionResult := range result.Results {
		if dimensionResult.Dimension == Dimension("future") {
			found = true
			if !dimensionResult.Pass {
				t.Fatalf("ungated dimension marked failing")
			}
		}
	}
	if !found {
		t.Fatalf("ungated measured dimension not surfaced in results")
	}
}

func TestPublishRendersDeterministicMetrics(t *testing.T) {
	t.Parallel()

	measurement := Measurement{Metrics: map[Dimension]Metric{
		DimensionComplexity:  {Precision: 1.0, Recall: 1.0, CoveredItems: 10, Labels: map[string]string{"go": "1->1"}},
		DimensionResolvers:   {Precision: 1.0, Recall: 1.0, CoveredItems: 14},
		DimensionCorrelation: {Precision: 1.0, Recall: 1.0, CoveredItems: 6},
	}}

	result := Evaluate(baselineForTest(), measurement)
	published := Publish(result)
	if !published.Pass {
		t.Fatalf("published metrics should pass")
	}
	first, err := published.Encode()
	if err != nil {
		t.Fatalf("encode error = %v", err)
	}
	second, err := published.Encode()
	if err != nil {
		t.Fatalf("encode error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("published metrics encoding not deterministic")
	}
	if !strings.Contains(string(first), schemaVersion) {
		t.Fatalf("published metrics missing schema version")
	}
}
