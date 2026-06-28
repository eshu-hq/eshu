// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
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
