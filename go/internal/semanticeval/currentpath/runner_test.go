package currentpath

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticeval"
)

func TestRunnerRunsBoundedCodeSearchAndMapsEnvelopeResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/code/search"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		if got, want := body["query"], "Score"; got != want {
			t.Fatalf("query = %q, want %q", got, want)
		}
		if got, want := body["repo_id"], "repo-1"; got != want {
			t.Fatalf("repo_id = %q, want %q", got, want)
		}
		if got, want := body["limit"], float64(10); got != want {
			t.Fatalf("limit = %v, want %v", got, want)
		}

		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"results": []map[string]any{
					{"entity_id": "entity://score", "score": 0.9},
					{"repo_id": "repo-1", "file_path": "internal/score.go"},
				},
			},
			"truth": map[string]any{"level": "exact"},
		})
	}))
	defer server.Close()

	suite := Suite{Cases: []Case{
		{
			Case: semanticeval.Case{
				ID:       "score-path",
				Question: "where does semantic scoring happen?",
				Scope:    map[string]string{"repo_id": "repo-1"},
				Expected: []semanticeval.ExpectedHandle{
					{Handle: "entity://score", Relevance: 3, Required: true, MaxTruth: semanticeval.TruthClassExact},
				},
			},
			CurrentPath: Request{Mode: ModeCodeSearch, Query: "Score", Limit: 10},
		},
	}}

	run, err := Runner{BaseURL: server.URL, Timeout: time.Second}.Run(context.Background(), suite)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(run.Results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	result := run.Results[0]
	if result.LatencyMS <= 0 {
		t.Fatalf("LatencyMS = %.3f, want positive", result.LatencyMS)
	}
	if got, want := result.Candidates[0].Handle, "entity://score"; got != want {
		t.Fatalf("first handle = %q, want %q", got, want)
	}
	if got, want := result.Candidates[0].Truth, semanticeval.TruthClassExact; got != want {
		t.Fatalf("first truth = %q, want %q", got, want)
	}
	if got, want := result.Candidates[1].Handle, "file://repo-1/internal/score.go"; got != want {
		t.Fatalf("second handle = %q, want %q", got, want)
	}
}

func TestRunnerMapsTopicEvidenceGroupsAndFallbackTruth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/code/topics/investigate"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		if _, ok := body["query"]; ok {
			t.Fatalf("body includes query key for topic request: %#v", body)
		}
		if got, want := body["topic"], "topic evidence"; got != want {
			t.Fatalf("topic = %q, want %q", got, want)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"evidence_groups": []map[string]any{
					{"source_handle": "file://repo-1/internal/topic.go"},
				},
			},
			"truth": map[string]any{"level": "fallback"},
		})
	}))
	defer server.Close()

	suite := Suite{Cases: []Case{
		{
			Case: semanticeval.Case{
				ID:       "topic-path",
				Question: "how is topic evidence found?",
				Scope:    map[string]string{"repo_id": "repo-1"},
				Expected: []semanticeval.ExpectedHandle{
					{Handle: "file://repo-1/internal/topic.go", Relevance: 3, Required: true, MaxTruth: semanticeval.TruthClassDerived},
				},
			},
			CurrentPath: Request{Mode: ModeCodeTopic, Query: "topic evidence", Limit: 10},
		},
	}}

	run, err := Runner{BaseURL: server.URL, Timeout: time.Second}.Run(context.Background(), suite)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := run.Results[0].Candidates[0].Truth, semanticeval.TruthClassDerived; got != want {
		t.Fatalf("truth = %q, want %q", got, want)
	}
}

func TestRunnerRecordsUnsupportedCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotImplemented, map[string]any{
			"error": map[string]any{"code": "unsupported_capability", "message": "not supported"},
		})
	}))
	defer server.Close()

	suite := Suite{Cases: []Case{
		{
			Case: semanticeval.Case{
				ID:       "unsupported-path",
				Question: "can current path do this?",
				Expected: []semanticeval.ExpectedHandle{
					{Handle: "answer://future", Relevance: 3, Required: true, MaxTruth: semanticeval.TruthClassSemanticCandidate},
				},
			},
			CurrentPath: Request{Mode: ModeCodeSearch, Query: "future", Limit: 10},
		},
	}}

	run, err := Runner{BaseURL: server.URL, Timeout: time.Second}.Run(context.Background(), suite)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	candidate := run.Results[0].Candidates[0]
	if got, want := candidate.Handle, "unsupported://unsupported-path"; got != want {
		t.Fatalf("handle = %q, want %q", got, want)
	}
	if got, want := candidate.Truth, semanticeval.TruthClassUnsupported; got != want {
		t.Fatalf("truth = %q, want %q", got, want)
	}
}

func TestRunnerRejectsUnexpectedSuccessPayloadShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data":  map[string]any{"items": []map[string]any{{"handle": "file://repo-1/a.go"}}},
			"truth": map[string]any{"level": "derived"},
		})
	}))
	defer server.Close()

	suite := Suite{Cases: []Case{
		{
			Case: semanticeval.Case{
				ID:       "unexpected-shape",
				Question: "what changed?",
				Expected: []semanticeval.ExpectedHandle{
					{Handle: "file://repo-1/a.go", Relevance: 3, Required: true, MaxTruth: semanticeval.TruthClassDerived},
				},
			},
			CurrentPath: Request{Mode: ModeCodeSearch, Query: "changed", Limit: 10},
		},
	}}

	_, err := Runner{BaseURL: server.URL, Timeout: time.Second}.Run(context.Background(), suite)
	if err == nil {
		t.Fatal("Run() error = nil, want unexpected payload shape error")
	}
	if !strings.Contains(err.Error(), "recognized result key") {
		t.Fatalf("Run() error = %q, want recognized result key", err.Error())
	}
}

func TestLoadSuiteJSONRejectsUnknownFieldsAndUnboundedLimits(t *testing.T) {
	_, err := LoadSuiteJSON(strings.NewReader(`{"cases":[{"id":"unknown","question":"q","expected":[{"handle":"h","relevance":1,"required":true,"max_truth":"exact"}],"current_path":{"mode":"code_search","query":"q","extra":true}}]}`))
	if err == nil {
		t.Fatal("LoadSuiteJSON() error = nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadSuiteJSON() error = %q, want unknown field", err.Error())
	}

	_, err = LoadSuiteJSON(strings.NewReader(`{"cases":[{"id":"wide","question":"q","expected":[{"handle":"h","relevance":1,"required":true,"max_truth":"exact"}],"current_path":{"mode":"code_search","query":"q","limit":51}}]}`))
	if err == nil {
		t.Fatal("LoadSuiteJSON() error = nil, want limit error")
	}
	if !strings.Contains(err.Error(), "between 0 and 50") {
		t.Fatalf("LoadSuiteJSON() error = %q, want between 0 and 50", err.Error())
	}
}

func TestLoadSuiteJSONReportsMalformedTrailingBytes(t *testing.T) {
	input := `{"cases":[{"id":"malformed","question":"q","expected":[{"handle":"h","relevance":1,"required":true,"max_truth":"exact"}],"current_path":{"mode":"code_search","query":"q"}}]} {`
	_, err := LoadSuiteJSON(strings.NewReader(input))
	if err == nil {
		t.Fatal("LoadSuiteJSON() error = nil, want malformed trailing bytes error")
	}
	if strings.Contains(err.Error(), "trailing values") {
		t.Fatalf("LoadSuiteJSON() error = %q, want parse error", err.Error())
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode response: %v", err)
	}
}
