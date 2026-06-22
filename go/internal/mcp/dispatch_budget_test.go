package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// bigRowsHandler writes a canonical success envelope whose data block carries
// rowCount synthetic rows, each padded to rowBytes, so the serialized response
// can be driven over a chosen byte budget deterministically.
func bigRowsHandler(t *testing.T, rowCount, rowBytes int) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]any, 0, rowCount)
		for i := 0; i < rowCount; i++ {
			rows = append(rows, map[string]any{
				"id":   i,
				"blob": strings.Repeat("x", rowBytes),
			})
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"source":  "content",
			"results": rows,
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.fuzzy_symbol",
			query.TruthBasisContentIndex,
			"resolved from content index fallback",
		))
	})
}

func dispatchWithBudget(t *testing.T, handler http.Handler, budget int) (*dispatchResult, error) {
	t.Helper()
	return dispatchToolWithOptions(
		context.Background(),
		handler,
		"find_code",
		map[string]any{"query": "Handle", "limit": 5},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dispatchOptions{responseByteBudget: budget},
	)
}

// singleEnvelopeMarshalBytes returns the size of a single JSON marshal of the
// handler's envelope, dispatched with the budget disabled so the payload is the
// untrimmed wire envelope. It lets a test assert that one copy fits under a
// budget, isolating the duplicate-copy accounting from raw payload size.
func singleEnvelopeMarshalBytes(t *testing.T, handler http.Handler) int {
	t.Helper()
	result, err := dispatchWithBudget(t, handler, 0)
	if err != nil {
		t.Fatalf("dispatchWithBudget(budget=0) error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("expected a canonical envelope to size")
	}
	encoded, err := json.Marshal(result.Envelope)
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v, want nil", err)
	}
	return len(encoded)
}

func TestDispatchToolResponseOverBudgetReturnsBoundedEnvelope(t *testing.T) {
	t.Parallel()

	// 200 rows * ~256 bytes each easily exceeds a 4 KiB budget.
	handler := bigRowsHandler(t, 200, 256)
	result, err := dispatchWithBudget(t, handler, 4*1024)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("over-budget result must still be a canonical envelope, got nil")
	}
	if !result.IsError {
		t.Fatal("over-budget result IsError = false, want true")
	}
	if result.Envelope.Error == nil {
		t.Fatal("over-budget envelope must carry an error block")
	}
	if got, want := result.Envelope.Error.Code, errorCodeResponseOverBudget; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	details := result.Envelope.Error.Details
	if details == nil {
		t.Fatal("over-budget error must carry details with budget accounting")
	}
	if _, ok := details["response_bytes"]; !ok {
		t.Fatalf("details missing response_bytes: %#v", details)
	}
	if _, ok := details["budget_bytes"]; !ok {
		t.Fatalf("details missing budget_bytes: %#v", details)
	}
	guidance, ok := details["guidance"].(string)
	if !ok || strings.TrimSpace(guidance) == "" {
		t.Fatalf("details must teach the agent how to narrow, got %#v", details["guidance"])
	}
	// The bounded over-budget envelope must itself be small.
	if size := estimateResponseBytes(result); size > 4*1024 {
		t.Fatalf("over-budget replacement envelope is %d bytes, must stay within budget", size)
	}
}

func TestDispatchToolResponseWithinBudgetPassesThrough(t *testing.T) {
	t.Parallel()

	handler := bigRowsHandler(t, 2, 16)
	result, err := dispatchWithBudget(t, handler, 1<<20)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("within-budget result envelope is nil")
	}
	if result.IsError {
		t.Fatalf("within-budget result IsError = true, want false; envelope = %#v", result.Envelope)
	}
	if result.Envelope.Error != nil {
		t.Fatalf("within-budget envelope must have no error block, got %#v", result.Envelope.Error)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "code_search.fuzzy_symbol" {
		t.Fatalf("within-budget truth = %#v, want code search truth preserved", result.Envelope.Truth)
	}
}

func TestDispatchToolZeroBudgetDisablesEnforcement(t *testing.T) {
	t.Parallel()

	handler := bigRowsHandler(t, 200, 256)
	result, err := dispatchWithBudget(t, handler, 0)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("disabled-budget result envelope is nil")
	}
	if result.IsError {
		t.Fatal("disabled-budget result must pass through unchanged (IsError=false)")
	}
}

func TestDispatchToolResponseDoubledWireOverBudgetTrips(t *testing.T) {
	t.Parallel()

	// handleMessage serializes an envelope-backed result onto the wire twice:
	// once as the embedded resource.Text block and again as StructuredContent.
	// A response whose single envelope marshal sits in the 130-256 KiB band
	// passes a naive single-marshal guard yet ships ~2x that on the wire. The
	// budget must account for the duplicate copy and refuse it.
	const budget = defaultToolResponseByteBudget // 256 KiB

	// 300 rows * 512 bytes marshals to ~156 KiB once: comfortably under the
	// 256 KiB budget and inside the 130-256 KiB danger band, so a naive
	// single-marshal guard would wave it through. The doubled wire payload
	// (resource.Text + structuredContent) lands near ~320 KiB and must be
	// refused.
	handler := bigRowsHandler(t, 300, 512)

	result, err := dispatchWithBudget(t, handler, budget)
	if err != nil {
		t.Fatalf("dispatchToolWithOptions() error = %v, want nil", err)
	}
	// Guard the test's own premise: one marshal of this payload must sit under
	// the budget, so the guard can only trip by counting the duplicate copy.
	if single := singleEnvelopeMarshalBytes(t, handler); single >= budget {
		t.Fatalf("test payload single marshal = %d bytes, must stay under %d to prove the duplicate-copy guard", single, budget)
	}
	if result.Envelope == nil || result.Envelope.Error == nil {
		t.Fatalf("doubled wire payload over budget must trip the guard, got %#v", result)
	}
	if got, want := result.Envelope.Error.Code, errorCodeResponseOverBudget; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if !result.IsError {
		t.Fatal("over-budget result IsError = false, want true")
	}
}

func TestDefaultDispatchAppliesResponseBudget(t *testing.T) {
	t.Parallel()

	// dispatchTool (no explicit options) must apply the default byte budget so
	// every MCP tool response is hub-throttled, not just option-driven tests.
	// Build a response large enough to exceed the default budget.
	rowBytes := 512
	rowCount := (defaultToolResponseByteBudget / rowBytes) + 64
	handler := bigRowsHandler(t, rowCount, rowBytes)
	result, err := dispatchTool(
		context.Background(),
		handler,
		"find_code",
		map[string]any{"query": "Handle", "limit": 5},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil || result.Envelope.Error == nil {
		t.Fatalf("default dispatch must enforce the response budget, got %#v", result)
	}
	if got, want := result.Envelope.Error.Code, errorCodeResponseOverBudget; got != want {
		t.Fatalf("default dispatch error code = %q, want %q", got, want)
	}
}
