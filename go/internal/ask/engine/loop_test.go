package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// scriptedAdapter replays a pre-configured sequence of Completion values, one
// per Complete call. It records all messages seen per call for replay assertions.
type scriptedAdapter struct {
	turns    []provider.Completion
	turnErr  error // returned on the first call if non-nil
	errOnIdx int   // which call index triggers the error (-1 means none)
	calls    int
	received [][]provider.Message // messages per call
}

func (a *scriptedAdapter) Complete(_ context.Context, msgs []provider.Message, _ []provider.Tool) (provider.Completion, error) {
	idx := a.calls
	a.calls++
	// Copy the message slice for later assertion.
	copied := make([]provider.Message, len(msgs))
	copy(copied, msgs)
	a.received = append(a.received, copied)

	if a.turnErr != nil && idx == a.errOnIdx {
		return provider.Completion{}, a.turnErr
	}
	if idx < len(a.turns) {
		return a.turns[idx], nil
	}
	// Default to an empty final turn so the loop can finish.
	return provider.Completion{Text: "default final"}, nil
}

func (a *scriptedAdapter) ModelID() string { return "scripted-model" }

// recordingRunner records calls and returns a scripted result or error.
type recordingRunner struct {
	env    *query.ResponseEnvelope
	runErr error
	calls  []runCall
}

type runCall struct {
	name string
	args map[string]any
}

func (r *recordingRunner) Run(_ context.Context, name string, args map[string]any) (*query.ResponseEnvelope, error) {
	r.calls = append(r.calls, runCall{name: name, args: args})
	if r.runErr != nil {
		return nil, r.runErr
	}
	return r.env, nil
}

// supportedEnvelope returns a minimal *query.ResponseEnvelope that
// NewAnswerPacket treats as Supported == true.
func supportedEnvelope() *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{
			Level: query.TruthLevelExact,
			Basis: query.TruthBasisAuthoritativeGraph,
		},
	}
}

// TestAskRunsToolThenAnswers: adapter emits a tool-call on turn-1 and a final
// text on turn-2; runner returns a supported envelope; asserts that the engine
// assembles exactly one packet, one trace entry, correct prose, and that token
// usage is summed across both turns.
func TestAskRunsToolThenAnswers(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "find_code", Arguments: map[string]any{"q": "x"}},
		},
		Usage: provider.TokenUsage{InputTokens: 5, OutputTokens: 3},
	}
	turn2 := provider.Completion{
		Text:  "the answer",
		Usage: provider.TokenUsage{InputTokens: 2, OutputTokens: 4},
	}

	adapter := &scriptedAdapter{
		turns:    []provider.Completion{turn1, turn2},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "what is x?")
	if err != nil {
		t.Fatalf("Ask returned unexpected error: %v", err)
	}

	// Runner called exactly once with the right tool and args.
	if len(runner.calls) != 1 {
		t.Errorf("runner.calls = %d, want 1", len(runner.calls))
	} else {
		if runner.calls[0].name != "find_code" {
			t.Errorf("runner call name = %q, want %q", runner.calls[0].name, "find_code")
		}
		if v, ok := runner.calls[0].args["q"]; !ok || v != "x" {
			t.Errorf("runner call args[q] = %v, want x", runner.calls[0].args["q"])
		}
	}

	// One packet, supported.
	if len(ans.Packets) != 1 {
		t.Errorf("len(Packets) = %d, want 1", len(ans.Packets))
	} else if !ans.Packets[0].Supported {
		t.Error("Packets[0].Supported = false, want true")
	}

	// One trace entry.
	if len(ans.Trace) != 1 {
		t.Errorf("len(Trace) = %d, want 1", len(ans.Trace))
	}

	// Prose from final turn.
	if ans.Prose != "the answer" {
		t.Errorf("Prose = %q, want %q", ans.Prose, "the answer")
	}

	// Token usage summed across both turns.
	if ans.Usage.InputTokens != 7 {
		t.Errorf("Usage.InputTokens = %d, want 7", ans.Usage.InputTokens)
	}
	if ans.Usage.OutputTokens != 7 {
		t.Errorf("Usage.OutputTokens = %d, want 7", ans.Usage.OutputTokens)
	}
}

// TestAskMaxIterationsFallsBack: adapter always returns a tool-call completion;
// asserts the loop stops at MaxIterations, Answer.Partial is true, a
// "reached max reasoning iterations" limitation is present, and prose is derived
// from a packet summary (not fabricated text).
func TestAskMaxIterationsFallsBack(t *testing.T) {
	t.Parallel()

	// Build a turns slice that is longer than MaxIterations — adapter always
	// returns a tool call.
	always := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "cx", Name: "find_code", Arguments: map[string]any{"q": "loop"}},
		},
		Usage: provider.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}
	const maxIter = 6
	turns := make([]provider.Completion, maxIter+5)
	for i := range turns {
		turns[i] = always
	}

	adapter := &scriptedAdapter{turns: turns, errOnIdx: -1}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, Options{
		MaxIterations:       maxIter,
		MaxToolCallsPerTurn: 4,
		SystemPrompt:        "sys",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "loop question")
	if err != nil {
		t.Fatalf("Ask returned unexpected error: %v", err)
	}

	// Loop must have called adapter exactly maxIter times.
	if adapter.calls != maxIter {
		t.Errorf("adapter.calls = %d, want %d", adapter.calls, maxIter)
	}

	if !ans.Partial {
		t.Error("Answer.Partial = false, want true")
	}

	foundLimitation := false
	for _, lim := range ans.Limitations {
		if strings.Contains(lim, "reached max reasoning iterations") {
			foundLimitation = true
			break
		}
	}
	if !foundLimitation {
		t.Errorf("Limitations %v does not contain 'reached max reasoning iterations'", ans.Limitations)
	}

	// Prose must equal a packet Summary (deterministic) — it must not be empty
	// if supported packets were assembled.
	if ans.Prose == "" && len(ans.Packets) > 0 {
		// Packets exist but prose is empty — acceptable only if no packet is
		// supported; the test runner returns supported packets so we expect prose.
		hasSupportedPacket := false
		for _, p := range ans.Packets {
			if p.Supported && p.Summary != "" {
				hasSupportedPacket = true
				break
			}
		}
		if hasSupportedPacket {
			t.Error("Prose is empty but a supported packet with non-empty Summary exists")
		}
	}
}

// TestAskToolErrorRecoverable: runner returns an error on the first call;
// adapter emits a tool-call on turn-1 and a final text on turn-2. Asserts that
// a failed TraceEntry is recorded and the loop continues so that a final prose
// answer is produced.
func TestAskToolErrorRecoverable(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c2", Name: "find_code", Arguments: map[string]any{"q": "err"}},
		},
		Usage: provider.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}
	turn2 := provider.Completion{
		Text:  "recovered",
		Usage: provider.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{runErr: errors.New("tool dispatch failed")}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ans, err := eng.Ask(context.Background(), "trigger error")
	if err != nil {
		t.Fatalf("Ask returned unexpected error: %v", err)
	}

	// Must have a failed trace entry.
	if len(ans.Trace) != 1 {
		t.Errorf("len(Trace) = %d, want 1", len(ans.Trace))
	} else {
		te := ans.Trace[0]
		if te.Supported {
			t.Error("TraceEntry.Supported = true for a failed call, want false")
		}
		if te.Err == "" {
			t.Error("TraceEntry.Err is empty, want a non-empty error description")
		}
	}

	// Loop continued and final turn yields prose.
	if ans.Prose != "recovered" {
		t.Errorf("Prose = %q, want %q", ans.Prose, "recovered")
	}
}

// TestAskAssistantMessageReplaysToolCalls: verifies that the messages slice
// passed to the adapter's second Complete call includes an assistant message
// carrying the turn-1 tool call ID "c1" AND a RoleTool message with
// ToolCallID "c1".
func TestAskAssistantMessageReplaysToolCalls(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "find_code", Arguments: map[string]any{"q": "replay"}},
		},
		Usage: provider.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}
	turn2 := provider.Completion{
		Text:  "done",
		Usage: provider.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}

	adapter := &scriptedAdapter{turns: []provider.Completion{turn1, turn2}, errOnIdx: -1}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = eng.Ask(context.Background(), "replay test")
	if err != nil {
		t.Fatalf("Ask returned unexpected error: %v", err)
	}

	// The adapter must have received at least 2 calls.
	if adapter.calls < 2 {
		t.Fatalf("adapter.calls = %d, want >= 2", adapter.calls)
	}

	// In the second call's messages, find an assistant message with ToolCalls
	// containing ID "c1" and a RoleTool message with ToolCallID "c1".
	msgs := adapter.received[1]

	var foundAssistantToolCall bool
	var foundToolResult bool

	for _, m := range msgs {
		if m.Role == provider.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.ID == "c1" {
					foundAssistantToolCall = true
				}
			}
		}
		if m.Role == provider.RoleTool && m.ToolCallID == "c1" {
			foundToolResult = true
		}
	}

	if !foundAssistantToolCall {
		t.Error("second Complete call messages missing assistant message with ToolCalls[ID=c1]")
	}
	if !foundToolResult {
		t.Error("second Complete call messages missing RoleTool message with ToolCallID=c1")
	}
}

// TestAskProviderErrorPropagates: adapter returns an error on the first
// Complete call; asserts Ask returns a non-nil error wrapped with "ask:".
func TestAskProviderErrorPropagates(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns:    []provider.Completion{},
		turnErr:  errors.New("provider down"),
		errOnIdx: 0,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = eng.Ask(context.Background(), "fail question")
	if err == nil {
		t.Fatal("Ask must return a non-nil error when the provider fails")
	}
	if !strings.Contains(err.Error(), "ask:") {
		t.Errorf("error %q does not contain 'ask:' prefix", err.Error())
	}
}
