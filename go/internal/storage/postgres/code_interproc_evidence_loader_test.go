package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCodeInterprocEvidenceFromEnvelope proves the cross-function fact payload
// maps to a reducer input, including the JSONB float64 confidence and the bool
// cloud flag.
func TestCodeInterprocEvidenceFromEnvelope(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			"source_function_uid":  "func-source",
			"sink_function_uid":    "func-sink",
			"relative_path":        "src/handler.go",
			"source_function_name": "readRequest",
			"sink_function_name":   "execQuery",
			"language":             "go",
			"sink_kind":            "sql",
			"source_kind":          "http_request",
			"confidence":           0.7,
			"cloud":                true,
			"why_trail": []map[string]any{
				{"role": "source", "function_uid": "func-source"},
				{"role": "sink", "function_uid": "func-sink"},
			},
			"why_trail_truncated": true,
		},
	}

	got := codeInterprocEvidenceFromEnvelope(envelope)
	if got.SourceFunctionUID != "func-source" || got.SinkFunctionUID != "func-sink" || got.RelativePath != "src/handler.go" {
		t.Fatalf("identity fields not mapped: %+v", got)
	}
	if got.SourceFunctionName != "readRequest" || got.SinkFunctionName != "execQuery" || got.Language != "go" {
		t.Fatalf("name/language fields not mapped: %+v", got)
	}
	if got.SinkKind != "sql" || got.SourceKind != "http_request" {
		t.Fatalf("kind fields not mapped: %+v", got)
	}
	if got.Confidence != 0.7 || got.Cloud != true {
		t.Fatalf("confidence/cloud not mapped: %+v", got)
	}
	if len(got.WhyTrail) != 2 || got.WhyTrail[0]["function_uid"] != "func-source" {
		t.Fatalf("why trail not mapped: %+v", got.WhyTrail)
	}
	if !got.WhyTrailTruncated {
		t.Fatalf("WhyTrailTruncated = false, want true")
	}
}

// TestCodeInterprocEvidenceFromEnvelopeDefaultsCloudAbsent proves the cloud flag
// defaults to false when the payload omits it (the collector only sets it when
// true).
func TestCodeInterprocEvidenceFromEnvelopeDefaultsCloudAbsent(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.CodeInterprocEvidenceFactKind,
		Payload: map[string]any{
			"source_function_uid": "func-source",
			"sink_function_uid":   "func-sink",
		},
	}
	if got := codeInterprocEvidenceFromEnvelope(envelope); got.Cloud {
		t.Fatalf("cloud must default to false when absent, got %+v", got)
	}
}
