// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"
)

func TestInvestigationWorkflowResolveIncidentContextForwardsBoundedIncidentInputs(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"incident_id":         "INC-1",
			"provider":            "pagerduty",
			"provider_service_id": "PD-SVC-1",
			"scope_id":            "scope-1",
			"service_id":          "checkout",
			"since":               "2026-06-18T00:00:00Z",
			"until":               "2026-06-19T00:00:00Z",
		},
		MissingEvidence: []string{"incident"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 1 {
		t.Fatalf("next calls = %#v, want one incident context call", resolved.RecommendedNextCalls)
	}
	call := resolved.RecommendedNextCalls[0]
	if got, want := call.Tool, "get_incident_context"; got != want {
		t.Fatalf("tool = %q, want %q", got, want)
	}
	wantArgs := map[string]any{
		"limit":                25,
		"provider":             "pagerduty",
		"provider_incident_id": "INC-1",
		"scope_id":             "scope-1",
		"service_id":           "PD-SVC-1",
		"since":                "2026-06-18T00:00:00Z",
		"until":                "2026-06-19T00:00:00Z",
	}
	if !reflect.DeepEqual(call.Arguments, wantArgs) {
		t.Fatalf("arguments = %#v, want %#v", call.Arguments, wantArgs)
	}
	if got := call.Arguments["service_id"]; got == "checkout" {
		t.Fatalf("incident provider service_id reused workflow service selector: %#v", call.Arguments)
	}
}

func TestInvestigationWorkflowResolveIncidentContextRoutesServiceRuntimeAndFreshness(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"environment": "prod",
			"repo_id":     "repo-checkout",
			"scope_id":    "scope-1",
			"service_id":  "checkout",
		},
		MissingEvidence: []string{"service", "runtime", "freshness"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	calls := map[string]ResolvedWorkflowCall{}
	for _, call := range resolved.RecommendedNextCalls {
		calls[call.ID] = call
	}

	service := calls["service_story"]
	if got, want := service.Tool, "get_service_story"; got != want {
		t.Fatalf("service tool = %q, want %q", got, want)
	}
	if got, want := service.Arguments["workload_id"], "checkout"; got != want {
		t.Fatalf("service workload_id = %#v, want %#v", got, want)
	}
	if got, want := service.Arguments["repo_id"], "repo-checkout"; got != want {
		t.Fatalf("service repo_id = %#v, want %#v", got, want)
	}

	runtime := calls["deployment_chain"]
	if got, want := runtime.Tool, "trace_deployment_chain"; got != want {
		t.Fatalf("runtime tool = %q, want %q", got, want)
	}
	if got, want := runtime.Arguments["service_name"], "checkout"; got != want {
		t.Fatalf("runtime service_name = %#v, want %#v", got, want)
	}
	if got, want := runtime.Arguments["direct_only"], true; got != want {
		t.Fatalf("runtime direct_only = %#v, want %#v", got, want)
	}

	freshness := calls["generation_lifecycle"]
	if got, want := freshness.Tool, "get_generation_lifecycle"; got != want {
		t.Fatalf("freshness tool = %q, want %q", got, want)
	}
	if got, want := freshness.Arguments["scope_id"], "scope-1"; got != want {
		t.Fatalf("freshness scope_id = %#v, want %#v", got, want)
	}
	if got, want := freshness.Arguments["repository"], "repo-checkout"; got != want {
		t.Fatalf("freshness repository = %#v, want %#v", got, want)
	}
}

func TestInvestigationWorkflowResolveIncidentContextAllowsEnvironmentAnchoredChanges(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs: map[string]string{
			"environment": "prod",
		},
		MissingEvidence: []string{"changes"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 1 {
		t.Fatalf("next calls = %#v, want one changes call", resolved.RecommendedNextCalls)
	}
	call := resolved.RecommendedNextCalls[0]
	if got, want := call.Tool, "list_ci_cd_run_correlations"; got != want {
		t.Fatalf("tool = %q, want %q", got, want)
	}
	if got, want := call.Arguments["environment"], "prod"; got != want {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestInvestigationWorkflowResolveIncidentContextBlocksUnanchoredOptionalEvidence(t *testing.T) {
	t.Parallel()

	workflow, ok := LookupInvestigationWorkflow("guided_incident_context")
	if !ok {
		t.Fatal("guided_incident_context missing")
	}
	resolved, err := workflow.Resolve(InvestigationWorkflowResolveInput{
		Inputs:          map[string]string{},
		MissingEvidence: []string{"incident", "service", "runtime", "observability", "changes", "freshness"},
	})
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if len(resolved.RecommendedNextCalls) != 0 {
		t.Fatalf("recommended calls = %#v, want none without service, repository, or scope anchors", resolved.RecommendedNextCalls)
	}
	blocked := map[string]BlockedWorkflowCall{}
	for _, call := range resolved.BlockedNextCalls {
		blocked[call.ID] = call
	}
	for _, id := range []string{"incident_context", "service_story", "deployment_chain", "observability_coverage", "service_changes", "generation_lifecycle"} {
		call, ok := blocked[id]
		if !ok {
			t.Fatalf("blocked call %q missing in %#v", id, resolved.BlockedNextCalls)
		}
		if len(call.RequiredInputsAny) == 0 {
			t.Fatalf("blocked call %q required inputs empty", id)
		}
	}
}
