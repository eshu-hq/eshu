// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"slices"
	"testing"
)

func TestFindWithoutAuthContextIsUnfilteredAndFlagged(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	result := engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "anything", "limit": 10})
	if result.IsError {
		t.Fatalf("Find() isError, structured = %v", structured(t, result))
	}
	names := candidateNames(t, result)
	if !slices.Contains(names, "execute_cypher_query") {
		t.Fatalf("unfiltered candidates = %v, want execute_cypher_query offered", names)
	}
	access, _ := structured(t, result)["access"].(map[string]any)
	if access["filtered"] != false || access["reason"] != "no_auth_context" {
		t.Fatalf("access = %v, want filtered=false reason=no_auth_context", access)
	}
}

func TestFindScopedTokenNeverOffersSharedKeyOnlyTools(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	result := engine.Find(scopedContext(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "run cypher", "limit": 10})
	names := candidateNames(t, result)
	if slices.Contains(names, "execute_cypher_query") {
		t.Fatalf("scoped candidates = %v, must not offer execute_cypher_query", names)
	}
	if !slices.Contains(names, "find_code") || !slices.Contains(names, "get_repo_summary") {
		t.Fatalf("scoped candidates = %v, want allowlisted tools kept", names)
	}
	access, _ := structured(t, result)["access"].(map[string]any)
	if access["filtered"] != true {
		t.Fatalf("access = %v, want filtered=true", access)
	}
}

func TestFindSharedTokenOffersEveryVisibleTool(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	names := candidateNames(t, engine.Find(sharedContext(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "x", "limit": 10}))
	if !slices.Contains(names, "execute_cypher_query") {
		t.Fatalf("shared candidates = %v, want execute_cypher_query", names)
	}
}

func TestFindNeverOffersRouterToolsAskOrHiddenTools(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	req := fixtureRequest(&recordingDispatcher{})
	req.Visible = append(req.Visible, Tools()...)
	names := candidateNames(t, engine.Find(context.Background(), req, map[string]any{"intent": "x", "limit": 10}))
	for _, banned := range []string{FindToolName, CallToolName, "ask", "find_divergent_code"} {
		if slices.Contains(names, banned) {
			t.Fatalf("candidates = %v, must not offer %s", names, banned)
		}
	}
}

func TestFindAppliesDefaultLimitAndCandidateFields(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	body := structured(t, engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "x"}))
	raw, _ := body["candidates"].([]any)
	if len(raw) == 0 || len(raw) > DefaultLimit {
		t.Fatalf("candidates = %d, want 1..%d", len(raw), DefaultLimit)
	}
	first := raw[0].(map[string]any)
	for _, key := range []string{"name", "why", "cost", "inputSchema", "example", "score"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("candidate %v missing %q", first, key)
		}
	}
	example, _ := first["example"].(map[string]any)
	if example["query"] == nil {
		t.Fatalf("find_code example = %v, want required query filled", example)
	}
}

func TestFindRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	for name, args := range map[string]map[string]any{
		"missing intent": {"limit": 3},
		"limit too high": {"intent": "x", "limit": MaxLimit + 1},
		"limit zero":     {"intent": "x", "limit": 0},
		"blank intent":   {"intent": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), args)
			if !result.IsError || errorCode(t, result) != "mcp_router_invalid_arguments" {
				t.Fatalf("Find(%v) = %+v, want mcp_router_invalid_arguments", args, structured(t, result))
			}
		})
	}
}

func TestFindWithoutRankerReportsUnavailable(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, nil)
	result := engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "x"})
	if !result.IsError || errorCode(t, result) != "mcp_router_ranker_unavailable" {
		t.Fatalf("Find() without ranker = %+v, want mcp_router_ranker_unavailable", structured(t, result))
	}
}

func TestFindAnchorsReachTheRanker(t *testing.T) {
	t.Parallel()

	spy := &spyRanker{}
	engine := newFixtureEngine(t, spy)
	engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{
		"intent": "x", "limit": 3,
		"anchors": map[string]any{"repo_id": "r1", "service_id": "s1", "workload_id": "w1", "environment": "prod"},
	})
	want := Query{Intent: "x", Limit: 3, Anchors: Anchors{RepoID: "r1", ServiceID: "s1", WorkloadID: "w1", Environment: "prod"}}
	if spy.got != want {
		t.Fatalf("ranker query = %+v, want %+v", spy.got, want)
	}
}

func TestFindDropsCandidatesTheFilterRejects(t *testing.T) {
	t.Parallel()

	// A misbehaving ranker that ignores allowed must still not leak a tool.
	engine := newFixtureEngine(t, leakyRanker{})
	names := candidateNames(t, engine.Find(scopedContext(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "x"}))
	if slices.Contains(names, "execute_cypher_query") {
		t.Fatalf("candidates = %v, filter must drop execute_cypher_query even when a ranker returns it", names)
	}
}

func TestFindAttachesMatchingPlaybookPlan(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	body := structured(t, engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{
		"intent": "tell me the service story with citation evidence for checkout",
	}))
	plan, ok := body["playbook"].(map[string]any)
	if !ok {
		t.Fatalf("find response = %v, want a playbook plan", body)
	}
	if plan["id"] != "service_story_citation" {
		t.Fatalf("playbook id = %v, want service_story_citation", plan["id"])
	}
	steps, _ := plan["steps"].([]any)
	if len(steps) == 0 {
		t.Fatalf("playbook plan = %v, want steps", plan)
	}
}

func TestFindOmitsPlaybookForUnrelatedIntent(t *testing.T) {
	t.Parallel()

	engine := newFixtureEngine(t, orderRanker{})
	body := structured(t, engine.Find(context.Background(), fixtureRequest(&recordingDispatcher{}), map[string]any{"intent": "zzz qqq"}))
	if _, ok := body["playbook"]; ok {
		t.Fatalf("find response = %v, want no playbook", body)
	}
}
