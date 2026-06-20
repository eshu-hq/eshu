package engine

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// minimalEnvelopeBytes is a minimal valid ResponseEnvelope JSON payload used by
// captureAuthHandler to satisfy the mcpRunner's envelope-parse path without
// pulling in a testing.T dependency.
var minimalEnvelopeBytes = []byte(`{"data":null,"truth":null,"error":null}`)

// captureAuthHandler records the Authorization header of each in-process
// request dispatched by the mcpRunner. It serves every path with a minimal
// valid ResponseEnvelope so Run can complete without error.
type captureAuthHandler struct {
	captured []string
}

func (h *captureAuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.captured = append(h.captured, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(minimalEnvelopeBytes)
}

// TestMCPRunnerContextAuthHeaderOverrideTakesPrecedence proves that when a
// context carries a caller auth header override (ContextWithCallerAuthHeader),
// the runner uses it in preference to the baked-in fallback. This is the
// per-request scope-preservation mechanism for scoped-token callers.
func TestMCPRunnerContextAuthHeaderOverrideTakesPrecedence(t *testing.T) {
	t.Parallel()

	const bakedIn = "Bearer baked-in-shared-key"
	const callerToken = "Bearer scoped-caller-token"

	capture := &captureAuthHandler{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewMCPRunner(capture, bakedIn, logger)

	ctx := ContextWithCallerAuthHeader(context.Background(), callerToken)
	_, err := runner.Run(ctx, "find_code", map[string]any{
		"query":   "main",
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(capture.captured) == 0 {
		t.Fatal("handler was not called")
	}
	got := capture.captured[0]
	if got != callerToken {
		t.Errorf("inner request Authorization = %q, want %q — caller scope was NOT preserved", got, callerToken)
	}
	if got == bakedIn {
		t.Errorf("inner request used baked-in shared key — scope was WIDENED to admin")
	}
}

// TestMCPRunnerFallsBackToBakedInWhenContextHasNoOverride proves that when no
// caller override is in the context, the baked-in authHeader is used unchanged.
// This preserves the existing shared-token behaviour.
func TestMCPRunnerFallsBackToBakedInWhenContextHasNoOverride(t *testing.T) {
	t.Parallel()

	const bakedIn = "Bearer shared-admin-key"

	capture := &captureAuthHandler{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewMCPRunner(capture, bakedIn, logger)

	_, err := runner.Run(context.Background(), "find_code", map[string]any{
		"query":   "main",
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(capture.captured) == 0 {
		t.Fatal("handler was not called")
	}
	if got := capture.captured[0]; got != bakedIn {
		t.Errorf("inner request Authorization = %q, want %q (baked-in fallback)", got, bakedIn)
	}
}

// TestMCPRunnerEmptyContextOverrideDoesNotReplaceAuthHeader proves that an
// empty string override in the context does NOT replace the baked-in header.
// An empty override is treated as "no override" so callers that do not set
// the value get the expected fallback.
func TestMCPRunnerEmptyContextOverrideDoesNotReplaceAuthHeader(t *testing.T) {
	t.Parallel()

	const bakedIn = "Bearer shared-admin-key"

	capture := &captureAuthHandler{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewMCPRunner(capture, bakedIn, logger)

	// Set an empty override — must not replace bakedIn.
	ctx := ContextWithCallerAuthHeader(context.Background(), "")
	_, err := runner.Run(ctx, "find_code", map[string]any{
		"query":   "main",
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(capture.captured) == 0 {
		t.Fatal("handler was not called")
	}
	if got := capture.captured[0]; got != bakedIn {
		t.Errorf("inner request Authorization = %q, want %q (empty override must not replace baked-in)", got, bakedIn)
	}
}

// TestContextWithCallerAuthHeaderRoundTrip verifies the context key stores and
// retrieves the value correctly.
func TestContextWithCallerAuthHeaderRoundTrip(t *testing.T) {
	t.Parallel()

	const token = "Bearer round-trip-token"
	ctx := ContextWithCallerAuthHeader(context.Background(), token)
	got, ok := ctx.Value(callerAuthHeaderKey{}).(string)
	if !ok {
		t.Fatal("value not found in context")
	}
	if got != token {
		t.Errorf("context value = %q, want %q", got, token)
	}
}
