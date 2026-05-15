package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	var observedRepoID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/content/files/search"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		observedRepoID, _ = body["repo_id"].(string)
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"results": []map[string]any{
					{"repo_id": observedRepoID, "relative_path": "go/internal/semanticeval/README.md"},
				},
			},
			"truth": map[string]any{"level": "derived"},
		})
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
      "current_path": {"mode": "content_file_search", "query": "Semantic Eval", "limit": 10}
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
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout = %q, want empty when report-output is set", stdout.String())
	}
}

func TestRunPrintsReportToStdoutWhenNoReportOutputIsSet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"results": []map[string]any{
					{"repo_id": "repository:r_test", "relative_path": "docs/docs/architecture.md"},
				},
			},
			"truth": map[string]any{"level": "derived"},
		})
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
	if !strings.Contains(stdout.String(), `"case_count": 1`) {
		t.Fatalf("stdout = %q, want JSON report", stdout.String())
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

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
