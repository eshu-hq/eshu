// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRenderSummaryGroupsSections(t *testing.T) {
	out := &bytes.Buffer{}
	if err := RenderSummary(out, Envelope{Data: sampleData("mapped"), Truth: freshTruth()}); err != nil {
		t.Fatalf("RenderSummary() error = %v, want nil", err)
	}
	got := out.String()
	want := strings.Join([]string{
		"Map: terraform/aws_lb.main",
		"Resolved: TerraformResource tfstate:aws_lb.main (aws_lb.main)",
		"Defined by:",
		"- DEFINES infra-repo",
		"Depends on:",
		"- PROVISIONS_DEPENDENCY_FOR checkout repo=repo-api",
		"Evidence: 2 relationships",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderSummaryOmitsEmptySectionsAndFlagsTruncation(t *testing.T) {
	out := &bytes.Buffer{}
	envelope := Envelope{Data: map[string]any{
		"status":   "mapped",
		"from":     "workload:checkout",
		"sections": map[string]any{"defined_by": []any{}, "runs_as": nil},
		"evidence": map[string]any{"relationship_count": float64(9), "truncated": true},
	}}
	if err := RenderSummary(out, envelope); err != nil {
		t.Fatalf("RenderSummary() error = %v, want nil", err)
	}
	got := out.String()
	if strings.Contains(got, "Defined by") || strings.Contains(got, "Runs as") {
		t.Fatalf("empty sections printed a title:\n%s", got)
	}
	for _, want := range []string{"Map: workload:checkout", "Evidence: 9 relationships", "Truncated: true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Resolved:") {
		t.Fatalf("summary printed a Resolved line with no selected entity:\n%s", got)
	}
}

func TestRenderSummaryLabelsAnUnlabeledEntity(t *testing.T) {
	out := &bytes.Buffer{}
	envelope := Envelope{Data: map[string]any{
		"from":       "orders",
		"resolution": map[string]any{"selected": map[string]any{"id": "workload:orders"}},
		"evidence":   map[string]any{"relationship_count": float64(0)},
	}}
	if err := RenderSummary(out, envelope); err != nil {
		t.Fatalf("RenderSummary() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "Resolved: Entity workload:orders\n") {
		t.Fatalf("summary missing the unlabeled fallback:\n%s", out.String())
	}
}

func TestRenderErrorListsAmbiguousCandidates(t *testing.T) {
	out := &bytes.Buffer{}
	if err := RenderError(out, Envelope{Data: sampleData("ambiguous")}); err != nil {
		t.Fatalf("RenderError() error = %v, want nil", err)
	}
	want := strings.Join([]string{
		"Map selector is ambiguous. Add --type, --repo, or --env.",
		"- workload:orders-api name=orders repo=repo-api",
		"- workload:orders-worker name=orders repo=repo-worker",
		"",
	}, "\n")
	if got := out.String(); got != want {
		t.Fatalf("ambiguous guidance =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderErrorStaysSilentForOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		envelope Envelope
	}{
		{"no data at all", Envelope{Error: &EnvelopeError{Code: "backend_unavailable"}}},
		{"mapped but stale", Envelope{Data: sampleData("mapped")}},
		{"no match", Envelope{Data: sampleData("no_match")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			if err := RenderError(out, tc.envelope); err != nil {
				t.Fatalf("RenderError() error = %v, want nil", err)
			}
			if out.Len() != 0 {
				t.Fatalf("RenderError() wrote %q, want nothing", out.String())
			}
		})
	}
}

func TestWriteSelectsTheOutputForm(t *testing.T) {
	for _, tc := range []struct {
		name       string
		jsonOutput bool
		envelope   Envelope
		failure    *Failure
		wantFirst  string
	}{
		{"text summary on success", false, Envelope{Data: sampleData("mapped")}, nil, "Map: terraform/aws_lb.main"},
		{
			"ambiguity guidance on failure", false,
			Envelope{Data: sampleData("ambiguous")},
			&Failure{Kind: FailureAmbiguous}, "Map selector is ambiguous. Add --type, --repo, or --env.",
		},
		{"json on success", true, Envelope{Data: sampleData("mapped")}, nil, "{"},
		{"json on failure", true, Envelope{Error: &EnvelopeError{Code: "not_found"}}, &Failure{Kind: FailureEnvelope}, "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			if err := Write(out, tc.jsonOutput, tc.envelope, tc.failure); err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}
			first, _, _ := strings.Cut(out.String(), "\n")
			if first != tc.wantFirst {
				t.Fatalf("first line = %q, want %q", first, tc.wantFirst)
			}
		})
	}
}

func TestWriteJSONKeepsSelectorCharactersLiteral(t *testing.T) {
	out := &bytes.Buffer{}
	envelope := Envelope{Data: map[string]any{"from": "workload:a&b<c>d"}}
	if err := WriteJSON(out, envelope); err != nil {
		t.Fatalf("WriteJSON() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), `"workload:a&b<c>d"`) {
		t.Fatalf("WriteJSON escaped selector characters:\n%s", out.String())
	}
	var round Envelope
	if err := json.Unmarshal(out.Bytes(), &round); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if round.Data["from"] != "workload:a&b<c>d" {
		t.Fatalf("round-tripped from = %#v, want the literal selector", round.Data["from"])
	}
}

// failingWriter fails after allowing n successful writes, so a renderer's
// error handling is exercised on the real production path rather than assumed.
type failingWriter struct {
	remaining int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errors.New("write failed")
	}
	w.remaining--
	return len(p), nil
}

func TestRenderersPropagateWriteErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		allow int
		run   func(w *failingWriter) error
	}{
		{"summary header", 0, func(w *failingWriter) error {
			return RenderSummary(w, Envelope{Data: sampleData("mapped")})
		}},
		{"summary section body", 4, func(w *failingWriter) error {
			return RenderSummary(w, Envelope{Data: sampleData("mapped")})
		}},
		{"ambiguity header", 0, func(w *failingWriter) error {
			return RenderError(w, Envelope{Data: sampleData("ambiguous")})
		}},
		{"ambiguity candidate", 2, func(w *failingWriter) error {
			return RenderError(w, Envelope{Data: sampleData("ambiguous")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(&failingWriter{remaining: tc.allow}); err == nil {
				t.Fatal("renderer error = nil, want the write failure")
			}
		})
	}
}

// TestWriteDropsRenderErrorOnTheFailingPath pins the deliberate discard in
// Write: the caller is about to report the classified failure, and replacing
// it with a write error would lose why the command failed.
func TestWriteDropsRenderErrorOnTheFailingPath(t *testing.T) {
	err := Write(&failingWriter{}, false, Envelope{Data: sampleData("ambiguous")}, &Failure{Kind: FailureAmbiguous})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil on the failing path", err)
	}
}

func TestValueReadersDegradeOnWrongTypes(t *testing.T) {
	parent := map[string]any{
		"object": map[string]any{"k": "v"},
		"list":   []any{"a", 2, "  b  ", ""},
		"typed":  []map[string]any{{"k": "v"}},
		"text":   "  spaced  ",
		"number": float64(7),
		"whole":  3,
		"wrong":  fmt.Errorf("not a json value"),
	}
	if got := mapField(parent, "object"); got["k"] != "v" {
		t.Fatalf("mapField(object) = %#v, want the nested object", got)
	}
	for _, key := range []string{"wrong", "missing"} {
		if got := mapField(parent, key); got != nil {
			t.Fatalf("mapField(%s) = %#v, want nil", key, got)
		}
		if got := sliceField(parent, key); got != nil {
			t.Fatalf("sliceField(%s) = %#v, want nil", key, got)
		}
		if got := stringField(parent, key); got != "" {
			t.Fatalf("stringField(%s) = %q, want empty", key, got)
		}
		if got := intField(parent, key); got != 0 {
			t.Fatalf("intField(%s) = %d, want 0", key, got)
		}
	}
	if got := mapField(nil, "object"); got != nil {
		t.Fatalf("mapField(nil) = %#v, want nil", got)
	}
	if got := sliceField(nil, "list"); got != nil {
		t.Fatalf("sliceField(nil) = %#v, want nil", got)
	}
	if got := stringField(nil, "text"); got != "" {
		t.Fatalf("stringField(nil) = %q, want empty", got)
	}
	if got := intField(nil, "number"); got != 0 {
		t.Fatalf("intField(nil) = %d, want 0", got)
	}
	if got := len(sliceField(parent, "typed")); got != 1 {
		t.Fatalf("sliceField(typed) length = %d, want 1", got)
	}
	if got := stringField(parent, "text"); got != "spaced" {
		t.Fatalf("stringField(text) = %q, want trimmed", got)
	}
	if got := intField(parent, "number"); got != 7 {
		t.Fatalf("intField(number) = %d, want 7", got)
	}
	if got := intField(parent, "whole"); got != 3 {
		t.Fatalf("intField(whole) = %d, want 3", got)
	}
	if got := stringList(parent["list"]); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("stringList(list) = %#v, want the trimmed non-empty strings", got)
	}
	if got := stringList([]string{"kept"}); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("stringList([]string) = %#v, want the slice unchanged", got)
	}
	if got := stringList("not a list"); got != nil {
		t.Fatalf("stringList(string) = %#v, want nil", got)
	}
	if got := firstNonEmpty("", "   ", " chosen ", "later"); got != "chosen" {
		t.Fatalf("firstNonEmpty() = %q, want chosen", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Fatalf("firstNonEmpty(all blank) = %q, want empty", got)
	}
}
