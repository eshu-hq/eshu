// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldenaudit

import (
	"strings"
	"testing"
)

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

func TestScoreAccuracyPythonAllCorrectIsPerfect(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "python_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:handlers.handle_request", TargetID: "func:handlers.validate_input", Type: "CALLS"},
			{SourceID: "func:handlers.handle_request", TargetID: "func:repository.save_record", Type: "CALLS"},
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

func TestScoreAccuracyPythonWrongTargetEdgeLowersPrecision(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "python_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			// Correct edge.
			{SourceID: "func:handlers.handle_request", TargetID: "func:handlers.validate_input", Type: "CALLS"},
			// Same source+type as golden's handle_request->save_record edge, but wrong target.
			{SourceID: "func:handlers.handle_request", TargetID: "func:repository.load_record", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision != 0.5 {
		t.Fatalf("overall precision = %v, want 0.5: %s", result.Overall.Precision, result.Summary())
	}
	if result.Overall.Recall != 0.5 {
		t.Fatalf("overall recall = %v, want 0.5: %s", result.Overall.Recall, result.Summary())
	}
	assertEdgeKeys(t, "wrong-target edges", result.WrongTarget,
		[]string{"func:handlers.handle_request|CALLS|func:repository.load_record"})
	if len(result.Extra) != 0 {
		t.Fatalf("wrong-target edge must not be counted as extra: %s", result.Summary())
	}
}

func TestScoreAccuracyJavaAllCorrectIsPerfect(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "java_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "method:OrderService.placeOrder", TargetID: "method:OrderService.reserveStock", Type: "CALLS"},
			{SourceID: "method:OrderService.placeOrder", TargetID: "method:OrderService.chargePayment", Type: "CALLS"},
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

func TestScoreAccuracyJavaWrongTargetEdgeLowersPrecision(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "java_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			// Correct edge.
			{SourceID: "method:OrderService.placeOrder", TargetID: "method:OrderService.reserveStock", Type: "CALLS"},
			// Same source+type as golden's placeOrder->chargePayment edge, but wrong target.
			{SourceID: "method:OrderService.placeOrder", TargetID: "method:OrderService.cancelOrder", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision != 0.5 {
		t.Fatalf("overall precision = %v, want 0.5: %s", result.Overall.Precision, result.Summary())
	}
	if result.Overall.Recall != 0.5 {
		t.Fatalf("overall recall = %v, want 0.5: %s", result.Overall.Recall, result.Summary())
	}
	assertEdgeKeys(t, "wrong-target edges", result.WrongTarget,
		[]string{"method:OrderService.placeOrder|CALLS|method:OrderService.cancelOrder"})
	if len(result.Extra) != 0 {
		t.Fatalf("wrong-target edge must not be counted as extra: %s", result.Summary())
	}
}

func TestMeetsThresholdPerfectResultPasses(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			{SourceID: "func:server.handle", TargetID: "func:store.Persist", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	ok, msg := result.MeetsThreshold(1.0, 1.0)
	if !ok {
		t.Fatalf("MeetsThreshold(1.0, 1.0) = false, want true: %s", msg)
	}
	if msg != "" {
		t.Fatalf("MeetsThreshold(1.0, 1.0) msg = %q, want empty", msg)
	}
}

func TestMeetsThresholdWrongTargetFailsAndReportsEdge(t *testing.T) {
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
	ok, msg := result.MeetsThreshold(1.0, 1.0)
	if ok {
		t.Fatalf("MeetsThreshold(1.0, 1.0) = true, want false for wrong-target result: %s", result.Summary())
	}
	if !strings.Contains(msg, "func:server.handle|CALLS|func:store.Lookup") {
		t.Fatalf("MeetsThreshold msg missing wrong-target edge key, got:\n%s", msg)
	}
	if !strings.Contains(msg, "0.500") {
		t.Fatalf("MeetsThreshold msg missing measured precision 0.500, got:\n%s", msg)
	}
}

func TestMeetsThresholdDisabledGatePasses(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			// Wrong target degrades precision and recall, but the gate is disabled.
			{SourceID: "func:server.handle", TargetID: "func:store.Lookup", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	ok, msg := result.MeetsThreshold(0, 0)
	if !ok {
		t.Fatalf("MeetsThreshold(0, 0) = false, want true (disabled gate): %s", msg)
	}
	if msg != "" {
		t.Fatalf("MeetsThreshold(0, 0) msg = %q, want empty", msg)
	}
}

func TestMeetsThresholdRecallOnlyFailure(t *testing.T) {
	t.Parallel()

	golden := loadTestGolden(t, "go_call_accuracy.json")
	observed := Graph{
		Edges: []Edge{
			// Correct edge only; golden's handle->Persist edge is missing.
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
		},
	}

	result := ScoreAccuracy(golden, observed)
	if result.Overall.Precision != 1.0 {
		t.Fatalf("precision = %v, want 1.0 for missing-only result: %s", result.Overall.Precision, result.Summary())
	}

	ok, msg := result.MeetsThreshold(1.0, 1.0)
	if ok {
		t.Fatalf("MeetsThreshold(1.0, 1.0) = true, want false when recall < 1.0: %s", result.Summary())
	}
	if !strings.Contains(msg, "func:server.handle|CALLS|func:store.Persist") {
		t.Fatalf("MeetsThreshold msg missing missing-edge key, got:\n%s", msg)
	}

	ok, msg = result.MeetsThreshold(1.0, 0)
	if !ok {
		t.Fatalf("MeetsThreshold(1.0, 0) = false, want true (recall gate disabled): %s", msg)
	}
	if msg != "" {
		t.Fatalf("MeetsThreshold(1.0, 0) msg = %q, want empty", msg)
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
