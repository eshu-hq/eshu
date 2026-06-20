package askwiring_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/askwiring"
)

const (
	askProofCredential          = "local-proof-key"
	askProofAuthorizationScheme = "Bearer"
)

func TestIsAskEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"empty", "", false},
		{"false", "false", false},
		{"true lowercase", "true", true},
		{"true uppercase", "TRUE", true},
		{"true mixed", "True", true},
		{"whitespace true", "  true  ", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := askwiring.IsAskEnabled(func(key string) string {
				if key == askwiring.EnvAskEnabled {
					return tc.envVal
				}
				return ""
			})
			if got != tc.want {
				t.Fatalf("IsAskEnabled(%q) = %v, want %v", tc.envVal, got, tc.want)
			}
		})
	}
}

func TestIsNarrationEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"empty", "", false},
		{"false", "false", false},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := askwiring.IsNarrationEnabled(func(key string) string {
				if key == askwiring.EnvAskNarrationEnabled {
					return tc.envVal
				}
				return ""
			})
			if got != tc.want {
				t.Fatalf("IsNarrationEnabled(%q) = %v, want %v", tc.envVal, got, tc.want)
			}
		})
	}
}

// TestBuildAskHandlerDefaultOff proves that BuildAskHandler returns a
// default-off handler (nil Asker) when ESHU_ASK_ENABLED is unset or false.
func TestBuildAskHandlerDefaultOff(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	if result.Handler == nil {
		t.Fatal("BuildAskHandler() Handler = nil, want non-nil")
	}
	if result.AdapterReady() {
		t.Fatal("BuildAskHandler() AdapterReady() = true, want false when disabled")
	}
	if result.SetPosture == nil {
		t.Fatal("BuildAskHandler() SetPosture = nil, want non-nil no-op func")
	}
}

// TestBuildAskHandlerDefaultOffNoProfileConfigured proves that even when
// ESHU_ASK_ENABLED=true but no agent_reasoning profile exists, the handler
// remains default-off.
func TestBuildAskHandlerDefaultOffNoProfileConfigured(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(key string) string {
			if key == askwiring.EnvAskEnabled {
				return "true"
			}
			return ""
		},
		http.NewServeMux(),
		"",
		nil,
	)

	if result.AdapterReady() {
		t.Fatal("BuildAskHandler() AdapterReady() = true, want false when no profile configured")
	}
}

// TestBuildAskHandlerDefaultOffSetPostureIsNoop proves SetPosture does not
// panic when called on a default-off result.
func TestBuildAskHandlerDefaultOffSetPostureIsNoop(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	// Must not panic.
	result.SetPosture(nil)
}

// TestBuildNarrationPostureDefaultClosed proves the posture function is
// default-closed when no env vars are set.
func TestBuildNarrationPostureDefaultClosed(t *testing.T) {
	t.Parallel()

	posture := askwiring.BuildNarrationPosture(func(string) string { return "" }, false)
	if posture == nil {
		t.Fatal("BuildNarrationPosture() = nil, want non-nil func")
	}

	got := posture()
	// When nothing is configured the posture must not be Available.
	if strings.EqualFold(got.State, "available") {
		t.Fatalf("BuildNarrationPosture() returned Available when nothing configured; want closed")
	}
}

// TestBuildAskHandlerRouteIsRegistered proves that the handler returned by
// BuildAskHandler can be mounted and replies to POST /api/v0/ask (503, not
// 404) when in default-off mode — mirroring the integration test in
// cmd/mcp-server/ask_wiring_test.go.
func TestBuildAskHandlerRouteIsRegistered(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	mux := http.NewServeMux()
	result.Handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		strings.NewReader(`{"question":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v0/ask returned 404; AskHandler route not mounted")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/v0/ask status = %d, want 503 (default-off)", rec.Code)
	}
}

// TestResolveAgentReasoningProfileEmptyEnv proves the function returns
// (zero, false) when no profile env var is set.
func TestResolveAgentReasoningProfileEmptyEnv(t *testing.T) {
	t.Parallel()

	_, ok := askwiring.ResolveAgentReasoningProfile(func(string) string { return "" }, nil)
	if ok {
		t.Fatal("ResolveAgentReasoningProfile() = _, true; want false when env is empty")
	}
}

func TestBuildAskHandlerProviderBackedJSONAndSSE(t *testing.T) {
	provider := newAskProofProvider(t)
	defer provider.Close()

	env := askProofEnv(provider.URL)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", askProofRepositoriesHandler(t))

	result := askwiring.BuildAskHandler(env, mux, "local-shared-key", nil)
	if !result.AdapterReady() {
		t.Fatal("BuildAskHandler() AdapterReady() = false, want true for local provider profile")
	}
	result.SetPosture(askwiring.BuildNarrationPosture(env, result.AdapterReady()))
	result.Handler.Mount(mux)

	jsonBody := postAskProof(t, mux, false)
	assertAskProofAnswer(t, jsonBody)

	sseBody := postAskProof(t, mux, true)
	if strings.Contains(sseBody, "raw provider final") {
		t.Fatalf("SSE leaked raw provider final text: %s", sseBody)
	}
	if !strings.Contains(sseBody, `"delta":"demo repo is indexed."`) {
		t.Fatalf("SSE body missing validated narration token: %s", sseBody)
	}
	answerJSON, ok := askProofSSEAnswer(sseBody)
	if !ok {
		t.Fatalf("SSE body missing answer event: %s", sseBody)
	}
	assertAskProofAnswer(t, answerJSON)

	if provider.nonStreamCalls() < 4 {
		t.Fatalf("provider non-stream calls = %d, want JSON main loop plus narration calls", provider.nonStreamCalls())
	}
	if provider.streamCalls() < 2 {
		t.Fatalf("provider stream calls = %d, want SSE main-loop stream calls", provider.streamCalls())
	}
}

type askProofProvider struct {
	*httptest.Server
	t *testing.T

	mu              sync.Mutex
	streamRequests  int
	regularRequests int
}

func newAskProofProvider(t *testing.T) *askProofProvider {
	t.Helper()
	p := &askProofProvider{t: t}
	p.Server = httptest.NewServer(http.HandlerFunc(p.handle))
	return p
}

func (p *askProofProvider) handle(w http.ResponseWriter, r *http.Request) {
	p.t.Helper()
	wantAuth := strings.Join([]string{askProofAuthorizationScheme, askProofCredential}, " ")
	if got := r.Header.Get("Authorization"); got != wantAuth {
		p.t.Errorf("provider Authorization = %q, want local authorization credential", got)
	}
	var req askProofProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.t.Errorf("decode provider request: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Stream {
		p.mu.Lock()
		p.streamRequests++
		p.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if req.hasToolResult() {
			_, _ = w.Write([]byte(askProofOpenAIStream([]string{"raw provider ", "final"}, "", "", "")))
			return
		}
		_, _ = w.Write([]byte(askProofOpenAIStream(nil, "call-repos", "list_indexed_repositories", `{"limit":1}`)))
		return
	}

	p.mu.Lock()
	p.regularRequests++
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case req.isNarration():
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"sentences\":[{\"text\":\"demo repo is indexed.\",\"kind\":\"factual\",\"provenance\":[{\"kind\":\"citation\",\"id\":\"repo:demo-service\"}]}]}","tool_calls":null},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	case req.hasToolResult():
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"raw provider final","tool_calls":null},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	default:
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call-repos","type":"function","function":{"name":"list_indexed_repositories","arguments":"{\"limit\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}
}

func (p *askProofProvider) streamCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streamRequests
}

func (p *askProofProvider) nonStreamCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.regularRequests
}

type askProofProviderRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

func (r askProofProviderRequest) hasToolResult() bool {
	for _, msg := range r.Messages {
		if msg.Role == "tool" {
			return true
		}
	}
	return false
}

func (r askProofProviderRequest) isNarration() bool {
	for _, msg := range r.Messages {
		if text, ok := msg.Content.(string); ok && strings.Contains(text, "ask-eshu-narration-v1") {
			return true
		}
	}
	return false
}

func askProofEnv(providerURL string) func(string) string {
	profiles := fmt.Sprintf(`[{
		"profile_id":"ask-local-proof",
		"provider_kind":"deepseek",
		"credential_source":{"kind":"environment_variable","handle":"ASK_PROOF_PROVIDER_KEY"},
		"model_id":"deepseek-chat",
		"endpoint_profile_id":%q,
		"source_classes":["agent_reasoning"],
		"source_policy_configured":true
	}]`, providerURL)
	values := map[string]string{
		askwiring.EnvAskEnabled:                "true",
		askwiring.EnvAskNarrationEnabled:       "true",
		"ESHU_SEMANTIC_PROVIDER_PROFILES_JSON": profiles,
		"ASK_PROOF_PROVIDER_KEY":               askProofCredential,
	}
	return func(key string) string {
		return values[key]
	}
}

func askProofRepositoriesHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/eshu.envelope+json" {
			t.Errorf("tool dispatch Accept = %q, want canonical envelope", got)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{
			"data": {
				"answer_packet": {
					"prompt_family": "ask-proof",
					"question": "which repos are indexed?",
					"primary_tool": "list_indexed_repositories",
					"summary": "demo repo is indexed",
					"truth_class": "deterministic",
					"supported": true,
					"truth": {
						"level": "exact",
						"capability": "repositories.list",
						"profile": "local_authoritative",
						"basis": "authoritative_graph",
						"backend": "nornicdb",
						"freshness": {"state": "fresh"}
					},
					"evidence_handles": [{"kind":"repository","repo_id":"repo-demo","reason":"proof"}],
					"citation_ref": "repo:demo-service"
				}
			},
			"truth": {
				"level": "exact",
				"capability": "repositories.list",
				"profile": "local_authoritative",
				"basis": "authoritative_graph",
				"backend": "nornicdb",
				"freshness": {"state": "fresh"}
			},
			"error": null
		}`))
	}
}

func postAskProof(t *testing.T, mux http.Handler, sse bool) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(`{"question":"which repos are indexed?"}`))
	req.Header.Set("Content-Type", "application/json")
	if sse {
		req.Header.Set("Accept", "text/event-stream")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v0/ask status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func assertAskProofAnswer(t *testing.T, body string) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode ask response: %v; body=%s", err, body)
	}
	if got := resp["answer_prose"]; got != "demo repo is indexed." {
		t.Fatalf("answer_prose = %#v, want governed narration", got)
	}
	if got := resp["truth_class"]; got != "deterministic" {
		t.Fatalf("truth_class = %#v, want deterministic", got)
	}
	handles, _ := resp["evidence_handles"].([]any)
	if len(handles) == 0 {
		t.Fatalf("evidence_handles missing: %#v", resp)
	}
	if got := resp["partial"]; got != false {
		t.Fatalf("partial = %#v, want false", got)
	}
}

func askProofSSEAnswer(body string) (string, bool) {
	var event string
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case event == "answer" && strings.HasPrefix(line, "data: "):
			return strings.TrimPrefix(line, "data: "), true
		}
	}
	return "", false
}

func askProofOpenAIStream(tokens []string, toolID, toolName, toolArgsJSON string) string {
	var b strings.Builder
	for _, token := range tokens {
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":null}],\"usage\":null}\n\n", strconv.Quote(token))
	}
	if toolID != "" && toolName != "" {
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":\"\"}}]},\"finish_reason\":null}],\"usage\":null}\n\n", toolID, toolName)
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":\"\",\"type\":\"function\",\"function\":{\"name\":\"\",\"arguments\":%s}}]},\"finish_reason\":null}],\"usage\":null}\n\n", strconv.Quote(toolArgsJSON))
	}
	b.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}
