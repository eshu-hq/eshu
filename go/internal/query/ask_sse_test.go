package query

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postAskSSE sends POST /api/v0/ask with Accept: text/event-stream.
func postAskSSE(h *AskHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	h.handleAsk(w, req)
	return w
}

// parseSSEEvents reads the raw SSE body and returns a slice of (eventName, dataJSON) pairs.
func parseSSEEvents(body string) []struct{ event, data string } {
	var events []struct{ event, data string }
	var cur struct{ event, data string }
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.event != "" {
				events = append(events, cur)
			}
			cur = struct{ event, data string }{}
		}
	}
	return events
}

func TestAskSSE_HappyPath(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "You have 2 repos.",
				Narrated: true,
				Trace: []AskTraceEntry{
					{Tool: "list_repos", Supported: true, TruthClass: AnswerTruthDeterministic},
					{Tool: "count_repos", Supported: true, TruthClass: AnswerTruthDeterministic},
				},
				Limitations: []string{"max 100 results"},
			},
		},
	}

	w := postAskSSE(h, `{"question":"how many repos?"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events := parseSSEEvents(w.Body.String())

	// Count event types.
	var traceCount, answerCount, doneCount int
	for _, ev := range events {
		switch ev.event {
		case "trace":
			traceCount++
		case "answer":
			answerCount++
		case "done":
			doneCount++
		}
	}

	if traceCount != 2 {
		t.Errorf("trace events = %d, want 2", traceCount)
	}
	if answerCount != 1 {
		t.Errorf("answer events = %d, want 1", answerCount)
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}

	// The answer event must parse as the canonical askResponse.
	var ansEv *struct{ event, data string }
	for i, ev := range events {
		if ev.event == "answer" {
			ansEv = &events[i]
			break
		}
	}
	if ansEv == nil {
		t.Fatal("no answer event found")
	}
	var resp askResponse
	if err := json.Unmarshal([]byte(ansEv.data), &resp); err != nil {
		t.Fatalf("parse answer event data: %v", err)
	}
	if resp.AnswerProse != "You have 2 repos." {
		t.Errorf("answer_prose = %q, want %q", resp.AnswerProse, "You have 2 repos.")
	}
	if len(resp.Limitations) != 1 || resp.Limitations[0] != "max 100 results" {
		t.Errorf("limitations = %v", resp.Limitations)
	}
	if len(resp.QueryTrace) != 2 {
		t.Errorf("query_trace len = %d, want 2", len(resp.QueryTrace))
	}
}

func TestAskSSE_TraceEventShape(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "ok",
				Narrated: true,
				Trace: []AskTraceEntry{
					{Tool: "search", Supported: true, TruthClass: AnswerTruthDerived, Err: ""},
				},
			},
		},
	}

	w := postAskSSE(h, `{"question":"search for something"}`)
	events := parseSSEEvents(w.Body.String())

	var traceEvent *struct{ event, data string }
	for i, ev := range events {
		if ev.event == "trace" {
			traceEvent = &events[i]
			break
		}
	}
	if traceEvent == nil {
		t.Fatal("no trace event emitted")
	}

	var te traceEntry
	if err := json.Unmarshal([]byte(traceEvent.data), &te); err != nil {
		t.Fatalf("parse trace event: %v", err)
	}
	if te.Tool != "search" {
		t.Errorf("trace tool = %q, want search", te.Tool)
	}
	if !te.Supported {
		t.Error("trace supported should be true")
	}
	if te.TruthClass != string(AnswerTruthDerived) {
		t.Errorf("trace truth_class = %q, want %q", te.TruthClass, AnswerTruthDerived)
	}
}

func TestAskSSE_Disabled_Returns503JSON(t *testing.T) {
	t.Parallel()

	// Nil Asker → disabled. Must return 503 JSON, NOT an event stream.
	h := &AskHandler{Asker: nil}
	w := postAskSSE(h, `{"question":"anything"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		t.Errorf("disabled handler must NOT return event-stream, got Content-Type %q", ct)
	}

	var resp askUnavailableResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.State != "unavailable" {
		t.Errorf("state = %q, want unavailable", resp.State)
	}
}

func TestAskSSE_EmptyQuestion_Returns400(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{}}
	w := postAskSSE(h, `{"question":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	// Must not open a stream.
	ct := w.Header().Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		t.Errorf("empty question must NOT open event-stream, got Content-Type %q", ct)
	}
}

func TestAskSSE_EngineError_EmitsErrorEvent(t *testing.T) {
	t.Parallel()

	const secretText = "SECRET_PROVIDER_BODY_12345"
	// errAskerWithSecret always returns an error whose message contains a secret.
	// The SSE handler must NOT echo this into the stream.
	h := &AskHandler{Asker: &errAskerWithSecret{secret: secretText}}
	w := postAskSSE(h, `{"question":"list services"}`)

	if w.Code != http.StatusOK {
		// After SSE headers are committed the status code stays 200; the error
		// is communicated via the "error" event.
		t.Fatalf("expected 200 (SSE open), got %d", w.Code)
	}

	body := w.Body.String()
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

	// The secret must not appear anywhere in the stream.
	if strings.Contains(body, secretText) {
		t.Errorf("secret leaked into SSE stream: %s", body)
	}

	// The error event payload must be a bounded unavailable response.
	var resp askUnavailableResponse
	if err := json.Unmarshal([]byte(errorEvent.data), &resp); err != nil {
		t.Fatalf("parse error event: %v", err)
	}
	if resp.State != "unavailable" {
		t.Errorf("error event state = %q, want unavailable", resp.State)
	}
}

// errAskerWithSecret is an Asker that returns an error whose text contains a
// secret. Used to verify the SSE handler never echoes the error body.
type errAskerWithSecret struct {
	secret string
}

func (e *errAskerWithSecret) Ask(_ *http.Request, _ string) (AskAnswer, error) {
	return AskAnswer{}, &sseSecretError{secret: e.secret}
}

type sseSecretError struct{ secret string }

func (e *sseSecretError) Error() string { return "engine failure: " + e.secret }

func TestAskSSE_NonSSEPathUnchanged(t *testing.T) {
	t.Parallel()

	// A request without Accept: text/event-stream must still return JSON.
	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "regression check",
				Narrated: true,
			},
		},
	}
	w := postAsk(h, `{"question":"regression check"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on JSON path, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("non-SSE path Content-Type = %q, want application/json", ct)
	}

	var resp askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON path response: %v", err)
	}
	if resp.AnswerProse != "regression check" {
		t.Errorf("answer_prose = %q, want %q", resp.AnswerProse, "regression check")
	}
}

func TestAskSSE_NoFlusher_Returns500(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{
		answer: AskAnswer{Prose: "ok", Narrated: true},
	}}

	// noFlushWriter does not implement http.Flusher — it is a plain
	// ResponseWriter with no Flush method. httptest.ResponseRecorder embeds
	// Flush() so we cannot use it here; instead we write a minimal recorder.
	nfw := &noFlushWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(`{"question":"anything"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	h.handleAsk(nfw, req)

	if nfw.code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Flusher absent, got %d", nfw.code)
	}
}

// noFlushWriter is a minimal http.ResponseWriter that does NOT implement
// http.Flusher. It is used to exercise the SSE no-flusher fallback path.
type noFlushWriter struct {
	header http.Header
	code   int
	buf    strings.Builder
}

func (w *noFlushWriter) Header() http.Header         { return w.header }
func (w *noFlushWriter) WriteHeader(code int)        { w.code = code }
func (w *noFlushWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }
