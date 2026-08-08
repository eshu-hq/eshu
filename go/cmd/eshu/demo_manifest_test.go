// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// repoManifestPath locates the committed demo-first-answers manifest from the
// package directory, so these tests read the real acceptance oracle rather
// than a fixture that could drift from it.
func repoManifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "specs", "demo-first-answers.v1.yaml")
}

func TestLoadDemoManifest_ReadsTheCommittedOracle(t *testing.T) {
	t.Parallel()
	m, err := loadDemoManifest(repoManifestPath(t))
	if err != nil {
		t.Fatalf("loadDemoManifest: %v", err)
	}
	if len(m.Questions) < 5 {
		t.Fatalf("questions = %d, want at least the five demo questions", len(m.Questions))
	}
	q := m.Questions[0]
	if q.ID == "" || strings.TrimSpace(q.Question) == "" {
		t.Errorf("first question is missing id or text: %+v", q)
	}
	// The manifest exists so the demo executes a declared surface instead of
	// inventing a query route. If this ever parses empty, the demo would have
	// nothing real to call.
	if q.Execute.Kind == "" || q.Execute.Ref == "" {
		t.Fatalf("first question declares no execute surface: %+v", q.Execute)
	}
	if len(q.ExpectedAnswer.RequiredResponseFields) == 0 {
		t.Errorf("first question declares no required_response_fields; the answer could not be checked")
	}
}

func TestLoadDemoManifest_FirstQuestionIsTheMCPServiceStory(t *testing.T) {
	t.Parallel()
	m, err := loadDemoManifest(repoManifestPath(t))
	if err != nil {
		t.Fatalf("loadDemoManifest: %v", err)
	}
	q := m.Questions[0]
	if q.Execute.Kind != demoExecuteMCP {
		t.Errorf("first question execute kind = %q, want %q", q.Execute.Kind, demoExecuteMCP)
	}
	if q.Execute.Ref != "get_service_story" {
		t.Errorf("first question execute ref = %q, want get_service_story", q.Execute.Ref)
	}
	if got := q.Execute.Arguments["workload_id"]; got != "workload:api-svc" {
		t.Errorf("workload_id = %v, want workload:api-svc", got)
	}
}

func TestAskDemoQuestion_MCPPostsToolsCallAndReturnsTheAnswer(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{
			"answer_metadata":{"truth":{"level":"exact"}},
			"answer_packet":{"summary":"api-svc runs in workload:api-svc"},
			"api_surface":{"routes":[]}}}`))
	}))
	defer srv.Close()

	q := demoQuestion{
		ID:       "q1",
		Question: "which workload?",
		Execute: demoExecute{
			Kind:      demoExecuteMCP,
			Ref:       "get_service_story",
			Arguments: map[string]any{"workload_id": "workload:api-svc"},
		},
	}
	q.ExpectedAnswer.RequiredResponseFields = []string{"answer_metadata", "answer_packet", "api_surface"}

	ans, err := executeDemoQuestion(context.Background(), srv.URL, srv.URL, "k", q)
	if err != nil {
		t.Fatalf("executeDemoQuestion: %v", err)
	}
	if !strings.Contains(gotPath, "mcp") {
		t.Errorf("posted to %q, want the MCP endpoint", gotPath)
	}
	if gotBody["method"] != "tools/call" {
		t.Errorf("method = %v, want tools/call", gotBody["method"])
	}
	params, _ := gotBody["params"].(map[string]any)
	if params["name"] != "get_service_story" {
		t.Errorf("params.name = %v, want get_service_story", params["name"])
	}
	args, _ := params["arguments"].(map[string]any)
	if args["workload_id"] != "workload:api-svc" {
		t.Errorf("arguments.workload_id = %v, want workload:api-svc", args["workload_id"])
	}
	if ans.Answer == "" {
		t.Error("answer is empty")
	}
	if len(ans.Truth) == 0 {
		t.Error("answer carries no truth labels")
	}
}

func TestAskDemoQuestion_MissingRequiredFieldIsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// answer_packet is absent: the tool replied, but not with the shape
		// the manifest says this question's answer must have.
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"answer_metadata":{},"api_surface":{}}}`))
	}))
	defer srv.Close()

	q := demoQuestion{ID: "q1", Question: "q", Execute: demoExecute{Kind: demoExecuteMCP, Ref: "get_service_story"}}
	q.ExpectedAnswer.RequiredResponseFields = []string{"answer_metadata", "answer_packet", "api_surface"}

	_, err := executeDemoQuestion(context.Background(), srv.URL, srv.URL, "k", q)
	if err == nil {
		t.Fatal("error = nil; a reply missing a required field must not pass as an answer")
	}
	if !strings.Contains(err.Error(), "answer_packet") {
		t.Errorf("error %q does not name the missing field", err)
	}
}

func TestAskDemoQuestion_HTTPExecutesTheDeclaredRoute(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"incident":{"id":"PSCD1"},"services":[{"name":"api-svc"}]}`))
	}))
	defer srv.Close()

	q := demoQuestion{ID: "q3", Question: "which services?", Execute: demoExecute{
		Kind: demoExecuteHTTP, Ref: "GET /api/v0/incidents/PSCD1/context",
	}}
	q.ExpectedAnswer.RequiredResponseFields = []string{"incident", "services"}

	if _, err := executeDemoQuestion(context.Background(), srv.URL, srv.URL, "k", q); err != nil {
		t.Fatalf("executeDemoQuestion: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v0/incidents/PSCD1/context" {
		t.Errorf("path = %q, want the manifest's declared route", gotPath)
	}
}

func TestAskDemoQuestion_UnknownExecuteKindFailsLoudly(t *testing.T) {
	t.Parallel()
	q := demoQuestion{ID: "qX", Execute: demoExecute{Kind: "carrier-pigeon", Ref: "x"}}
	_, err := executeDemoQuestion(context.Background(), "http://127.0.0.1:1", "http://127.0.0.1:1", "k", q)
	if err == nil {
		t.Fatal("error = nil; an unrecognized execute kind must fail rather than be skipped")
	}
}

func TestDemoUp_MintsAnEphemeralKeyAndPassesItToCompose(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)

	if _, err := rt.up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	// The demo runtime overlay refuses to start mcp-server with no resolvable
	// credential source (#5168), so "zero credentials" has to mean the command
	// mints one rather than that the stack runs without one.
	if rt.apiKey == "" {
		t.Fatal("no ephemeral demo key was minted; mcp-server will refuse to start")
	}
	if len(rt.apiKey) < 24 {
		t.Errorf("ephemeral key is %d chars; too short to be a credential", len(rt.apiKey))
	}
	var sawKeyEnv bool
	for _, e := range exec.envs {
		for _, kv := range e {
			if strings.HasPrefix(kv, "ESHU_DEMO_API_KEY=") && strings.TrimPrefix(kv, "ESHU_DEMO_API_KEY=") == rt.apiKey {
				sawKeyEnv = true
			}
		}
	}
	if !sawKeyEnv {
		t.Errorf("ESHU_DEMO_API_KEY was never passed to compose; envs=%v", exec.envs)
	}
}

func TestDemoEphemeralKeys_AreNotReusedAcrossRuns(t *testing.T) {
	t.Parallel()
	a, err := newEphemeralDemoKey()
	if err != nil {
		t.Fatalf("newEphemeralDemoKey: %v", err)
	}
	b, err := newEphemeralDemoKey()
	if err != nil {
		t.Fatalf("newEphemeralDemoKey: %v", err)
	}
	if a == b {
		t.Error("two demo runs minted the same key; it must be per-run, not a constant")
	}
}

func TestDemoStatus_RecoversTheKeyFromTheRunningStack(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		// A running project, and the stack hands back its own key.
		"ps --quiet":            {out: []byte("abc123\n")},
		"printenv ESHU_API_KEY": {out: []byte("demo-recovered-key\n")},
	}}
	rt := newTestDemoRuntime(exec)
	rt.apiKey = "" // status runs in a fresh process; up's ephemeral key is gone.

	var probedWith string
	rt.probe = func(_ context.Context, _, key string) (demoIndexStatus, error) {
		probedWith = key
		return demoIndexStatus{Status: "healthy", RepositoryCount: 6}, nil
	}

	res, err := rt.status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// Without recovery the probe goes out unauthenticated, gets 401, and a
	// healthy stack is reported as not ready — indistinguishable from a real
	// failure.
	if probedWith != "demo-recovered-key" {
		t.Errorf("probed with key %q, want the key recovered from the running stack", probedWith)
	}
	if !res.Ready {
		t.Error("healthy stack with an empty queue reported as not ready")
	}
}
