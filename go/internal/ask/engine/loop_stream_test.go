package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// TestAskStream_DropsRawDeltasWhenNarrationRejected verifies that AskStream
// never forwards provider token deltas before narration validation succeeds.
func TestAskStream_DropsRawDeltasWhenNarrationRejected(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "find_code", Arguments: map[string]any{"q": "x"}},
		},
		Usage: provider.TokenUsage{InputTokens: 5, OutputTokens: 2},
	}
	turn2 := provider.Completion{
		Text:  "here is the answer",
		Usage: provider.TokenUsage{InputTokens: 3, OutputTokens: 4},
	}

	adapter := &scriptedStreamingAdapter{
		turns: []provider.Completion{turn1, turn2},
		// turn 1 has no deltas (it's a tool-call turn); turn 2 emits 2 deltas.
		tokenDeltas: [][]string{
			nil,
			{"here is ", "the answer"},
		},
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Narration is available, but the scripted narration completion is not valid
	// JSON. Raw provider deltas still must not be emitted.
	eng.SetNarrationPosture(func() status.AnswerNarrationStatus {
		return status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	})

	var tokenEvents []StreamEvent
	var traceEvents []StreamEvent
	ans, err := eng.AskStream(context.Background(), "what is x?", func(ev StreamEvent) {
		switch ev.Kind {
		case KindToken:
			tokenEvents = append(tokenEvents, ev)
		case KindTraceEntry:
			traceEvents = append(traceEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	if len(tokenEvents) != 0 {
		t.Errorf("token events = %d, want 0 when narration is rejected", len(tokenEvents))
	}
	if ans.Prose != "here is the answer" {
		t.Errorf("Prose = %q, want %q", ans.Prose, "here is the answer")
	}
	if ans.Narrated {
		t.Error("Narrated = true, want false when narration is rejected")
	}

	// One trace entry from the tool call.
	if len(traceEvents) != 1 {
		t.Errorf("trace events = %d, want 1", len(traceEvents))
	}
	if traceEvents[0].TraceEntry == nil || traceEvents[0].TraceEntry.Tool != "find_code" {
		t.Errorf("trace entry tool = %v, want find_code", traceEvents[0].TraceEntry)
	}

	// Answer matches what Ask would return.
	if len(ans.Trace) != 1 {
		t.Errorf("ans.Trace len = %d, want 1", len(ans.Trace))
	}
	if ans.Trace[0].Tool != "find_code" {
		t.Errorf("trace tool = %q, want find_code", ans.Trace[0].Tool)
	}
	if ans.Usage.InputTokens != 8 || ans.Usage.OutputTokens != 6 {
		t.Errorf("usage = %+v, want input=8 output=6", ans.Usage)
	}
}

// TestAskStream_GatesProseWhenNarrationClosed verifies the governance gate: when
// the narration posture is not Available (the default-closed state), AskStream
// must NOT emit prose token deltas — otherwise SSE clients would receive
// unvalidated LLM prose that the JSON path suppresses (Narrated=false). Tool
// lifecycle (trace) events still stream, and the final Answer is unchanged.
func TestAskStream_GatesProseWhenNarrationClosed(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "find_code", Arguments: map[string]any{"q": "x"}},
		},
	}
	turn2 := provider.Completion{Text: "here is the answer"}

	adapter := &scriptedStreamingAdapter{
		turns:       []provider.Completion{turn1, turn2},
		tokenDeltas: [][]string{nil, {"here is ", "the answer"}},
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// No SetNarrationPosture call: posture defaults to Unavailable (closed).

	var tokenEvents, traceEvents []StreamEvent
	ans, err := eng.AskStream(context.Background(), "what is x?", func(ev StreamEvent) {
		switch ev.Kind {
		case KindToken:
			tokenEvents = append(tokenEvents, ev)
		case KindTraceEntry:
			traceEvents = append(traceEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	// The leak is closed: zero prose token events when narration is not allowed.
	if len(tokenEvents) != 0 {
		t.Errorf("token events = %d, want 0 (narration closed)", len(tokenEvents))
	}
	// Tool-lifecycle streaming is unaffected.
	if len(traceEvents) != 1 {
		t.Errorf("trace events = %d, want 1", len(traceEvents))
	}
	// The final answer is still assembled; it is just not narrated.
	if ans.Narrated {
		t.Error("Narrated = true, want false when narration posture is closed")
	}
	if len(ans.Trace) != 1 || ans.Trace[0].Tool != "find_code" {
		t.Errorf("ans.Trace = %+v, want one find_code entry", ans.Trace)
	}
}

func TestAskStream_EmitsOnlyValidatedNarrationTokens(t *testing.T) {
	t.Parallel()

	const unsafeRawDelta = "AKIA1234567890ABCDEF"
	const validatedSentence = "Checkout uses the billing service."

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "find_code", Arguments: map[string]any{"q": "checkout"}},
		},
	}
	turn2 := provider.Completion{
		Text: "The checkout path leaked " + unsafeRawDelta,
	}
	narrationTurn := provider.Completion{
		Text: `{"sentences":[{"text":"` + validatedSentence + `","kind":"factual","provenance":[{"kind":"citation","id":"citation:packet"}]}]}`,
	}

	adapter := &scriptedStreamingAdapter{
		turns: []provider.Completion{turn1, turn2, narrationTurn},
		tokenDeltas: [][]string{
			nil,
			{"The checkout path leaked ", unsafeRawDelta},
		},
	}
	env := supportedEnvelope()
	env.Data = map[string]any{
		"answer_packet": query.AnswerPacket{
			TruthClass:  query.AnswerTruthDeterministic,
			Supported:   true,
			Summary:     validatedSentence,
			CitationRef: "citation:packet",
			Truth:       env.Truth,
		},
	}
	runner := &recordingRunner{env: env}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	eng.SetNarrationPosture(func() status.AnswerNarrationStatus {
		return status.AnswerNarrationStatus{State: status.AnswerNarrationAvailable}
	})

	var tokenText strings.Builder
	ans, err := eng.AskStream(context.Background(), "what handles checkout?", func(ev StreamEvent) {
		if ev.Kind == KindToken {
			tokenText.WriteString(ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	if strings.Contains(tokenText.String(), unsafeRawDelta) {
		t.Fatalf("streamed token text leaked unsafe raw completion %q in %q", unsafeRawDelta, tokenText.String())
	}
	if got := tokenText.String(); got != validatedSentence {
		t.Fatalf("streamed token text = %q, want validated narration %q", got, validatedSentence)
	}
	if !ans.Narrated {
		t.Fatal("Narrated = false, want true for validated narration")
	}
	if ans.Prose != validatedSentence {
		t.Fatalf("Prose = %q, want %q", ans.Prose, validatedSentence)
	}
}

// TestAskStream_NoStreaming verifies that AskStream returns ErrNoStreaming when
// the adapter does not implement provider.StreamingAdapter.
func TestAskStream_NoStreaming(t *testing.T) {
	t.Parallel()

	adapter := &scriptedAdapter{
		turns:    []provider.Completion{{Text: "answer"}},
		errOnIdx: -1,
	}
	runner := &recordingRunner{env: supportedEnvelope()}

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = eng.AskStream(context.Background(), "hi", func(StreamEvent) {})
	if err == nil {
		t.Fatal("expected ErrNoStreaming, got nil")
	}
	if err != ErrNoStreaming {
		t.Errorf("error = %v, want ErrNoStreaming", err)
	}
}

// TestAskStream_ToolCallStartedEvent verifies that AskStream emits
// KindToolCallStarted when the streaming adapter signals a tool call start.
func TestAskStream_ToolCallStartedEvent(t *testing.T) {
	t.Parallel()

	turn1 := provider.Completion{
		ToolCalls: []provider.ToolCall{
			{ID: "c2", Name: "list_services", Arguments: map[string]any{}},
		},
	}
	turn2 := provider.Completion{Text: "done"}

	adapterInner := &scriptedStreamingAdapter{
		turns:       []provider.Completion{turn1, turn2},
		tokenDeltas: nil,
	}

	// injectToolCallAdapter wraps scriptedStreamingAdapter and injects a
	// StreamEventToolCallStarted event on the first CompleteStream call.
	calls := 0
	var adapterWrapped provider.StreamingAdapter = &injectToolCallAdapter{
		inner:       adapterInner,
		injectOnIdx: 0,
		injectEvent: provider.StreamEvent{
			Kind:       provider.StreamEventToolCallStarted,
			ToolCallID: "c2",
			ToolName:   "list_services",
		},
		callsPtr: &calls,
	}

	runner := &recordingRunner{env: supportedEnvelope()}
	eng, err := New(adapterWrapped, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var toolStartEvents []StreamEvent
	_, err = eng.AskStream(context.Background(), "list services", func(ev StreamEvent) {
		if ev.Kind == KindToolCallStarted {
			toolStartEvents = append(toolStartEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	if len(toolStartEvents) != 1 {
		t.Fatalf("tool_call_started events = %d, want 1", len(toolStartEvents))
	}
	if toolStartEvents[0].ToolCallID != "c2" {
		t.Errorf("ToolCallID = %q, want c2", toolStartEvents[0].ToolCallID)
	}
	if toolStartEvents[0].ToolName != "list_services" {
		t.Errorf("ToolName = %q, want list_services", toolStartEvents[0].ToolName)
	}
}

// injectToolCallAdapter wraps scriptedStreamingAdapter and injects an extra
// StreamEvent on a specific call index before delegating to the inner adapter.
type injectToolCallAdapter struct {
	inner       *scriptedStreamingAdapter
	injectOnIdx int
	injectEvent provider.StreamEvent
	callsPtr    *int
}

func (a *injectToolCallAdapter) ModelID() string { return a.inner.ModelID() }

func (a *injectToolCallAdapter) Complete(ctx context.Context, msgs []provider.Message, tools []provider.Tool) (provider.Completion, error) {
	return a.inner.Complete(ctx, msgs, tools)
}

func (a *injectToolCallAdapter) CompleteStream(ctx context.Context, msgs []provider.Message, tools []provider.Tool, emit func(provider.StreamEvent)) (provider.Completion, error) {
	if *a.callsPtr == a.injectOnIdx {
		emit(a.injectEvent)
	}
	(*a.callsPtr)++
	return a.inner.CompleteStream(ctx, msgs, tools, emit)
}
