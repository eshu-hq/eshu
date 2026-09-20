// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifySendsExpectedRequestAndParsesResponse(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		bodyBytes, _ := json.Marshal(body)
		gotBody = string(bodyBytes)

		content := `{"findings":[` +
			`{"path":"go/internal/reducer/workloadinstance","name":"workloadinstance","verdict":"glued_compound","confidence":"high","evidence":"workload+instance glued","suggested_split":["workload","instance"]},` +
			`{"path":"go/internal/query/entity","name":"entity","verdict":"acceptable","confidence":"high","evidence":"single word"}` +
			`]}`
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &DeepSeekClient{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		APIKey:     "test-key-123",
		Model:      "deepseek-flash",
	}
	candidates := []Candidate{
		{Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance"},
		{Path: "go/internal/query/entity", Name: "entity"},
	}

	report, err := client.Classify(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if gotAuth != "Bearer test-key-123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-key-123")
	}
	if gotPath != "/chat/completions" {
		t.Errorf("request path = %q, want %q", gotPath, "/chat/completions")
	}
	for _, want := range []string{`"model":"deepseek-flash"`, `"temperature":0`, `"json_object"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("request body missing %s; got %s", want, gotBody)
		}
	}

	if len(report.Findings) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(report.Findings), report.Findings)
	}
	if report.Findings[0].Verdict != VerdictGluedCompound || report.Findings[0].Kind != "directory" {
		t.Errorf("finding[0] = %+v, want glued_compound/directory", report.Findings[0])
	}
	if report.Findings[1].Verdict != VerdictAcceptable {
		t.Errorf("finding[1] = %+v, want acceptable", report.Findings[1])
	}
}

func TestClassifyReturnsErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "bad", Model: "deepseek-flash"}
	_, err := client.Classify(context.Background(), []Candidate{{Path: "p", Name: "n"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil on HTTP 401")
	}
}

func TestClassifyReturnsErrorOnMalformedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "not json"}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}
	_, err := client.Classify(context.Background(), []Candidate{{Path: "p", Name: "n"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil when the model content is not the expected JSON shape")
	}
}

func TestClassifyReturnsErrorOnNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{}})
	}))
	defer server.Close()

	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}
	_, err := client.Classify(context.Background(), []Candidate{{Path: "p", Name: "n"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil on an empty choices array")
	}
}

// contentServer returns an httptest.Server whose chat-completions response
// carries exactly the given raw model content string, for the reconciliation
// tests below.
func contentServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestClassifyReturnsErrorOnFindingCountMismatch(t *testing.T) {
	server := contentServer(t, `{"findings":[{"path":"a","name":"a","verdict":"acceptable"}]}`)
	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}

	_, err := client.Classify(context.Background(), []Candidate{{Path: "a", Name: "a"}, {Path: "b", Name: "b"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil when the model returns fewer findings than candidates")
	}
}

func TestClassifyReturnsErrorOnUnknownPath(t *testing.T) {
	server := contentServer(t, `{"findings":[{"path":"never-asked-about","name":"x","verdict":"glued_compound"}]}`)
	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}

	_, err := client.Classify(context.Background(), []Candidate{{Path: "a", Name: "a"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil when a finding names a path that was never sent")
	}
}

func TestClassifyReturnsErrorOnDuplicatePath(t *testing.T) {
	// Two findings for "a" satisfy both the count check (2 findings, 2
	// candidates) and the membership check (both paths are known), but "b"
	// is never judged at all -- crowded out silently without this check.
	server := contentServer(t, `{"findings":[{"path":"a","name":"a","verdict":"acceptable"},{"path":"a","name":"a","verdict":"glued_compound"}]}`)
	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}

	_, err := client.Classify(context.Background(), []Candidate{{Path: "a", Name: "a"}, {Path: "b", Name: "b"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil when two findings name the same candidate, silently leaving another candidate unjudged")
	}
}

func TestClassifyReturnsErrorOnUnknownVerdict(t *testing.T) {
	server := contentServer(t, `{"findings":[{"path":"a","name":"a","verdict":"glued-compound"}]}`)
	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}

	_, err := client.Classify(context.Background(), []Candidate{{Path: "a", Name: "a"}})
	if err == nil {
		t.Fatal("Classify() error = nil, want non-nil on an off-spec verdict string (hyphen instead of underscore) that would silently fall through Violations()")
	}
}

func TestClassifyWithNoCandidatesDoesNotCallServer(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := &DeepSeekClient{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "k", Model: "deepseek-flash"}
	report, err := client.Classify(context.Background(), nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if called {
		t.Error("Classify() called the server with zero candidates, want it to short-circuit")
	}
	if len(report.Findings) != 0 {
		t.Errorf("report.Findings = %+v, want empty", report.Findings)
	}
}
