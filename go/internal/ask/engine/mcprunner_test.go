package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// minimalRunnerEnvelopeJSON returns a JSON-encoded ResponseEnvelope with the
// three required top-level keys so that parseCanonicalEnvelope accepts it.
func minimalRunnerEnvelopeJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"data":  nil,
		"truth": nil,
		"error": nil,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal runner envelope: %v", err)
	}
	return b
}

// findCodeRunnerHandler responds to POST /api/v0/code/search — the route that
// find_code resolves to — with a minimal valid ResponseEnvelope.
func findCodeRunnerHandler(t *testing.T) http.Handler {
	t.Helper()
	body := minimalRunnerEnvelopeJSON(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v0/code/search" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}

func TestMCPRunnerRunsReadOnlyTool(t *testing.T) {
	t.Parallel()

	handler := findCodeRunnerHandler(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewMCPRunner(handler, "", logger)

	envelope, err := runner.Run(context.Background(), "find_code", map[string]any{
		"query":   "main",
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if envelope == nil {
		t.Fatal("Run() envelope = nil, want non-nil")
	}
}

func TestMCPRunnerNilLoggerUsesDiscard(t *testing.T) {
	t.Parallel()

	handler := findCodeRunnerHandler(t)
	// A nil logger must not panic; NewMCPRunner should replace it with a discard logger.
	runner := NewMCPRunner(handler, "", nil)

	envelope, err := runner.Run(context.Background(), "find_code", map[string]any{
		"query":   "main",
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("Run() with nil logger error = %v, want nil", err)
	}
	if envelope == nil {
		t.Fatal("Run() with nil logger envelope = nil, want non-nil")
	}
}

func TestMCPRunnerUnknownToolErrors(t *testing.T) {
	t.Parallel()

	runner := NewMCPRunner(http.NewServeMux(), "", nil)

	_, err := runner.Run(context.Background(), "definitely_not_a_tool", nil)
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil for unknown tool")
	}
}
