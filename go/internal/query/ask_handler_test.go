package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAsker is an Asker stub for unit tests. AskStream returns ErrNoStreaming
// so the SSE handler exercises the synchronous fallback path by default. Tests
// that need live streaming should use fakeStreamingAsker instead.
type fakeAsker struct {
	answer AskAnswer
	err    error
}

func (f *fakeAsker) Ask(_ *http.Request, _ string) (AskAnswer, error) {
	return f.answer, f.err
}

// AskStream returns ErrNoStreaming so tests using fakeAsker exercise the
// synchronous SSE fallback path.
func (f *fakeAsker) AskStream(_ *http.Request, _ string, _ func(AskStreamEvent)) (AskAnswer, error) {
	return AskAnswer{}, ErrNoStreaming
}

// errAsker always returns an error.
type errAsker struct{}

func (e *errAsker) Ask(_ *http.Request, _ string) (AskAnswer, error) {
	return AskAnswer{}, &fakeAskErr{}
}

func (e *errAsker) AskStream(_ *http.Request, _ string, _ func(AskStreamEvent)) (AskAnswer, error) {
	return AskAnswer{}, ErrNoStreaming
}

type fakeAskErr struct{}

func (e *fakeAskErr) Error() string { return "engine failure" }

// postAsk sends a POST /api/v0/ask request with the given body.
func postAsk(h *AskHandler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleAsk(w, req)
	return w
}

func TestAskHandler_Disabled(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: nil}
	w := postAsk(h, `{"question":"what services do I have?"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp askUnavailableResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.State != "unavailable" {
		t.Errorf("state = %q, want unavailable", resp.State)
	}
	if resp.Reason == "" {
		t.Error("reason must not be empty")
	}
}

func TestAskHandler_EmptyQuestion(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{}}
	w := postAsk(h, `{"question":""}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAskHandler_MissingQuestion(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{}}
	w := postAsk(h, `{}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAskHandler_WhitespaceOnlyQuestion(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{}}
	w := postAsk(h, `{"question":"   "}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAskHandler_BadJSON(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &fakeAsker{}}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleAsk(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAskHandler_EngineError_Returns503(t *testing.T) {
	t.Parallel()

	h := &AskHandler{Asker: &errAsker{}}
	w := postAsk(h, `{"question":"what repos do I have?"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp askUnavailableResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Must NOT leak engine error text.
	if strings.Contains(resp.Reason, "engine failure") {
		t.Errorf("leaked engine error in reason: %q", resp.Reason)
	}
}

func TestAskHandler_SuccessResponseShape(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:       "You have 3 services.",
				Narrated:    true,
				Partial:     false,
				Limitations: []string{"limited to 100 results"},
				Trace: []AskTraceEntry{
					{Tool: "list_services", Supported: true, TruthClass: AnswerTruthDeterministic},
				},
			},
		},
	}
	w := postAsk(h, `{"question":"what services do I have?"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if resp.AnswerProse != "You have 3 services." {
		t.Errorf("answer_prose = %q, want %q", resp.AnswerProse, "You have 3 services.")
	}
	if resp.Partial {
		t.Error("partial should be false")
	}
	if len(resp.Limitations) != 1 || resp.Limitations[0] != "limited to 100 results" {
		t.Errorf("limitations = %v", resp.Limitations)
	}
	if len(resp.QueryTrace) != 1 {
		t.Fatalf("expected 1 trace entry, got %d", len(resp.QueryTrace))
	}
	if resp.QueryTrace[0].Tool != "list_services" {
		t.Errorf("trace tool = %q", resp.QueryTrace[0].Tool)
	}
}

func TestAskHandler_NoNarration_ProseEmpty(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "",
				Narrated: false,
			},
		},
	}
	w := postAsk(h, `{"question":"what services?"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp askResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AnswerProse != "" {
		t.Errorf("answer_prose should be empty when not narrated, got %q", resp.AnswerProse)
	}
}

func TestAskHandler_PartialAnswer(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Partial: true,
			},
		},
	}
	w := postAsk(h, `{"question":"list repos"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["partial"] != true {
		t.Errorf("partial = %v, want true", resp["partial"])
	}
}

func TestAskHandler_DisabledNoEngineConstruction(t *testing.T) {
	t.Parallel()

	// Verify the disabled handler never invokes the asker.
	h := &AskHandler{Asker: nil}
	w := postAsk(h, `{"question":"anything"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when disabled, got %d", w.Code)
	}

	var resp askUnavailableResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "unavailable" {
		t.Errorf("state = %q", resp.State)
	}
}

func TestAskHandler_Mount(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	h := &AskHandler{Asker: nil}
	h.Mount(mux)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"question":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", body)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, req)

	// Disabled → 503.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 from mux, got %d", w.Code)
	}
}

func TestBuildAskResponse_TruthClassFromPrimary(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Packets: []AnswerPacket{
			{TruthClass: AnswerTruthUnsupported, Supported: false},
			{TruthClass: AnswerTruthDeterministic, Supported: true},
		},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.TruthClass != string(AnswerTruthDeterministic) {
		t.Errorf("truth_class = %q, want %q", resp.TruthClass, AnswerTruthDeterministic)
	}
}

func TestBuildAskResponse_LeakSafety(t *testing.T) {
	t.Parallel()

	// Engine returned an answer with prose but Narrated=false.
	// The prose must not appear in the response.
	ans := AskAnswer{
		Prose:    "PROVIDER_BODY: api_key=supersecret",
		Narrated: false,
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != "" {
		t.Errorf("leaked prose when not narrated: %q", resp.AnswerProse)
	}

	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), "supersecret") {
		t.Errorf("leaked credential in response: %s", string(b))
	}
}

func TestBuildAskResponse_SuppressesUnsafeNarratedOutput(t *testing.T) {
	t.Parallel()

	rawAddress := strings.Join([]string{"10", "88", "4", "7"}, ".")
	ans := AskAnswer{
		Prose:    "The private host is " + rawAddress,
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass:      AnswerTruthDeterministic,
			Supported:       true,
			EvidenceHandles: []evidenceCitationHandle{{Kind: "entity", EntityID: "service:checkout"}},
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != "" {
		t.Fatalf("answer_prose = %q, want suppressed unsafe prose", resp.AnswerProse)
	}
	if !resp.Partial {
		t.Fatal("partial = false, want true when guardrail suppresses output")
	}
	if !hasLimitation(resp.Limitations, "publish_safety") {
		t.Fatalf("limitations = %#v, want publish_safety marker", resp.Limitations)
	}
}

func TestBuildAskResponse_DropsUnsafeGuardrailFields(t *testing.T) {
	t.Parallel()

	rawAddress := strings.Join([]string{"10", "91", "7", "12"}, ".")
	rawToken := strings.Join([]string{"token", "do-not-echo"}, "=")
	ans := AskAnswer{
		Prose:       "checkout-service owns refunds",
		Narrated:    true,
		Limitations: []string{"upstream host " + rawAddress},
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
			EvidenceHandles: []evidenceCitationHandle{{
				Kind:     "entity",
				EntityID: "service:checkout",
				Reason:   rawToken,
			}},
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(body), rawAddress) {
		t.Fatalf("unsafe limitation leaked into response: %s", string(body))
	}
	if strings.Contains(string(body), rawToken) {
		t.Fatalf("unsafe evidence handle leaked into response: %s", string(body))
	}
	if len(resp.EvidenceHandles) != 0 {
		t.Fatalf("evidence_handles = %#v, want dropped unsafe handle", resp.EvidenceHandles)
	}
	if !hasLimitation(resp.Limitations, "publish_safety") {
		t.Fatalf("limitations = %#v, want publish_safety marker", resp.Limitations)
	}
}

func TestBuildAskResponse_PublishesTruthProvenanceAnswerWithoutCitations(t *testing.T) {
	t.Parallel()

	// A supported, narrated answer backed by a classified packet's truth
	// provenance (non-empty truth_class) but carrying no inlined citation
	// handles must PUBLISH: truth provenance is an accepted citation_coverage
	// class, mirroring the narration validator (issue #3609).
	ans := AskAnswer{
		Prose:    "checkout-service owns refunds",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse == "" {
		t.Fatal("answer_prose = \"\", want published truth-provenance prose (#3609)")
	}
	if hasLimitation(resp.Limitations, "citation_coverage") {
		t.Fatalf("limitations = %#v, want no citation_coverage marker for truth-provenance prose", resp.Limitations)
	}
}

func TestBuildAskResponse_SuppressesGenuinelyUncitedAnswer(t *testing.T) {
	t.Parallel()

	// A supported answer with NO truth provenance (empty truth_class) and NO
	// citation handles is genuinely uncited and must still be suppressed by the
	// citation_coverage guardrail (the #3609 change must not weaken this).
	ans := AskAnswer{
		Prose:    "checkout-service owns refunds",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: "",
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != "" {
		t.Fatalf("answer_prose = %q, want suppressed genuinely-uncited prose", resp.AnswerProse)
	}
	if !resp.Partial {
		t.Fatal("partial = false, want true when citation guardrail suppresses output")
	}
	if !hasLimitation(resp.Limitations, "citation_coverage") {
		t.Fatalf("limitations = %#v, want citation_coverage marker", resp.Limitations)
	}
}

// TestBuildAskResponse_DerivedProseFallbackWhenNotNarrated proves the
// defense-in-depth fallback: when the engine could not narrate (Narrated=false)
// but a supported packet carries a non-empty deterministic Summary, the handler
// surfaces that Summary as answer_prose instead of returning empty prose. This
// prevents total prose loss whenever narration is unavailable or rejected
// (issue #3550). The fallback prose is publish-safety checked and clearly
// marked as derived/un-narrated via a limitation so callers do not mistake it
// for a governed narration.
func TestBuildAskResponse_DerivedProseFallbackWhenNotNarrated(t *testing.T) {
	t.Parallel()

	const summary = "checkout-service owns refund processing"
	ans := AskAnswer{
		Prose:    "",
		Narrated: false,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
			Summary:    summary,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != summary {
		t.Fatalf("answer_prose = %q, want derived summary %q", resp.AnswerProse, summary)
	}
	if !hasLimitation(resp.Limitations, "derived") {
		t.Fatalf("limitations = %#v, want derived/un-narrated marker", resp.Limitations)
	}
}

// TestBuildAskResponse_DerivedProseFallbackCarriesTruthProvenance proves the
// derived fallback never publishes bare uncited prose. When narration is
// unavailable (Narrated=false) and the supported packet has no EvidenceHandles
// and no citation_ref, the surfaced answer_prose must still carry explicit
// provenance coverage: the narration path guarantees citation coverage (via a
// citation_ref/handles or, for an uncitable packet, truth provenance keyed to
// truth_class), and the deterministic fallback must match that guarantee rather
// than emitting prose with no citation or provenance reference (issue #3550).
func TestBuildAskResponse_DerivedProseFallbackCarriesTruthProvenance(t *testing.T) {
	t.Parallel()

	const summary = "checkout-service owns refund processing"
	ans := AskAnswer{
		Prose:    "",
		Narrated: false,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
			Summary:    summary,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != summary {
		t.Fatalf("answer_prose = %q, want derived summary %q", resp.AnswerProse, summary)
	}
	if resp.TruthClass != string(AnswerTruthDeterministic) {
		t.Fatalf("truth_class = %q, want %q", resp.TruthClass, AnswerTruthDeterministic)
	}
	// With no handles and no citation_ref, the coverage is the packet's truth
	// provenance. The response must mark that explicitly so the prose is not
	// bare uncited prose.
	if !hasLimitation(resp.Limitations, "truth provenance") {
		t.Fatalf("limitations = %#v, want explicit truth-provenance coverage marker", resp.Limitations)
	}
}

// TestBuildAskResponse_DerivedProseFallbackSurfacesCitationRef proves the
// derived fallback carries the packet's citation_ref as citation coverage when
// the packet has one, so derived prose is anchored to the citation packet that
// hydrates its evidence handles rather than published uncited (issue #3550).
func TestBuildAskResponse_DerivedProseFallbackSurfacesCitationRef(t *testing.T) {
	t.Parallel()

	const summary = "checkout-service owns refund processing"
	const citationRef = "eshu://citation/checkout-refunds"
	ans := AskAnswer{
		Prose:    "",
		Narrated: false,
		Packets: []AnswerPacket{{
			TruthClass:  AnswerTruthDeterministic,
			Supported:   true,
			Summary:     summary,
			CitationRef: citationRef,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != summary {
		t.Fatalf("answer_prose = %q, want derived summary %q", resp.AnswerProse, summary)
	}
	if resp.CitationRef != citationRef {
		t.Fatalf("citation_ref = %q, want %q surfaced as coverage", resp.CitationRef, citationRef)
	}
}

// TestBuildAskResponse_DerivedProseFallbackKeepsEvidenceHandleCoverage proves
// the derived fallback leaves resolved evidence handles intact as citation
// coverage: when the supported packet carries handles, surfaced derived prose is
// covered by those handles and no redundant truth-provenance marker is added
// (issue #3550).
func TestBuildAskResponse_DerivedProseFallbackKeepsEvidenceHandleCoverage(t *testing.T) {
	t.Parallel()

	const summary = "checkout-service owns refund processing"
	ans := AskAnswer{
		Prose:    "",
		Narrated: false,
		Packets: []AnswerPacket{{
			TruthClass:      AnswerTruthDeterministic,
			Supported:       true,
			Summary:         summary,
			EvidenceHandles: []evidenceCitationHandle{{Kind: "entity", EntityID: "service:checkout"}},
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != summary {
		t.Fatalf("answer_prose = %q, want derived summary %q", resp.AnswerProse, summary)
	}
	if len(resp.EvidenceHandles) != 1 {
		t.Fatalf("evidence_handles = %#v, want one handle preserved as coverage", resp.EvidenceHandles)
	}
	if hasLimitation(resp.Limitations, "truth provenance") {
		t.Fatalf("limitations = %#v, want no truth-provenance marker when handles cover the prose", resp.Limitations)
	}
}

// TestBuildAskResponse_DerivedProseFallbackSkipsUnsafeSummary proves the derived
// fallback still honors publish safety: an unsafe deterministic Summary is never
// surfaced as answer_prose even when narration is unavailable.
func TestBuildAskResponse_DerivedProseFallbackSkipsUnsafeSummary(t *testing.T) {
	t.Parallel()

	rawAddress := strings.Join([]string{"10", "0", "5", "9"}, ".")
	ans := AskAnswer{
		Prose:    "",
		Narrated: false,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
			Summary:    "the private host is " + rawAddress,
		}},
	}

	resp := buildAskResponse(ans, "q", "")
	if resp.AnswerProse != "" {
		t.Fatalf("answer_prose = %q, want empty for unsafe derived summary", resp.AnswerProse)
	}
	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), rawAddress) {
		t.Fatalf("unsafe derived summary leaked into response: %s", string(b))
	}
}

func hasLimitation(limitations []string, want string) bool {
	for _, limitation := range limitations {
		if strings.Contains(limitation, want) {
			return true
		}
	}
	return false
}
