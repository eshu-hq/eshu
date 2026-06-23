package query

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
