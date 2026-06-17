package goldenaudit

import "testing"

func TestScoreAccuracyAllCorrectIsPerfect(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			{SourceID: "func:server.handle", TargetID: "func:store.Persist", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision != 1.0 {
		t.Fatalf("overall precision = %v, want 1.0: %s", result.Overall.Precision, result.Summary())
	}
	if result.Overall.Recall != 1.0 {
		t.Fatalf("overall recall = %v, want 1.0: %s", result.Overall.Recall, result.Summary())
	}
	callsType := findTypeAccuracy(t, result, "CALLS")
	if callsType.Precision != 1.0 || callsType.Recall != 1.0 {
		t.Fatalf("CALLS precision/recall = %v/%v, want 1.0/1.0", callsType.Precision, callsType.Recall)
	}
	if len(result.WrongTarget) != 0 || len(result.Missing) != 0 || len(result.Extra) != 0 {
		t.Fatalf("expected empty mismatch breakdown, got %s", result.Summary())
	}
}

func TestScoreAccuracyWrongTargetEdgeLowersPrecision(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			// Same source+type as golden's handle->Persist edge, but wrong target.
			{SourceID: "func:server.handle", TargetID: "func:store.Lookup", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision >= 1.0 {
		t.Fatalf("overall precision = %v, want < 1.0", result.Overall.Precision)
	}
	assertEdgeKeys(t, "wrong-target edges", result.WrongTarget,
		[]string{"func:server.handle|CALLS|func:store.Lookup"})
	if len(result.Extra) != 0 {
		t.Fatalf("wrong-target edge must not be counted as extra: %s", result.Summary())
	}
}

func TestScoreAccuracyMissingGoldenEdgeLowersRecall(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			// golden's func:server.handle -> func:store.Persist edge is absent.
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Recall >= 1.0 {
		t.Fatalf("overall recall = %v, want < 1.0", result.Overall.Recall)
	}
	assertEdgeKeys(t, "missing edges", result.Missing,
		[]string{"func:server.handle|CALLS|func:store.Persist"})
}

func TestScoreAccuracyExtraObservedEdgeLowersPrecision(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			{SourceID: "func:server.handle", TargetID: "func:store.Persist", Type: "CALLS"},
			// Extra: no golden edge shares this source+type.
			{SourceID: "func:store.Persist", TargetID: "func:store.Lookup", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision >= 1.0 {
		t.Fatalf("overall precision = %v, want < 1.0", result.Overall.Precision)
	}
	assertEdgeKeys(t, "extra edges", result.Extra,
		[]string{"func:store.Persist|CALLS|func:store.Lookup"})
	if len(result.WrongTarget) != 0 {
		t.Fatalf("extra edge must not be counted as wrong-target: %s", result.Summary())
	}
}

func TestScoreAccuracyEmptyGraphsConventionIsPerfect(t *testing.T) {
	t.Parallel()

	result := ScoreAccuracy(Graph{}, Graph{})
	if result.Overall.Precision != 1.0 || result.Overall.Recall != 1.0 {
		t.Fatalf("empty-vs-empty precision/recall = %v/%v, want 1.0/1.0",
			result.Overall.Precision, result.Overall.Recall)
	}
}

func findTypeAccuracy(t *testing.T, result AccuracyResult, relType string) TypeAccuracy {
	t.Helper()

	for _, ta := range result.ByType {
		if ta.Type == relType {
			return ta
		}
	}
	t.Fatalf("no per-type accuracy for %q in %v", relType, result.ByType)
	return TypeAccuracy{}
}
