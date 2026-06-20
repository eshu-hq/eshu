package query

import (
	"reflect"
	"testing"
)

func TestInvestigationWorkflowResolveDeployableDriftAdmissionUsesBoundedInputs(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_deployable_drift")
	if !ok {
		t.Fatal("guided_deployable_drift missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"deployable_unit_id": "workload:checkout",
			"generation_id":      "gen-1",
			"repo_id":            "repo-checkout",
			"scope_id":           "scope-1",
		},
		MissingEvidence: []string{"admission"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 1 {
		t.Fatalf("next calls = %#v, want one admission call", resolved.RecommendedNextCalls)
	}
	call := resolved.RecommendedNextCalls[0]
	if got, want := call.Tool, "list_admission_decisions"; got != want {
		t.Fatalf("tool = %q, want %q", got, want)
	}
	wantArgs := map[string]any{
		"anchor_id":        "repo-checkout",
		"anchor_kind":      "repository",
		"domain":           "deployable_unit_correlation",
		"generation_id":    "gen-1",
		"include_evidence": true,
		"limit":            10,
		"scope_id":         "scope-1",
	}
	if !reflect.DeepEqual(call.Arguments, wantArgs) {
		t.Fatalf("arguments = %#v, want %#v", call.Arguments, wantArgs)
	}
}

func TestInvestigationWorkflowResolveDeployableDriftBlocksAdmissionWithoutRepositoryAnchor(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_deployable_drift")
	if !ok {
		t.Fatal("guided_deployable_drift missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"deployable_unit_id": "workload:checkout",
			"generation_id":      "gen-1",
			"scope_id":           "scope-1",
		},
		MissingEvidence: []string{"admission"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 0 {
		t.Fatalf("recommended calls = %#v, want none without repository anchor", resolved.RecommendedNextCalls)
	}
	if len(resolved.BlockedNextCalls) != 1 {
		t.Fatalf("blocked calls = %#v, want one blocked admission call", resolved.BlockedNextCalls)
	}
	call := resolved.BlockedNextCalls[0]
	if got, want := call.ID, "admission_decision"; got != want {
		t.Fatalf("blocked call ID = %q, want %q", got, want)
	}
	if got, want := call.RequiredInputsAny, []string{"repo_id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required inputs = %#v, want %#v", got, want)
	}
}

func TestInvestigationWorkflowResolveDeployableDriftRoutesRuntimeServiceAndFreshness(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_deployable_drift")
	if !ok {
		t.Fatal("guided_deployable_drift missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"deployable_unit_id": "workload:checkout",
			"generation_id":      "gen-1",
			"provider":           "aws",
			"scope_id":           "scope-1",
		},
		MissingEvidence: []string{"runtime", "service", "freshness"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	calls := map[string]ResolvedWorkflowCall{}
	for _, call := range resolved.RecommendedNextCalls {
		calls[call.ID] = call
	}

	runtime := calls["runtime_drift"]
	if got, want := runtime.Tool, "list_cloud_runtime_drift_findings"; got != want {
		t.Fatalf("runtime tool = %q, want %q", got, want)
	}
	if got, want := runtime.Arguments["scope_id"], "scope-1"; got != want {
		t.Fatalf("runtime scope_id = %#v, want %#v", got, want)
	}
	if got, want := runtime.Arguments["provider"], "aws"; got != want {
		t.Fatalf("runtime provider = %#v, want %#v", got, want)
	}

	service := calls["workload_story"]
	if got, want := service.Tool, "get_workload_story"; got != want {
		t.Fatalf("service tool = %q, want %q", got, want)
	}
	if got, want := service.Arguments["workload_id"], "workload:checkout"; got != want {
		t.Fatalf("service workload_id = %#v, want %#v", got, want)
	}

	freshness := calls["generation_lifecycle"]
	if got, want := freshness.Tool, "get_generation_lifecycle"; got != want {
		t.Fatalf("freshness tool = %q, want %q", got, want)
	}
	if got, want := freshness.Arguments["scope_id"], "scope-1"; got != want {
		t.Fatalf("freshness scope_id = %#v, want %#v", got, want)
	}
	if got, want := freshness.Arguments["generation_id"], "gen-1"; got != want {
		t.Fatalf("freshness generation_id = %#v, want %#v", got, want)
	}
}
