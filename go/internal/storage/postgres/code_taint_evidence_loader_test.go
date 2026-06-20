package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeTaintEvidenceFromEnvelope proves the fact payload maps to a reducer
// input, including the JSONB float64-to-int coercion for line numbers.
func TestCodeTaintEvidenceFromEnvelope(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeTaintEvidenceFactKind,
		Payload: map[string]any{
			"function_uid":  "func-handle",
			"function_name": "handle",
			"relative_path": "src/handler.go",
			"language":      "go",
			"kind":          "TAINTED",
			"sink_kind":     "sql",
			"source_kind":   "http_request",
			"binding":       "q",
			// JSONB numeric scan yields float64.
			"source_line":   float64(4),
			"sink_line":     float64(5),
			"confidence":    0.8,
			"class_context": "Repo",
			"sink_label":    "Query",
			"guard_reason":  "allowed",
		},
	}

	got := codeTaintEvidenceFromEnvelope(envelope)
	if got.FunctionUID != "func-handle" || got.FunctionName != "handle" || got.RelativePath != "src/handler.go" {
		t.Fatalf("identity fields not mapped: %+v", got)
	}
	if got.Kind != "TAINTED" || got.SinkKind != "sql" || got.SourceKind != "http_request" || got.Binding != "q" {
		t.Fatalf("finding fields not mapped: %+v", got)
	}
	if got.SourceLine != 4 || got.SinkLine != 5 {
		t.Fatalf("line numbers not coerced from float64: %+v", got)
	}
	if got.Confidence != 0.8 || got.ClassContext != "Repo" || got.SinkLabel != "Query" || got.GuardReason != "allowed" {
		t.Fatalf("provenance fields not mapped: %+v", got)
	}
}

// TestCodeTaintPayloadIntHandlesNumericTypes proves the int coercion accepts the
// types a payload can carry (float64 from JSONB, native int).
func TestCodeTaintPayloadIntHandlesNumericTypes(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"float64": {"n": float64(7)},
		"int":     {"n": 7},
		"int64":   {"n": int64(7)},
		"missing": {},
	}
	want := map[string]int{"float64": 7, "int": 7, "int64": 7, "missing": 0}
	for name, payload := range cases {
		if got := codeTaintPayloadInt(payload, "n"); got != want[name] {
			t.Fatalf("codeTaintPayloadInt(%s) = %d, want %d", name, got, want[name])
		}
	}
}
