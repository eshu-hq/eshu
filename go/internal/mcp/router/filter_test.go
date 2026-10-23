// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/playbook"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func suggestionsFor(t *testing.T, ctx context.Context, typo string) []any {
	t.Helper()
	out := newFixtureEngine(t, orderRanker{}).Call(ctx, fixtureRequest(&recordingDispatcher{}),
		map[string]any{"name": typo, "arguments": map[string]any{}})
	if errorCode(t, out.Result) != "mcp_router_unknown_tool" {
		t.Fatalf("Call(%s) = %v, want mcp_router_unknown_tool", typo, structured(t, out.Result))
	}
	details := structured(t, out.Result)["error"].(map[string]any)["details"].(map[string]any)
	suggestions, _ := details["suggestions"].([]any)
	return suggestions
}

// TestUnknownToolSuggestionsHonorTheCallerFilter: a suggestion is only ever a
// name find_tool could offer the same caller.
func TestUnknownToolSuggestionsHonorTheCallerFilter(t *testing.T) {
	t.Parallel()

	if got := suggestionsFor(t, scopedContext(), "execute_cypher_quer"); slices.Contains(got, any("execute_cypher_query")) {
		t.Fatalf("scoped suggestions = %v, must not offer execute_cypher_query", got)
	}
	if got := suggestionsFor(t, context.Background(), "execute_cypher_quer"); len(got) == 0 || got[0] != "execute_cypher_query" {
		t.Fatalf("unfiltered suggestions = %v, want execute_cypher_query first", got)
	}
	if got := suggestionsFor(t, context.Background(), "find_divergent_cod"); slices.Contains(got, any("find_divergent_code")) {
		t.Fatalf("suggestions = %v, must not offer a profile-hidden tool", got)
	}
}

// TestPlaybookPlanDropsDisallowedDrilldowns: a resolved plan's drilldowns
// name tools too, so they pass through the same filter as its steps.
func TestPlaybookPlanDropsDisallowedDrilldowns(t *testing.T) {
	t.Parallel()

	def := playbook.Definition{
		ID: "pb", Name: "Repo check", Version: "1.0.0", PromptFamily: "repo.check",
		RequiredInputs: []playbook.Input{{Name: "repo_id", Type: playbook.InputIdentifier, Required: true}},
		Steps: []playbook.Step{{
			ID: "s1", Tool: "get_repo_summary", Params: []playbook.Param{playbook.InputParam("repo_id", "repo_id")},
			ExpectedTruth: querycontract.AnswerTruthDeterministic, EvidenceExpected: "summary",
			Drilldowns: []playbook.Drilldown{{Tool: "find_code", Reason: "ok"}, {Tool: "list_ingesters", Reason: "hidden"}},
		}},
		FailureModes: []playbook.FailureMode{{Condition: "c", Meaning: "m", Fallback: "f"}},
	}
	allowed := func(name string) bool { return name != "list_ingesters" }
	plan := planFor(def, Anchors{RepoID: "r1"}, allowed)
	if plan.Resolved == nil || len(plan.Resolved.Calls) != 1 {
		t.Fatalf("plan = %+v, want one resolved call", plan)
	}
	var tools []string
	for _, d := range plan.Resolved.Calls[0].Drilldowns {
		tools = append(tools, d.Tool)
	}
	if !slices.Equal(tools, []string{"find_code"}) {
		t.Fatalf("drilldowns = %v, want only find_code", tools)
	}
}

// TestCallRefusesBeforeEchoingAForbiddenToolsSchema: a caller who can never
// call a tool gets the refusal, not that tool's schema and example, even when
// its arguments are also wrong.
func TestCallRefusesBeforeEchoingAForbiddenToolsSchema(t *testing.T) {
	t.Parallel()

	out := newFixtureEngine(t, orderRanker{}).Call(scopedContext(), fixtureRequest(&recordingDispatcher{}),
		map[string]any{"name": "execute_cypher_query", "arguments": map[string]any{}})
	if code := errorCode(t, out.Result); code != "mcp_router_refused" {
		t.Fatalf("scoped call with bad arguments = %s, want mcp_router_refused before validation", code)
	}
	unscoped := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(&recordingDispatcher{}),
		map[string]any{"name": "execute_cypher_query", "arguments": map[string]any{}})
	if code := errorCode(t, unscoped.Result); code != "mcp_router_invalid_arguments" {
		t.Fatalf("unscoped call with bad arguments = %s, want mcp_router_invalid_arguments", code)
	}
}
