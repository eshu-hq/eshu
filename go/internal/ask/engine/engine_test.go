package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// stubAdapter is a minimal provider.Adapter for testing.
type stubAdapter struct{}

func (s *stubAdapter) Complete(_ context.Context, _ []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	return provider.Completion{}, nil
}

func (s *stubAdapter) ModelID() string { return "stub-model" }

// stubRunner is a minimal Runner for testing.
type stubRunner struct{}

func (s *stubRunner) Run(_ context.Context, _ string, _ map[string]any) (*query.ResponseEnvelope, error) {
	return &query.ResponseEnvelope{}, nil
}

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()

	if opts.MaxIterations != 6 {
		t.Errorf("DefaultOptions().MaxIterations = %d, want 6", opts.MaxIterations)
	}
	if opts.MaxToolCallsPerTurn != 4 {
		t.Errorf("DefaultOptions().MaxToolCallsPerTurn = %d, want 4", opts.MaxToolCallsPerTurn)
	}
	if opts.SystemPrompt == "" {
		t.Error("DefaultOptions().SystemPrompt must not be empty")
	}
}

func TestNew_NilAdapter(t *testing.T) {
	t.Parallel()

	_, err := New(nil, &stubRunner{}, nil, DefaultOptions())
	if err == nil {
		t.Error("New with nil adapter must return an error")
	}
}

func TestNew_NilRunner(t *testing.T) {
	t.Parallel()

	_, err := New(&stubAdapter{}, nil, nil, DefaultOptions())
	if err == nil {
		t.Error("New with nil runner must return an error")
	}
}

func TestNew_ValidInputs_ReturnsEngine(t *testing.T) {
	t.Parallel()

	eng, err := New(&stubAdapter{}, &stubRunner{}, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New with valid adapter and runner returned error: %v", err)
	}
	if eng == nil {
		t.Fatal("New with valid inputs must return a non-nil Engine")
	}
}

func TestNew_ZeroOptions_AppliesDefaults(t *testing.T) {
	t.Parallel()

	eng, err := New(&stubAdapter{}, &stubRunner{}, nil, Options{})
	if err != nil {
		t.Fatalf("New with zero Options returned error: %v", err)
	}

	got := eng.opts
	want := DefaultOptions()

	if got.MaxIterations != want.MaxIterations {
		t.Errorf("opts.MaxIterations = %d after zero Options, want %d", got.MaxIterations, want.MaxIterations)
	}
	if got.MaxToolCallsPerTurn != want.MaxToolCallsPerTurn {
		t.Errorf("opts.MaxToolCallsPerTurn = %d after zero Options, want %d", got.MaxToolCallsPerTurn, want.MaxToolCallsPerTurn)
	}
	if got.SystemPrompt == "" {
		t.Error("opts.SystemPrompt must not be empty after zero Options defaulting")
	}
}

func TestNew_PartialOptions_OnlyZeroFieldsDefaulted(t *testing.T) {
	t.Parallel()

	customPrompt := "my-custom-system-prompt"
	eng, err := New(&stubAdapter{}, &stubRunner{}, nil, Options{
		SystemPrompt: customPrompt,
	})
	if err != nil {
		t.Fatalf("New with partial Options returned error: %v", err)
	}

	if eng.opts.MaxIterations != 6 {
		t.Errorf("opts.MaxIterations = %d, want 6", eng.opts.MaxIterations)
	}
	if eng.opts.MaxToolCallsPerTurn != 4 {
		t.Errorf("opts.MaxToolCallsPerTurn = %d, want 4", eng.opts.MaxToolCallsPerTurn)
	}
	if eng.opts.SystemPrompt != customPrompt {
		t.Errorf("opts.SystemPrompt = %q, want %q", eng.opts.SystemPrompt, customPrompt)
	}
}

func TestNew_BothNil_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := New(nil, nil, nil, DefaultOptions())
	if !errors.Is(err, ErrNilAdapter) {
		t.Error("New with both nil adapter and runner must return an error")
	}
}
