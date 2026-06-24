// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"strings"
	"testing"
)

// queryToContextPlaybookIDs is the third-wave family that starts from semantic
// search and bridges into bounded readbacks.
var queryToContextPlaybookIDs = []string{
	"query_to_service_context",
	"query_to_code_topic_context",
	"query_to_incident_context",
	"query_to_supply_chain_context",
}

func TestCatalogIncludesQueryToContextPlaybooks(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	for _, pb := range PlaybookCatalog() {
		seen[pb.ID] = pb.Version
	}
	for _, id := range queryToContextPlaybookIDs {
		if version, ok := seen[id]; !ok || version != "1.0.0" {
			t.Fatalf("playbook %q version = %q present=%t; want 1.0.0", id, version, ok)
		}
	}
}

func TestQueryToContextPlaybooksStartWithSemanticSearch(t *testing.T) {
	t.Parallel()

	for _, id := range queryToContextPlaybookIDs {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			pb, ok := LookupPlaybook(id)
			if !ok {
				t.Fatalf("playbook %q missing", id)
			}
			if len(pb.Steps) == 0 || pb.Steps[0].Tool != "search_semantic_context" {
				t.Fatalf("playbook %q must start with search_semantic_context, got %#v", id, pb.Steps)
			}
			// Search is read-only context discovery: derived truth, never canonical.
			if pb.Steps[0].ExpectedTruth != AnswerTruthDerived {
				t.Fatalf("search step expected_truth = %q, want derived", pb.Steps[0].ExpectedTruth)
			}
			// The search step opts into graph-neighborhood reranking so its
			// recommended_next_calls drive the following steps.
			rerankSet := false
			boundedLimit := false
			for _, p := range pb.Steps[0].Params {
				if p.Name == "rerank" && p.source() == PlaybookParamConstBool && p.ConstBool {
					rerankSet = true
				}
				if p.Name == "limit" && p.source() == PlaybookParamConstInt && p.ConstInt > 0 {
					boundedLimit = true
				}
			}
			if !rerankSet {
				t.Fatalf("playbook %q search step must set rerank:true", id)
			}
			if !boundedLimit {
				t.Fatalf("playbook %q search step must declare a bounded limit", id)
			}
		})
	}
}

func TestQueryToContextResolveIsDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	pb, ok := LookupPlaybook("query_to_service_context")
	if !ok {
		t.Fatal("query_to_service_context missing")
	}
	inputs := map[string]string{"repo_id": "repo-x", "query": "how does checkout charge cards"}
	r1, err := pb.Resolve(inputs)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	r2, err := pb.Resolve(inputs)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("resolve is not deterministic:\n%#v\n%#v", r1, r2)
	}

	first := r1.Calls[0]
	if first.Tool != "search_semantic_context" {
		t.Fatalf("first call tool = %q, want search_semantic_context", first.Tool)
	}
	if got := first.Arguments["repo_id"]; got != "repo-x" {
		t.Fatalf("first call repo_id = %#v, want repo-x", got)
	}
	if got := first.Arguments["query"]; got != "how does checkout charge cards" {
		t.Fatalf("first call query = %#v", got)
	}
	if got := first.Arguments["rerank"]; got != true {
		t.Fatalf("first call rerank = %#v, want true", got)
	}
	if _, ok := first.Arguments["limit"]; !ok {
		t.Fatalf("first call must be bounded with a limit: %#v", first.Arguments)
	}

	tools := map[string]bool{}
	for _, c := range r1.Calls {
		tools[c.Tool] = true
	}
	if !tools["get_service_story"] {
		t.Fatalf("service playbook must hand off to get_service_story, got %#v", tools)
	}
	if !tools["build_evidence_citation_packet"] {
		t.Fatalf("service playbook must cite evidence, got %#v", tools)
	}
}

func TestQueryToContextPerFamilyHandoff(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"query_to_service_context":      "get_service_story",
		"query_to_code_topic_context":   "investigate_code_topic",
		"query_to_incident_context":     "get_incident_context",
		"query_to_supply_chain_context": "explain_supply_chain_impact",
	}
	for id, readback := range want {
		pb, ok := LookupPlaybook(id)
		if !ok {
			t.Fatalf("playbook %q missing", id)
		}
		found := false
		for _, step := range pb.Steps {
			if step.Tool == readback {
				found = true
			}
		}
		if !found {
			t.Fatalf("playbook %q must hand off to %q", id, readback)
		}
	}
}

func TestQueryToContextResolveRejectsMissingRequiredInput(t *testing.T) {
	t.Parallel()

	pb, ok := LookupPlaybook("query_to_incident_context")
	if !ok {
		t.Fatal("query_to_incident_context missing")
	}
	if _, err := pb.Resolve(map[string]string{"repo_id": "repo-x"}); err == nil {
		t.Fatal("resolve must reject a missing required query input")
	}
}

func TestQueryToContextPlaybooksDeclareReadinessFailureModes(t *testing.T) {
	t.Parallel()

	// #2679 requires each playbook to declare the failure modes a caller hits:
	// missing search readiness, no hits, stale vectors, ambiguous target, and
	// truncation.
	wantSubstrings := []string{"search", "no ", "stale", "ambiguous", "truncat"}
	for _, id := range queryToContextPlaybookIDs {
		pb, ok := LookupPlaybook(id)
		if !ok {
			t.Fatalf("playbook %q missing", id)
		}
		conditions := strings.ToLower(joinFailureConditions(pb))
		for _, want := range wantSubstrings {
			if !strings.Contains(conditions, want) {
				t.Fatalf("playbook %q failure modes must cover %q, got: %s", id, want, conditions)
			}
		}
	}
}

func TestPlaybookParamRejectsMultipleValueSources(t *testing.T) {
	t.Parallel()

	bad := QueryPlaybook{
		ID:           "bad_multi_source",
		Name:         "bad",
		Version:      "1.0.0",
		PromptFamily: "bad",
		RequiredInputs: []PlaybookInput{
			{Name: "repo_id", Type: PlaybookInputIdentifier, Required: true},
		},
		Steps: []PlaybookStep{
			{
				ID:   "step",
				Tool: "search_semantic_context",
				Params: []PlaybookParam{
					// Declares both an input binding and a constant int.
					{Name: "limit", FromInput: "repo_id", ConstInt: 5, hasConstInt: true},
				},
				ExpectedTruth:    AnswerTruthDerived,
				EvidenceExpected: "x",
			},
		},
		FailureModes: []PlaybookFailureMode{
			{Condition: "c", Meaning: "m", Fallback: "f"},
		},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("Validate must reject a param declaring multiple value sources")
	}
}

func TestQueryToContextBoolParamResolvesAsBool(t *testing.T) {
	t.Parallel()

	// A const_bool param resolves to a real bool, not the string "true".
	pb, _ := LookupPlaybook("query_to_service_context")
	resolved, err := pb.Resolve(map[string]string{"repo_id": "r", "query": "q"})
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	value, ok := resolved.Calls[0].Arguments["rerank"]
	if !ok {
		t.Fatal("rerank argument missing from resolved search call")
	}
	if _, isBool := value.(bool); !isBool {
		t.Fatalf("rerank argument type = %T, want bool", value)
	}
}

func joinFailureConditions(pb QueryPlaybook) string {
	parts := make([]string, 0, len(pb.FailureModes))
	for _, fm := range pb.FailureModes {
		parts = append(parts, fm.Condition)
	}
	return strings.Join(parts, " | ")
}
