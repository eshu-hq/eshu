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
		"ESHU_ASK_ENABLED":                      "true",
		"ESHU_ASK_NARRATION_ENABLED":            "true",
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

const askProofCodeRouteAsk = "/api/v0/ask"

// strconv quotes s as a JSON string.
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
		semanticprofile.EnvProviderProfilesJSON: "",
		askProofCredEnv:                         "",
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

func assertNoLeakJSON(t *testing.T, rec *httptest.ResponseRecorder, leak string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("leak ask status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), leak) {
		t.Fatalf("leak %q present in JSON answer body: %s", leak, rec.Body.String())
	}
}

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
