package main

// ask_local_proof_helpers_test.go contains all stub, harness, and assertion
// helpers for the Ask Eshu local proof (ask_local_proof_test.go). The split
// keeps each file under the repo 500-line limit.
//
// Suppression routing note: the three leak families exercise different
// suppression layers:
//   - AKIA-style keys (AKIA...): caught by answerguardrail.CriterionPublishSafety
//     and by the narration validator (publish_safety checks the narration prose).
//   - Bearer tokens (Bearer sk-live-...): same dual suppression path.
//   - Raw bare IPv4 addresses (e.g. 10.1.2.3 without http(s)://): suppressed by
//     the runtime answer guardrail's rawAddressPattern, NOT by the narration
//     validator whose IP rule requires an http(s):// URL prefix. The proof
//     verifies suppression at the guardrail layer for this family.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

const (
	askProofAPIKey       = "ask-proof-shared-token"
	askProofCredEnv      = "ASK_PROOF_STUB_KEY"
	askProofCredValue    = "stub-key-not-a-real-secret"
	askProofCitationRef  = "citation:redacted-demo"
	askProofCodeRoute    = "/api/v0/code/search"
	askProofCodeRouteAsk = "/api/v0/ask"
	askProofCleanSummary = "The demo repository exposes one supported service entrypoint."
)

// askStubScript holds the canned completions a stub provider returns, in order.
// Each Complete call pops the next script entry. The main-loop turns drive the
// engine; the final narration turn (no tools) drives narrate(). The narration
// call is detected by the narration system sentinel in the messages.
type askStubScript struct {
	mainTurns []openAICompatCompletion
	narration openAICompatCompletion
}

// openAICompatCompletion is the minimal stub completion body. It is encoded as
// the OpenAI-compatible /v1/chat/completions response the adapter expects.
type openAICompatCompletion struct {
	content   string
	toolCalls []openAICompatToolCall
}

type openAICompatToolCall struct {
	id   string
	name string
	args string
}

// newAskStubProvider returns an httptest server emulating an OpenAI-compatible
// /v1/chat/completions endpoint. It serves mainTurns in order for tool-loop
// completions and serves the narration completion when the request carries the
// narration system sentinel. It records the Authorization header it observed so
// the test can assert the credential was threaded from the configured profile.
func newAskStubProvider(t *testing.T, script askStubScript) (*httptest.Server, *atomic.Pointer[string]) {
	t.Helper()
	var turn atomic.Int32
	var sawAuth atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		sawAuth.Store(&auth)
		body, _ := io.ReadAll(r.Body)
		raw := string(body)
		isNarration := strings.Contains(raw, "ask-eshu-narration-v1")
		var comp openAICompatCompletion
		if isNarration {
			// Narration always uses the non-streaming Complete path.
			comp = script.narration
		} else {
			idx := int(turn.Add(1)) - 1
			if idx < len(script.mainTurns) {
				comp = script.mainTurns[idx]
			} else {
				comp = script.mainTurns[len(script.mainTurns)-1]
			}
		}
		// The main loop may request a streaming response (stream:true); narration
		// never does. Serve provider-side SSE frames for streaming requests so the
		// real openai-compat adapter's CompleteStream parser is exercised.
		if !isNarration && strings.Contains(raw, `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(encodeStubSSE(comp))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encodeStubCompletion(comp))
	}))
	t.Cleanup(srv.Close)
	return srv, &sawAuth
}

// encodeStubCompletion renders the OpenAI-compatible response body for comp.
func encodeStubCompletion(comp openAICompatCompletion) []byte {
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type tc struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	calls := make([]tc, 0, len(comp.toolCalls))
	for _, c := range comp.toolCalls {
		calls = append(calls, tc{ID: c.id, Type: "function", Function: fn{Name: c.name, Arguments: c.args}})
	}
	finish := "stop"
	var toolCalls any
	if len(calls) > 0 {
		finish = "tool_calls"
		toolCalls = calls
	}
	payload := map[string]any{
		"choices": []map[string]any{{
			"message":       map[string]any{"content": comp.content, "tool_calls": toolCalls},
			"finish_reason": finish,
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
	b, _ := json.Marshal(payload)
	return b
}

// encodeStubSSE renders comp as OpenAI-compatible provider SSE frames:
// one data chunk carrying the content and/or tool calls, then data: [DONE].
// This is what the openai-compat adapter's CompleteStream parser expects.
func encodeStubSSE(comp openAICompatCompletion) []byte {
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type tc struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	calls := make([]tc, 0, len(comp.toolCalls))
	for i, c := range comp.toolCalls {
		calls = append(calls, tc{Index: i, ID: c.id, Type: "function", Function: fn{Name: c.name, Arguments: c.args}})
	}
	finish := "stop"
	delta := map[string]any{}
	if comp.content != "" {
		delta["content"] = comp.content
	}
	if len(calls) > 0 {
		finish = "tool_calls"
		delta["tool_calls"] = calls
	}
	chunk := map[string]any{
		"choices": []map[string]any{{"delta": delta, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
	cb, _ := json.Marshal(chunk)
	var b strings.Builder
	b.WriteString("data: ")
	b.Write(cb)
	b.WriteString("\n\ndata: [DONE]\n\n")
	return []byte(b.String())
}

// stubProviderProfilesJSON builds an ESHU_SEMANTIC_PROVIDER_PROFILES_JSON value
// for a deepseek/openai-compatible agent_reasoning profile whose
// endpoint_profile_id points at baseURL (the local stub). The credential is
// sourced from an environment variable so no secret is embedded.
func stubProviderProfilesJSON(baseURL string) string {
	return fmt.Sprintf(`{"profiles":[{"profile_id":"local-ask-proof","display_name":"Local Ask Proof Stub","provider_kind":%q,"credential_source":{"kind":"environment_variable","handle":%q},"model_id":"deepseek-chat","endpoint_profile_id":%q,"source_classes":[%q]}]}`,
		semanticprofile.ProviderDeepSeek, askProofCredEnv, baseURL, semanticprofile.SourceAgentReasoning)
}

// citedEnvelopeBody returns a canonical ResponseEnvelope JSON whose Data embeds
// a supported answer_packet carrying the given summary and one redacted evidence
// handle plus a citation_ref. This is the controllable backend leaf that the
// engine's in-process tool dispatch resolves to.
func citedEnvelopeBody(summary string) []byte {
	packet := map[string]any{
		"prompt_family": "code_topic",
		"primary_tool":  "find_code",
		"truth_class":   "code_hint",
		"summary":       summary,
		"supported":     true,
		"citation_ref":  askProofCitationRef,
		"evidence_handles": []map[string]any{{
			"kind":            "code",
			"repo_id":         "repo:demo",
			"relative_path":   "src/service.go",
			"evidence_family": "code_symbol",
		}},
	}
	envelope := map[string]any{
		"data":  map[string]any{"answer_packet": packet},
		"truth": map[string]any{"level": "code_hint", "freshness": map[string]any{"state": "fresh"}},
		"error": nil,
	}
	b, _ := json.Marshal(envelope)
	return b
}

// askProofEnv returns a getenv closure that enables ask + narration, configures
// the stub provider profile, and sets the stub credential. Individual fields can
// be overridden per case via the overrides map (an empty value deletes the key).
func askProofEnv(stubURL string, overrides map[string]string) func(string) string {
	base := map[string]string{
		"ESHU_ASK_ENABLED":                     "true",
		"ESHU_ASK_NARRATION_ENABLED":           "true",
		semanticprofile.EnvProviderProfilesJSON: stubProviderProfilesJSON(stubURL),
		askProofCredEnv:                         askProofCredValue,
	}
	for k, v := range overrides {
		if v == "" {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	return func(key string) string { return base[key] }
}

// buildAskProofHandler assembles the production-shaped API handler with the real
// router (nil backends, like the existing newRouter tests), the real scoped-auth
// middleware, and the real ask wiring. innerLeaf, when non-nil, intercepts the
// in-process code-search route so the engine's tool dispatch returns a
// controlled envelope; all other routes flow through the real authed mux.
func buildAskProofHandler(t *testing.T, getenv func(string) string, innerLeaf http.Handler) http.Handler {
	t.Helper()
	router, err := newRouter(
		nil, nil, nil, staticStatusReader{}, nil,
		query.ProfileLocalFullStack, query.GraphBackendNornicDB,
		nil, nil, "", "", component.Policy{}, query.GovernanceStatusConfig{}, nil,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}
	apiMux := http.NewServeMux()
	router.Mount(apiMux)

	authedMux := query.AuthMiddlewareWithScopedTokens(askProofAPIKey, nil, apiMux)

	// inProcessHandler backs the engine's in-process MCP runner. It is the
	// scoped-auth-wrapped handler with an optional controllable leaf override.
	inProcess := http.Handler(authedMux)
	if innerLeaf != nil {
		inProcess = leafOverrideHandler{leafPath: askProofCodeRoute, leaf: innerLeaf, rest: authedMux}
	}

	mountAskAndNarration(getenv, apiMux, inProcess, askProofAPIKey, router.Status, slog.Default())
	return authedMux
}

// leafOverrideHandler routes leafPath to leaf and everything else to rest. It
// lets the proof control the single backend-dependent tool-dispatch leaf while
// keeping auth, scoped routing, and status fully real.
type leafOverrideHandler struct {
	leafPath string
	leaf     http.Handler
	rest     http.Handler
}

func (h leafOverrideHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == h.leafPath {
		h.leaf.ServeHTTP(w, r)
		return
	}
	h.rest.ServeHTTP(w, r)
}

// postAskJSON drives POST /api/v0/ask in JSON mode through handler.
func postAskJSON(handler http.Handler, question string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, askProofCodeRouteAsk, strings.NewReader(`{"question":`+jsonQuote(question)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+askProofAPIKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// postAskSSE drives POST /api/v0/ask in SSE mode through handler.
func postAskSSE(handler http.Handler, question string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, askProofCodeRouteAsk, strings.NewReader(`{"question":`+jsonQuote(question)+`}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+askProofAPIKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// jsonQuote quotes s as a JSON string.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// sseEvent is one parsed SSE event.
type sseEvent struct {
	name string
	data string
}

// parseSSE parses event/data SSE frames from body.
func parseSSE(body string) []sseEvent {
	var events []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.name != "" {
				events = append(events, cur)
			}
			cur = sseEvent{}
		}
	}
	return events
}

// cleanScript drives one tool call then a final prose turn; narration emits a
// single cited sentence referencing the packet citation_ref.
func cleanScript(prose string) askStubScript {
	return askStubScript{
		mainTurns: []openAICompatCompletion{
			{toolCalls: []openAICompatToolCall{{id: "call_1", name: "find_code", args: `{"query":"service entrypoint","repo_id":"repo:demo"}`}}},
			{content: "final"},
		},
		narration: openAICompatCompletion{content: fmt.Sprintf(
			`{"sentences":[{"text":%s,"kind":"factual","provenance":[{"kind":"citation","id":%q}]}]}`,
			jsonQuote(prose), askProofCitationRef)},
	}
}

// buildLeakHandler constructs a proof handler whose narration stub returns leak
// as its prose, exercising the suppression path.
func buildLeakHandler(t *testing.T, leak string) http.Handler {
	t.Helper()
	srv, _ := newAskStubProvider(t, cleanScript(leak))
	getenv := askProofEnv(srv.URL, nil)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(citedEnvelopeBody(askProofCleanSummary))
	})
	return buildAskProofHandler(t, getenv, leaf)
}

// assertNoLeakJSON asserts leak is absent from the JSON answer body.
func assertNoLeakJSON(t *testing.T, rec *httptest.ResponseRecorder, leak string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("leak ask status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), leak) {
		t.Fatalf("leak %q present in JSON answer body: %s", leak, rec.Body.String())
	}
}

// assertNoLeakSSE asserts leak is absent from all SSE token deltas and the full
// SSE body.
func assertNoLeakSSE(t *testing.T, rec *httptest.ResponseRecorder, leak string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("leak ask SSE status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	for _, ev := range parseSSE(rec.Body.String()) {
		if ev.name == "token" && strings.Contains(ev.data, leak) {
			t.Fatalf("leak %q present in SSE token delta: %s", leak, ev.data)
		}
	}
	if strings.Contains(rec.Body.String(), leak) {
		t.Fatalf("leak %q present anywhere in SSE body: %s", leak, rec.Body.String())
	}
}
