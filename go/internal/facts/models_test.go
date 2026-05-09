package facts

import (
	"testing"
	"time"
)

func TestFactEnvelopeScopeGenerationKey(t *testing.T) {
	t.Parallel()

	ref := Ref{
		SourceSystem:   "git",
		ScopeID:        "scope-123",
		GenerationID:   "generation-456",
		FactKey:        "fact-key",
		SourceURI:      "file:///repo/path",
		SourceRecordID: "record-789",
	}

	if got, want := ref.ScopeGenerationKey(), "scope-123:generation-456"; got != want {
		t.Fatalf("Ref.ScopeGenerationKey() = %q, want %q", got, want)
	}

	envelope := Envelope{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"name": "eshu"},
		IsTombstone:   false,
		SourceRef:     ref,
	}

	if got, want := envelope.ScopeGenerationKey(), "scope-123:generation-456"; got != want {
		t.Fatalf("Envelope.ScopeGenerationKey() = %q, want %q", got, want)
	}
}

func TestFactEnvelopeCloneIsCopySafe(t *testing.T) {
	t.Parallel()

	original := Envelope{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"name":   "original",
			"nested": map[string]any{"count": 1},
		},
		SourceRef: Ref{
			SourceSystem:   "git",
			ScopeID:        "scope-123",
			GenerationID:   "generation-456",
			FactKey:        "fact-key",
			SourceURI:      "file:///repo/path",
			SourceRecordID: "record-789",
		},
	}

	cloned := original.Clone()
	cloned.Payload["name"] = "changed"
	cloned.Payload["nested"].(map[string]any)["count"] = 2
	cloned.SourceRef.FactKey = "changed"

	if got := original.Payload["name"]; got != "original" {
		t.Fatalf("original.Payload[name] = %v, want %v", got, "original")
	}

	nested := original.Payload["nested"].(map[string]any)
	if got := nested["count"]; got != 1 {
		t.Fatalf("original.Payload[nested][count] = %v, want %v", got, 1)
	}

	if got, want := original.SourceRef.FactKey, "fact-key"; got != want {
		t.Fatalf("original.SourceRef.FactKey = %q, want %q", got, want)
	}
}

func TestFactEnvelopeClonePreservesDurableCollectorFields(t *testing.T) {
	t.Parallel()

	original := Envelope{
		FactID:           "fact-1",
		ScopeID:          "scope-123",
		GenerationID:     "generation-456",
		FactKind:         "terraform_state_resource",
		StableFactKey:    "terraform_state_resource:aws_instance.app",
		SchemaVersion:    "1.0.0",
		CollectorKind:    "terraform_state",
		FencingToken:     42,
		SourceConfidence: "observed",
		ObservedAt:       time.Date(2026, time.May, 9, 9, 0, 0, 0, time.UTC),
		SourceRef: Ref{
			SourceSystem: "terraform_state",
			ScopeID:      "scope-123",
			GenerationID: "generation-456",
			FactKey:      "aws_instance.app",
		},
	}

	cloned := original.Clone()

	if cloned.SchemaVersion != original.SchemaVersion {
		t.Fatalf("Clone().SchemaVersion = %q, want %q", cloned.SchemaVersion, original.SchemaVersion)
	}
	if cloned.CollectorKind != original.CollectorKind {
		t.Fatalf("Clone().CollectorKind = %q, want %q", cloned.CollectorKind, original.CollectorKind)
	}
	if cloned.FencingToken != original.FencingToken {
		t.Fatalf("Clone().FencingToken = %d, want %d", cloned.FencingToken, original.FencingToken)
	}
	if cloned.SourceConfidence != original.SourceConfidence {
		t.Fatalf("Clone().SourceConfidence = %q, want %q", cloned.SourceConfidence, original.SourceConfidence)
	}
}
