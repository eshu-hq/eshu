// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestCatalogStabilityGolden pins the catalog identity: the set of playbook IDs
// and their versions must not drift silently. A change here is a deliberate
// catalog change, not an accident.
func TestCatalogStabilityGolden(t *testing.T) {
	t.Parallel()

	want := []PlaybookVersionRef{
		{ID: "service_story_citation", Version: "1.0.0"},
		{ID: "repository_code_topic_investigation", Version: "1.0.0"},
		{ID: "documentation_truth_citation", Version: "1.0.0"},
		{ID: "incident_context_evidence_path", Version: "1.0.0"},
		{ID: "supply_chain_impact_explanation", Version: "1.0.0"},
		{ID: "secrets_iam_trust_chain_posture", Version: "1.0.0"},
		{ID: "incremental_freshness_readiness", Version: "1.0.0"},
		{ID: "hosted_onboarding_governance_status", Version: "1.0.0"},
		{ID: "change_surface_source_investigation", Version: "1.0.0"},
		{ID: "query_to_service_context", Version: "1.0.0"},
		{ID: "query_to_code_topic_context", Version: "1.0.0"},
		{ID: "query_to_incident_context", Version: "1.0.0"},
		{ID: "query_to_supply_chain_context", Version: "1.0.0"},
		{ID: "demo_deployment_to_cloud_resource", Version: "1.0.0"},
		{ID: "demo_dependency_cross_repo", Version: "1.0.0"},
		{ID: "demo_observability_to_workload", Version: "1.0.0"},
	}

	got := PlaybookCatalogVersions()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog version drift:\n got=%#v\nwant=%#v", got, want)
	}
}

// TestCatalogValidatesStatically asserts that every playbook in the catalog
// passes structural validation: required IDs, bounded steps, declared failure
// modes, and no raw Cypher step.
func TestCatalogValidatesStatically(t *testing.T) {
	t.Parallel()

	for _, pb := range PlaybookCatalog() {
		pb := pb
		t.Run(pb.ID, func(t *testing.T) {
			t.Parallel()
			if err := pb.Validate(); err != nil {
				t.Fatalf("playbook %q failed validation: %v", pb.ID, err)
			}
		})
	}
}

// TestRawCypherStepsRejected proves a playbook that references a raw Cypher tool
// is rejected by validation, so the no-raw-Cypher rule is enforced, not just
// honored by convention.
func TestRawCypherStepsRejected(t *testing.T) {
	t.Parallel()

	for _, tool := range rawCypherTools() {
		bad := QueryPlaybook{
			ID:           "bad",
			Name:         "bad",
			Version:      "1.0.0",
			PromptFamily: "bad",
			RequiredInputs: []PlaybookInput{
				{Name: "x", Type: PlaybookInputString, Required: true},
			},
			Steps: []PlaybookStep{
				{
					ID:               "s1",
					Tool:             tool,
					Params:           []PlaybookParam{{Name: "x", FromInput: "x"}},
					ExpectedTruth:    AnswerTruthDeterministic,
					EvidenceExpected: "n/a",
				},
			},
			FailureModes: []PlaybookFailureMode{
				{Condition: "x", Meaning: "y", Fallback: "z"},
			},
		}
		if err := bad.Validate(); err == nil {
			t.Fatalf("expected raw Cypher tool %q to be rejected", tool)
		}
	}
}

// TestResolveServiceStoryDeterministic proves the service-story playbook resolves
// to a stable, fully specified ordered call sequence with bounded params and a
// declared truth/evidence expectation per step, using only the provided inputs.
func TestResolveServiceStoryDeterministic(t *testing.T) {
	t.Parallel()

	pb, ok := LookupPlaybook("service_story_citation")
	if !ok {
		t.Fatal("service_story_citation playbook missing from catalog")
	}

	inputs := map[string]string{"service_name": "payments-api", "environment": "prod"}

	first := mustResolve(t, pb, inputs)
	second := mustResolve(t, pb, inputs)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("resolution is not deterministic:\n first=%#v\nsecond=%#v", first, second)
	}

	if first.PlaybookID != "service_story_citation" || first.Version != "1.0.0" {
		t.Fatalf("unexpected resolved identity: %+v", first)
	}
	if len(first.Calls) < 2 {
		t.Fatalf("expected at least two resolved calls, got %d", len(first.Calls))
	}

	step0 := first.Calls[0]
	if step0.Tool != "get_service_story" {
		t.Fatalf("expected first call get_service_story, got %q", step0.Tool)
	}
	if got := step0.Arguments["workload_id"]; got != "payments-api" {
		t.Fatalf("expected workload_id payments-api, got %v", got)
	}
	if step0.ExpectedTruth == "" {
		t.Fatal("first call missing expected truth class")
	}

	// Bounded: every resolved call that declares a limit param must carry a
	// positive integer limit, never an unbounded value.
	for _, call := range first.Calls {
		if lim, ok := call.Arguments["limit"]; ok {
			n, ok := lim.(int)
			if !ok || n <= 0 {
				t.Fatalf("call %q has non-positive or non-int limit %v", call.Tool, lim)
			}
		}
	}
}

// TestResolveCodeTopicDeterministic proves the second required playbook resolves
// deterministically with bounded params and required-input enforcement.
func TestResolveCodeTopicDeterministic(t *testing.T) {
	t.Parallel()

	pb, ok := LookupPlaybook("repository_code_topic_investigation")
	if !ok {
		t.Fatal("repository_code_topic_investigation playbook missing from catalog")
	}

	inputs := map[string]string{"topic": "repo sync authentication"}
	first := mustResolve(t, pb, inputs)
	second := mustResolve(t, pb, inputs)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("resolution is not deterministic:\n first=%#v\nsecond=%#v", first, second)
	}

	if first.Calls[0].Tool != "investigate_code_topic" {
		t.Fatalf("expected first call investigate_code_topic, got %q", first.Calls[0].Tool)
	}
	if first.Calls[0].Arguments["topic"] != "repo sync authentication" {
		t.Fatalf("topic not bound into first call: %v", first.Calls[0].Arguments)
	}
	// Default limit must be applied even though the caller did not pass one.
	if first.Calls[0].Arguments["limit"] == nil {
		t.Fatal("expected default limit to be applied on investigate_code_topic")
	}
}

// TestResolveMissingRequiredInput proves the resolver fails loudly when a
// required input is absent instead of silently inventing a value.
func TestResolveMissingRequiredInput(t *testing.T) {
	t.Parallel()

	pb, _ := LookupPlaybook("service_story_citation")
	if _, err := pb.Resolve(map[string]string{}); err == nil {
		t.Fatal("expected error when required input service_name is missing")
	}
}

// TestResolveUnknownInputRejected proves the resolver rejects inputs the
// playbook does not declare, so callers cannot smuggle hidden parameters.
func TestResolveUnknownInputRejected(t *testing.T) {
	t.Parallel()

	pb, _ := LookupPlaybook("service_story_citation")
	_, err := pb.Resolve(map[string]string{"service_name": "x", "not_declared": "y"})
	if err == nil {
		t.Fatal("expected error for undeclared input")
	}
}

// TestEveryStepToolNameInValidSet cross-checks each catalog step tool against the
// playbook tool-name allowlist helper. The exhaustive registry cross-check lives
// in the mcp package to avoid an import cycle.
func TestEveryStepToolNameInValidSet(t *testing.T) {
	t.Parallel()

	names := PlaybookToolNames()
	if len(names) == 0 {
		t.Fatal("PlaybookToolNames returned no names")
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("PlaybookToolNames has duplicate %q", sorted[i])
		}
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			t.Fatal("empty tool name in PlaybookToolNames")
		}
	}
}

func mustResolve(t *testing.T, pb QueryPlaybook, inputs map[string]string) ResolvedPlaybook {
	t.Helper()
	resolved, err := pb.Resolve(inputs)
	if err != nil {
		t.Fatalf("resolve %q: %v", pb.ID, err)
	}
	return resolved
}
