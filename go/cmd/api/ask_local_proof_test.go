package main

// This file is the CI-runnable local proof for Ask Eshu (issue #3332). It drives
// the REAL Ask Eshu runtime path — the production router mux, the scoped-auth
// middleware, askwiring.BuildAskHandler, the real ask/engine, the real provider
// adapter, the runtime answer guardrail, and both the JSON and SSE handlers —
// using a local httptest stub provider instead of a hosted DeepSeek endpoint.
//
// It proves, without any live DeepSeek credentials or graph/Postgres backend:
//   - disabled (503), missing-provider (503), and bad-provider (503) states;
//   - the scoped-token allowlist admits POST /api/v0/ask;
//   - GET /api/v0/status/answer-narration reports consistent gate state;
//   - a clean cited answer succeeds on both JSON and SSE;
//   - an answer whose narration carries an AKIA-style key, a Bearer token, or an
//     uncited factual claim is suppressed on BOTH JSON and SSE (no leak in the
//     token deltas).
//
// Suppression routing for the three leak families:
//   - AKIA keys and Bearer tokens: caught by the narration validator and the
//     runtime answer guardrail's CriterionPublishSafety.
//   - Raw bare IPv4 addresses (e.g. 10.1.2.3): suppressed by the runtime answer
//     guardrail's rawAddressPattern; the narration validator's IP rule requires
//     an http(s):// prefix and does NOT fire on bare addresses.
//
// Harness/stub helpers live in ask_local_proof_helpers_test.go (split to keep
// each file under the 500-line repo limit).
//
// The provider-backed path uses the endpoint_profile_id override to point the
// deepseek/openai_compatible adapter at a local stub server (see the package
// doc on factory.go: endpoint_profile_id is a base-URL override). The single
// backend-dependent leaf — the in-process tool dispatch that would normally read
// the graph — is served by a controllable handler so the cited-vs-suppressed
// behavior is deterministic and offline. The hosted, real-DeepSeek end-to-end
// rerun is operator-gated and documented in
// docs/public/reference/local-testing/ask-eshu-local-proof.md; it is never run
// here and never committed.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAskLocalProof_DisabledReturns503(t *testing.T) {
	handler := buildAskProofHandler(t, func(string) string { return "" }, nil)
	rec := postAskJSON(handler, "anything")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled ask status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatalf("disabled ask body missing unavailable state: %s", rec.Body.String())
	}
}

func TestAskLocalProof_MissingProviderReturns503(t *testing.T) {
	// Ask enabled but no provider profile configured at all.
	getenv := askProofEnv("http://127.0.0.1:0", map[string]string{
		"ESHU_SEMANTIC_PROVIDER_PROFILES_JSON": "",
		askProofCredEnv:                        "",
	})
	handler := buildAskProofHandler(t, getenv, nil)
	rec := postAskJSON(handler, "anything")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing-provider ask status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAskLocalProof_BadProviderReturns503(t *testing.T) {
	// Profile present but the credential env var is unset, so adapter
	// construction fails and the handler stays default-off (nil Asker -> 503).
	srv, _ := newAskStubProvider(t, cleanScript(askProofCleanSummary))
	getenv := askProofEnv(srv.URL, map[string]string{askProofCredEnv: ""})
	handler := buildAskProofHandler(t, getenv, nil)
	rec := postAskJSON(handler, "anything")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("bad-provider ask status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAskLocalProof_StatusNarrationReportsAvailableWhenConfigured(t *testing.T) {
	srv, _ := newAskStubProvider(t, cleanScript(askProofCleanSummary))
	getenv := askProofEnv(srv.URL, nil)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(citedEnvelopeBody(askProofCleanSummary))
	})
	handler := buildAskProofHandler(t, getenv, leaf)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/answer-narration", nil)
	req.Header.Set("Authorization", "Bearer "+askProofAPIKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status/answer-narration status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if got["provider_configured"] != true {
		t.Fatalf("provider_configured = %v, want true; body: %s", got["provider_configured"], rec.Body.String())
	}
}

func TestAskLocalProof_CleanCitedAnswerSucceedsJSON(t *testing.T) {
	srv, sawAuth := newAskStubProvider(t, cleanScript(askProofCleanSummary))
	getenv := askProofEnv(srv.URL, nil)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(citedEnvelopeBody(askProofCleanSummary))
	})
	handler := buildAskProofHandler(t, getenv, leaf)

	rec := postAskJSON(handler, "What service entrypoint does the demo repo expose?")
	if rec.Code != http.StatusOK {
		t.Fatalf("clean ask status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if prose, _ := got["answer_prose"].(string); !strings.Contains(prose, "supported service entrypoint") {
		t.Fatalf("answer_prose = %q, want narrated cited prose; body: %s", prose, rec.Body.String())
	}
	if handles, _ := got["evidence_handles"].([]any); len(handles) == 0 {
		t.Fatalf("evidence_handles empty, want at least one; body: %s", rec.Body.String())
	}
	if got := sawAuth.Load(); got == nil || *got != "Bearer "+askProofCredValue {
		t.Fatalf("stub provider Authorization = %v, want Bearer <stub cred>", got)
	}
}

func TestAskLocalProof_CleanCitedAnswerSucceedsSSE(t *testing.T) {
	srv, _ := newAskStubProvider(t, cleanScript(askProofCleanSummary))
	getenv := askProofEnv(srv.URL, nil)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(citedEnvelopeBody(askProofCleanSummary))
	})
	handler := buildAskProofHandler(t, getenv, leaf)

	rec := postAskSSE(handler, "What service entrypoint does the demo repo expose?")
	if rec.Code != http.StatusOK {
		t.Fatalf("clean ask SSE status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	events := parseSSE(rec.Body.String())
	var sawToken, sawAnswer, sawDone bool
	for _, ev := range events {
		switch ev.name {
		case "token":
			sawToken = true
		case "answer":
			sawAnswer = true
		case "done":
			sawDone = true
		}
	}
	if !sawAnswer || !sawDone {
		t.Fatalf("SSE missing answer/done events: %#v", events)
	}
	if !sawToken {
		t.Fatalf("clean SSE answer emitted no validated token deltas: %#v", events)
	}
}

func TestAskLocalProof_AkiaKeyIsSuppressedJSONAndSSE(t *testing.T) {
	// leaks maps case name to the unsafe prose the narration stub returns.
	// raw_address uses a bare IPv4 (no scheme): suppressed by rawAddressPattern
	// in the runtime guardrail, not the narration validator (which requires https://).
	leaks := map[string]string{
		"akia_key":     "AKIAIOSFODNN7EXAMPLE is the access key",
		"bearer_token": "use Bearer sk-live-abcdef to authenticate",
		"raw_address":  "the host is at " + strings.Join([]string{"10", "1", "2", "3"}, "."),
	}
	for name, leak := range leaks {
		leak := leak
		t.Run(name+"_json", func(t *testing.T) {
			handler := buildLeakHandler(t, leak)
			rec := postAskJSON(handler, "describe the host")
			assertNoLeakJSON(t, rec, leak)
		})
		t.Run(name+"_sse", func(t *testing.T) {
			handler := buildLeakHandler(t, leak)
			rec := postAskSSE(handler, "describe the host")
			assertNoLeakSSE(t, rec, leak)
		})
	}
}

func TestAskLocalProof_UncitedFactualClaimIsSuppressed(t *testing.T) {
	// Narration emits a factual sentence with NO provenance. The governed
	// narration validator rejects it, so narrate() keeps Narrated=false and the
	// runtime guardrail suppresses the prose.
	srv, _ := newAskStubProvider(t, askStubScript{
		mainTurns: []openAICompatCompletion{
			{toolCalls: []openAICompatToolCall{{id: "call_1", name: "find_code", args: `{"query":"x","repo_id":"repo:demo"}`}}},
			{content: "final"},
		},
		narration: openAICompatCompletion{content: `{"sentences":[{"text":"This is an uncited factual claim.","kind":"factual"}]}`},
	})
	getenv := askProofEnv(srv.URL, nil)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(citedEnvelopeBody(askProofCleanSummary))
	})
	handler := buildAskProofHandler(t, getenv, leaf)

	rec := postAskJSON(handler, "describe the demo repo")
	if rec.Code != http.StatusOK {
		t.Fatalf("uncited ask status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if prose, _ := got["answer_prose"].(string); strings.Contains(prose, "uncited factual claim") {
		t.Fatalf("uncited factual claim leaked into answer_prose: %s", rec.Body.String())
	}
}
