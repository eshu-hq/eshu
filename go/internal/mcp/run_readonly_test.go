package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// minimalEnvelopeJSON returns a JSON-encoded ResponseEnvelope with the three
// required top-level keys ("data", "truth", "error") so that parseCanonicalEnvelope
// accepts it.
func minimalEnvelopeJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"data":  nil,
		"truth": nil,
		"error": nil,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return b
}

// findCodeHandler responds to POST /api/v0/code/search — the route that
// find_code resolves to — with a minimal valid ResponseEnvelope.
func findCodeHandler(t *testing.T) http.Handler {
	t.Helper()
	body := minimalEnvelopeJSON(t)
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

func TestRunReadOnlyTool(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("find_code returns envelope", func(t *testing.T) {
		t.Parallel()

		handler := findCodeHandler(t)
		envelope, value, isError, err := RunReadOnlyTool(
			context.Background(),
			handler,
			"find_code",
			map[string]any{"query": "main", "repo_id": "repo-1"},
			"",
			logger,
		)
		if err != nil {
			t.Fatalf("RunReadOnlyTool() error = %v, want nil", err)
		}
		if envelope == nil {
			t.Fatal("RunReadOnlyTool() envelope = nil, want non-nil")
		}
		if value != nil {
			t.Fatalf("RunReadOnlyTool() value = %v, want nil when envelope is set", value)
		}
		if isError {
			t.Fatal("RunReadOnlyTool() isError = true, want false")
		}
	})

	t.Run("plain JSON returns value with nil envelope", func(t *testing.T) {
		t.Parallel()

		// A handler that returns plain JSON (not a canonical envelope).
		plainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v0/status/collectors" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"collectors":["pagerduty","github"]}`))
		})
		envelope, value, isError, err := RunReadOnlyTool(
			context.Background(),
			plainHandler,
			"list_collectors",
			nil,
			"",
			logger,
		)
		if err != nil {
			t.Fatalf("RunReadOnlyTool() error = %v, want nil for plain JSON tool", err)
		}
		if envelope != nil {
			t.Fatalf("RunReadOnlyTool() envelope = %v, want nil for plain JSON tool", envelope)
		}
		if value == nil {
			t.Fatal("RunReadOnlyTool() value = nil, want non-nil for plain JSON tool")
		}
		if isError {
			t.Fatal("RunReadOnlyTool() isError = true, want false")
		}
	})

	t.Run("unknown tool returns error", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := RunReadOnlyTool(
			context.Background(),
			http.NewServeMux(),
			"definitely_not_a_tool",
			nil,
			"",
			logger,
		)
		if err == nil {
			t.Fatal("RunReadOnlyTool() error = nil, want non-nil for unknown tool")
		}
	})
}
