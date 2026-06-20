package query

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestAskSSE_Streaming_ForwardsValidatedTokenEvents verifies that a
// streaming-capable Asker can produce "event: token" events before the final
// "event: answer" event. The production engine only emits token events for
// validated narration prose.
func TestAskSSE_Streaming_ForwardsValidatedTokenEvents(t *testing.T) {
	t.Parallel()

	traceTE := AskTraceEntry{Tool: "list_repos", Supported: true, TruthClass: AnswerTruthDeterministic}
	h := &AskHandler{
		Asker: &fakeStreamingAsker{
			events: []AskStreamEvent{
				{Kind: "token", TextDelta: "Hello"},
				{Kind: "token", TextDelta: ", world"},
				{Kind: "token", TextDelta: "!"},
				{Kind: "trace_entry", TraceEntry: &traceTE},
			},
			answer: AskAnswer{
				Prose:    "Hello, world!",
				Narrated: true,
				Trace:    []AskTraceEntry{traceTE},
			},
		},
	}

	w := postAskSSE(h, `{"question":"hi"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	events := parseSSEEvents(w.Body.String())

	var tokenCount, traceCount, answerCount, doneCount int
	var tokenDeltas []string
	for _, ev := range events {
		switch ev.event {
		case "token":
			tokenCount++
			var p struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(ev.data), &p); err != nil {
				t.Errorf("parse token event data: %v", err)
			}
			tokenDeltas = append(tokenDeltas, p.Delta)
		case "trace":
			traceCount++
		case "answer":
			answerCount++
		case "done":
			doneCount++
		}
	}

	if tokenCount != 3 {
		t.Errorf("token events = %d, want 3", tokenCount)
	}
	if traceCount != 1 {
		t.Errorf("trace events = %d, want 1", traceCount)
	}
	if answerCount != 1 {
		t.Errorf("answer events = %d, want 1", answerCount)
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}

	concatenated := strings.Join(tokenDeltas, "")
	if concatenated != "Hello, world!" {
		t.Errorf("concatenated token deltas = %q, want %q", concatenated, "Hello, world!")
	}

	// Token events must appear before the answer event in the stream.
	firstTokenIdx, answerIdx := -1, -1
	for i, ev := range events {
		if ev.event == "token" && firstTokenIdx == -1 {
			firstTokenIdx = i
		}
		if ev.event == "answer" {
			answerIdx = i
		}
	}
	if firstTokenIdx == -1 || answerIdx == -1 {
		t.Fatal("missing token or answer event")
	}
	if firstTokenIdx > answerIdx {
		t.Errorf("first token event (idx %d) must appear before answer event (idx %d)", firstTokenIdx, answerIdx)
	}
}

// TestAskSSE_Streaming_FallbackToSync verifies that when AskStream returns
// ErrNoStreaming the handler falls back to the synchronous path and still
// produces "trace", "answer", and "done" events (no "token" events).
func TestAskSSE_Streaming_FallbackToSync(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "sync answer",
				Narrated: true,
				Trace: []AskTraceEntry{
					{Tool: "list_services", Supported: true, TruthClass: AnswerTruthDeterministic},
				},
			},
		},
	}

	w := postAskSSE(h, `{"question":"list services"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	events := parseSSEEvents(w.Body.String())

	var tokenCount, traceCount, answerCount, doneCount int
	for _, ev := range events {
		switch ev.event {
		case "token":
			tokenCount++
		case "trace":
			traceCount++
		case "answer":
			answerCount++
		case "done":
			doneCount++
		}
	}

	// Sync fallback must not emit token events.
	if tokenCount != 0 {
		t.Errorf("sync fallback should emit 0 token events, got %d", tokenCount)
	}
	if traceCount != 1 {
		t.Errorf("trace events = %d, want 1", traceCount)
	}
	if answerCount != 1 {
		t.Errorf("answer events = %d, want 1", answerCount)
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}
}

// TestAskSSE_Streaming_LeakSafe verifies that a streaming error containing a
// secret never appears in the SSE stream.
func TestAskSSE_Streaming_LeakSafe(t *testing.T) {
	t.Parallel()

	const secretText = "STREAMING_SECRET_9999"
	h := &AskHandler{Asker: &errAskerWithSecret{secret: secretText}}
	w := postAskSSE(h, `{"question":"list services"}`)

	body := w.Body.String()
	if strings.Contains(body, secretText) {
		t.Errorf("secret leaked into streaming SSE stream: %s", body)
	}

	events := parseSSEEvents(body)
	var errorEvent *struct{ event, data string }
	for i, ev := range events {
		if ev.event == "error" {
			errorEvent = &events[i]
			break
		}
	}
	if errorEvent == nil {
		t.Fatal("expected an 'error' SSE event, got none")
	}
}

func TestAskSSE_StreamingFinalAnswerSuppressesUnsafeNarration(t *testing.T) {
	t.Parallel()

	rawAddress := strings.Join([]string{"10", "66", "8", "4"}, ".")
	h := &AskHandler{
		Asker: &fakeStreamingAsker{
			answer: AskAnswer{
				Prose:    "The private host is " + rawAddress,
				Narrated: true,
				Packets: []AnswerPacket{{
					TruthClass:      AnswerTruthDeterministic,
					Supported:       true,
					EvidenceHandles: []evidenceCitationHandle{{Kind: "entity", EntityID: "service:checkout"}},
				}},
			},
		},
	}

	w := postAskSSE(h, `{"question":"list services"}`)
	body := w.Body.String()
	if strings.Contains(body, rawAddress) {
		t.Fatalf("unsafe address leaked into SSE stream: %s", body)
	}

	events := parseSSEEvents(body)
	var answer askResponse
	for _, ev := range events {
		if ev.event != "answer" {
			continue
		}
		if err := json.Unmarshal([]byte(ev.data), &answer); err != nil {
			t.Fatalf("decode answer event: %v", err)
		}
	}
	if answer.AnswerProse != "" {
		t.Fatalf("answer_prose = %q, want suppressed unsafe prose", answer.AnswerProse)
	}
	if !answer.Partial {
		t.Fatal("partial = false, want true when runtime guardrail suppresses output")
	}
	if !hasLimitation(answer.Limitations, "publish_safety") {
		t.Fatalf("limitations = %#v, want publish_safety marker", answer.Limitations)
	}
}
