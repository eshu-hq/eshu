package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

func TestPrintVersionFlagReturnsBeforeRunningEval(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-semantic-eval"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout bytes.Buffer
	handled, err := printVersionFlag([]string{"--version"}, &stdout)
	if err != nil {
		t.Fatalf("printVersionFlag() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("printVersionFlag() handled = false, want true")
	}
	if got, want := stdout.String(), "eshu-semantic-eval-currentpath v1.2.3-semantic-eval\n"; got != want {
		t.Fatalf("printVersionFlag() output = %q, want %q", got, want)
	}
}

func TestRunWritesCurrentPathRunAndReport(t *testing.T) {
	t.Parallel()

	handlerErrors := make(chan error, 1)
	observedRepoIDs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/content/files/search"; got != want {
			recordHandlerError(handlerErrors, fmt.Errorf("path = %q, want %q", got, want))
			http.Error(w, "unexpected path", http.StatusInternalServerError)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			recordHandlerError(handlerErrors, fmt.Errorf("decode request body: %w", err))
			http.Error(w, "invalid request body", http.StatusInternalServerError)
			return
		}
		observedRepoID, _ := body["repo_id"].(string)
		observedRepoIDs <- observedRepoID
		if err := writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"results": []map[string]any{
					{"handle": "file://repository:r_test/go/internal/semanticeval/currentpath/testdata/eshu_phase0_suite.json"},
					{"repo_id": observedRepoID, "relative_path": "go/internal/semanticeval/README.md"},
				},
			},
			"truth": map[string]any{"level": "derived"},
		}); err != nil {
			recordHandlerError(handlerErrors, fmt.Errorf("encode response: %w", err))
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	suitePath := filepath.Join(dir, "suite.json")
	runPath := filepath.Join(dir, "run.json")
	reportPath := filepath.Join(dir, "report.json")
	suite := `{
  "cases": [
    {
      "id": "semantic-eval-readme",
      "question": "Where is the semantic eval contract documented?",
      "scope": {"repo_id": "{repo_id}"},
      "expected": [
        {"handle": "file://{repo_id}/go/internal/semanticeval/README.md", "relevance": 3, "required": true, "max_truth": "derived"}
      ],
      "current_path": {
        "mode": "content_file_search",
        "query": "Semantic Eval",
        "limit": 10,
        "exclude_handles": ["file://{repo_id}/go/internal/semanticeval/currentpath/testdata/eshu_phase0_suite.json"]
      }
    }
  ]
}`
	if err := os.WriteFile(suitePath, []byte(suite), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := run(context.Background(), []string{
		"--base-url", server.URL,
		"--suite", suitePath,
		"--repo-id", "repository:r_test",
		"--run-output", runPath,
		"--report-output", reportPath,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	select {
	case handlerErr := <-handlerErrors:
		t.Fatalf("handler error: %v", handlerErr)
	default:
	}
	var observedRepoID string
	select {
	case observedRepoID = <-observedRepoIDs:
	default:
		t.Fatal("handler did not observe repo_id")
	}
	if got, want := observedRepoID, "repository:r_test"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}

	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(reportData), `"recall_at_k": 1`) {
		t.Fatalf("report = %s, want recall_at_k 1", string(reportData))
	}
	runData, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if !strings.Contains(string(runData), "file://repository:r_test/go/internal/semanticeval/README.md") {
		t.Fatalf("run = %s, want substituted file handle", string(runData))
	}
	if strings.Contains(string(runData), "eshu_phase0_suite.json") {
		t.Fatalf("run = %s, want eval suite artifact filtered", string(runData))
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout = %q, want empty when report-output is set", stdout.String())
	}
}

func TestRunPrintsReportToStdoutWhenNoReportOutputIsSet(t *testing.T) {
	t.Parallel()

	handlerErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"results": []map[string]any{
					{"repo_id": "repository:r_test", "relative_path": "docs/docs/architecture.md"},
				},
			},
			"truth": map[string]any{"level": "derived"},
		}); err != nil {
			recordHandlerError(handlerErrors, fmt.Errorf("encode response: %w", err))
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	suitePath := filepath.Join(dir, "suite.json")
	suite := `{
  "cases": [
    {
      "id": "architecture",
      "question": "Where is the architecture overview?",
      "scope": {"repo_id": "repository:r_test"},
      "expected": [
        {"handle": "file://repository:r_test/docs/docs/architecture.md", "relevance": 3, "required": true, "max_truth": "derived"}
      ],
      "current_path": {"mode": "content_file_search", "query": "System Architecture", "limit": 10}
    }
  ]
}`
	if err := os.WriteFile(suitePath, []byte(suite), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := run(context.Background(), []string{
		"--base-url", server.URL,
		"--suite", suitePath,
	}, stdout, stderr)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	select {
	case handlerErr := <-handlerErrors:
		t.Fatalf("handler error: %v", handlerErr)
	default:
	}
	if !strings.Contains(stdout.String(), `"case_count": 1`) {
		t.Fatalf("stdout = %q, want JSON report", stdout.String())
	}
}

func TestRunRejectsRepoIDPlaceholderWithoutRepoID(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	suitePath := filepath.Join(dir, "suite.json")
	suite := `{
  "cases": [
    {
      "id": "placeholder",
      "question": "Where is the architecture overview?",
      "scope": {"repo_id": "{repo_id}"},
      "expected": [
        {"handle": "file://{repo_id}/docs/docs/architecture.md", "relevance": 3, "required": true, "max_truth": "derived"}
      ],
      "current_path": {"mode": "content_file_search", "query": "System Architecture", "limit": 10}
    }
  ]
}`
	if err := os.WriteFile(suitePath, []byte(suite), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}

	err := run(context.Background(), []string{
		"--base-url", server.URL,
		"--suite", suitePath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want repo-id placeholder requirement")
	}
	if !strings.Contains(err.Error(), "--repo-id is required") {
		t.Fatalf("run() error = %q, want repo-id placeholder requirement", err.Error())
	}
	if called.Load() {
		t.Fatal("server was called before placeholder validation")
	}
}

func TestRunRequiresSuitePath(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"--base-url", "http://127.0.0.1:8080"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() error = nil, want suite requirement")
	}
	if !strings.Contains(err.Error(), "suite path is required") {
		t.Fatalf("run() error = %q, want suite path", err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}

func recordHandlerError(handlerErrors chan<- error, err error) {
	select {
	case handlerErrors <- err:
	default:
	}
}
