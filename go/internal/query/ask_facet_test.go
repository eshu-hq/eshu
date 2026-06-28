// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildAskResponse_FacetHelm verifies that a question naming "helm" yields:
//   - applied_facets.source_tool = "helm"
//   - a detected-intent limitation mentioning source_tool=helm
//   - no unknown_tool_note
func TestBuildAskResponse_FacetHelm(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Prose:    "Services that deploy via Helm.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "which services deploy via Helm?", "")

	if resp.AppliedFacets == nil {
		t.Fatal("applied_facets is nil, want non-nil for a helm question")
	}
	if resp.AppliedFacets.SourceTool != "helm" {
		t.Errorf("applied_facets.source_tool = %q, want %q", resp.AppliedFacets.SourceTool, "helm")
	}
	if resp.AppliedFacets.Language != "" {
		t.Errorf("applied_facets.language = %q, want empty", resp.AppliedFacets.Language)
	}
	if resp.AppliedFacets.UnknownToolNote != "" {
		t.Errorf("applied_facets.unknown_tool_note = %q, want empty", resp.AppliedFacets.UnknownToolNote)
	}
	if !hasLimitation(resp.Limitations, "Detected a source_tool=helm intent") {
		t.Errorf("limitations %v missing detected-intent source_tool=helm note", resp.Limitations)
	}
}

// TestBuildAskResponse_FacetGoLanguage verifies that a question naming "Go repos"
// yields applied_facets.language = "go" and a detected-intent language limitation.
func TestBuildAskResponse_FacetGoLanguage(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Prose:    "Go repos that depend on lib-common.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "what Go repos depend on lib-common?", "")

	if resp.AppliedFacets == nil {
		t.Fatal("applied_facets is nil, want non-nil for a Go language question")
	}
	if resp.AppliedFacets.Language != "go" {
		t.Errorf("applied_facets.language = %q, want %q", resp.AppliedFacets.Language, "go")
	}
	if resp.AppliedFacets.SourceTool != "" {
		t.Errorf("applied_facets.source_tool = %q, want empty", resp.AppliedFacets.SourceTool)
	}
	if !hasLimitation(resp.Limitations, "Detected a language=go intent") {
		t.Errorf("limitations %v missing detected-intent language=go note", resp.Limitations)
	}
}

// TestBuildAskResponse_FacetUnknownTool verifies that a question naming an
// unrecognized tool (e.g. "Frobnicator") yields:
//   - applied_facets with a non-empty unknown_tool_note
//   - a "not a recognized Eshu source_tool" limitation
//   - no source_tool or language set
func TestBuildAskResponse_FacetUnknownTool(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Prose:    "No result.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "which services deploy via Frobnicator?", "")

	if resp.AppliedFacets == nil {
		t.Fatal("applied_facets is nil, want non-nil when unknown tool detected")
	}
	if resp.AppliedFacets.SourceTool != "" {
		t.Errorf("applied_facets.source_tool = %q, want empty for unknown tool", resp.AppliedFacets.SourceTool)
	}
	if resp.AppliedFacets.Language != "" {
		t.Errorf("applied_facets.language = %q, want empty", resp.AppliedFacets.Language)
	}
	if resp.AppliedFacets.UnknownToolNote == "" {
		t.Error("applied_facets.unknown_tool_note is empty, want a note for the unknown tool mention")
	}
	if !strings.Contains(resp.AppliedFacets.UnknownToolNote, "frobnicator") {
		t.Errorf("unknown_tool_note %q does not mention frobnicator", resp.AppliedFacets.UnknownToolNote)
	}
	if !hasLimitation(resp.Limitations, "not a recognized Eshu source_tool") {
		t.Errorf("limitations %v missing 'not a recognized Eshu source_tool' note", resp.Limitations)
	}
}

// TestBuildAskResponse_FacetPlainQuestion verifies that a plain question with no
// tool or language mention yields applied_facets=nil and no scoping limitations.
func TestBuildAskResponse_FacetPlainQuestion(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Prose:    "You have 3 services.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "which services are in production?", "")

	if resp.AppliedFacets != nil {
		t.Errorf("applied_facets = %+v, want nil for a plain question", resp.AppliedFacets)
	}
	for _, lim := range resp.Limitations {
		if strings.Contains(lim, "source_tool") || strings.Contains(lim, "language=") {
			t.Errorf("unexpected scoping limitation for plain question: %q", lim)
		}
	}
}

// TestBuildAskResponse_FacetJSONShape verifies the applied_facets field
// serialises to the documented wire shape.
func TestBuildAskResponse_FacetJSONShape(t *testing.T) {
	t.Parallel()

	ans := AskAnswer{
		Prose:    "Helm services.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}

	resp := buildAskResponse(ans, "services deployed via helm", "")
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	raw, ok := wire["applied_facets"]
	if !ok {
		t.Fatal("applied_facets field missing from JSON response")
	}
	var af map[string]string
	if err := json.Unmarshal(raw, &af); err != nil {
		t.Fatalf("unmarshal applied_facets: %v", err)
	}
	if af["source_tool"] != "helm" {
		t.Errorf("applied_facets.source_tool = %q, want helm", af["source_tool"])
	}
	// language and unknown_tool_note must be absent (omitempty) when empty.
	if _, exists := af["language"]; exists {
		t.Error("applied_facets.language present in JSON, want omitted when empty")
	}
	if _, exists := af["unknown_tool_note"]; exists {
		t.Error("applied_facets.unknown_tool_note present in JSON, want omitted when empty")
	}
}

// TestAskHandler_FacetHelm exercises the full HTTP handler path with a helm
// question, confirming applied_facets appears in the 200 response.
func TestAskHandler_FacetHelm(t *testing.T) {
	t.Parallel()

	h := &AskHandler{
		Asker: &fakeAsker{
			answer: AskAnswer{
				Prose:    "Services deployed by Helm.",
				Narrated: true,
				Packets: []AnswerPacket{{
					TruthClass: AnswerTruthDeterministic,
					Supported:  true,
				}},
			},
		},
	}

	w := postAsk(h, `{"question":"which services deploy via helm?"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := wire["applied_facets"]; !ok {
		t.Fatal("applied_facets missing from handler response")
	}
}

// TestHandleAskAppliedFacetsDetectedIntent drives h.handleAsk through an
// httptest mux (mirroring the Mount pattern) and verifies:
//   - A Helm question yields applied_facets.source_tool=="helm" and a
//     detected-intent limitation; the limitation must NOT contain "steered".
//   - A plain question (no tool or language) yields no applied_facets.
func TestHandleAskAppliedFacetsDetectedIntent(t *testing.T) {
	t.Parallel()

	fakeAnswer := AskAnswer{
		Prose:    "Helm services.",
		Narrated: true,
		Packets: []AnswerPacket{{
			TruthClass: AnswerTruthDeterministic,
			Supported:  true,
		}},
	}
	h := &AskHandler{Asker: &fakeAsker{answer: fakeAnswer}}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/ask", h.handleAsk)

	// --- Helm question: expect applied_facets with source_tool=="helm" ---
	helmBody := strings.NewReader(`{"question":"which services are deployed via helm charts?"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask", helmBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("helm question: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var helmWire struct {
		AppliedFacets *struct {
			SourceTool string `json:"source_tool"`
			Language   string `json:"language"`
		} `json:"applied_facets"`
		Limitations []string `json:"limitations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &helmWire); err != nil {
		t.Fatalf("helm question: unmarshal: %v", err)
	}
	if helmWire.AppliedFacets == nil {
		t.Fatal("helm question: applied_facets is nil, want non-nil")
	}
	if helmWire.AppliedFacets.SourceTool != "helm" {
		t.Errorf("helm question: applied_facets.source_tool = %q, want %q", helmWire.AppliedFacets.SourceTool, "helm")
	}
	if helmWire.AppliedFacets.Language != "" {
		t.Errorf("helm question: applied_facets.language = %q, want empty", helmWire.AppliedFacets.Language)
	}
	if !hasLimitation(helmWire.Limitations, "Detected a source_tool=helm intent") {
		t.Errorf("helm question: limitations %v missing detected-intent note", helmWire.Limitations)
	}
	// The limitation must reflect detected intent only — never claim the agent was "steered".
	for _, lim := range helmWire.Limitations {
		if strings.Contains(lim, "steered") {
			t.Errorf("helm question: limitation contains forbidden word 'steered': %q", lim)
		}
	}

	// --- Plain question: expect no applied_facets ---
	plainBody := strings.NewReader(`{"question":"which services are in production?"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v0/ask", plainBody)
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("plain question: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var plainWire struct {
		AppliedFacets *json.RawMessage `json:"applied_facets"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &plainWire); err != nil {
		t.Fatalf("plain question: unmarshal: %v", err)
	}
	if plainWire.AppliedFacets != nil {
		t.Errorf("plain question: applied_facets = %s, want nil (omitted)", *plainWire.AppliedFacets)
	}
}
