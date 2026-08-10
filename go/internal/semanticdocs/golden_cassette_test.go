// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticdocs

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestGoldenCassetteMatchesEmitterAndReplays(t *testing.T) {
	t.Parallel()

	expected := goldenDocumentationObservationEnvelope(t)
	cassettePath := filepath.Join(
		"..", "..", "..", "testdata", "cassettes", "semanticextraction", "supply-chain-demo.json",
	)
	source, err := cassette.NewSource(cassettePath)
	if err != nil {
		t.Fatalf("cassette.NewSource(%q) error = %v, want nil", cassettePath, err)
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Source.Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Source.Next() ok = false, want one replayed scope")
	}

	var replayed []facts.Envelope
	for envelope := range collected.Facts {
		replayed = append(replayed, envelope)
	}
	if got, want := len(replayed), 1; got != want {
		t.Fatalf("replayed fact count = %d, want %d", got, want)
	}
	actual := replayed[0]

	for field, values := range map[string][2]string{
		"fact kind":       {actual.FactKind, expected.FactKind},
		"schema version":  {actual.SchemaVersion, expected.SchemaVersion},
		"collector kind":  {actual.CollectorKind, expected.CollectorKind},
		"stable fact key": {actual.StableFactKey, expected.StableFactKey},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	assertJSONEquivalent(t, actual.Payload, expected.Payload)

	decoded, err := factschema.DecodeSemanticDocumentationObservation(factschema.Envelope{
		FactKind:         actual.FactKind,
		SchemaVersion:    actual.SchemaVersion,
		StableFactKey:    actual.StableFactKey,
		ScopeID:          actual.ScopeID,
		GenerationID:     actual.GenerationID,
		CollectorKind:    actual.CollectorKind,
		SourceConfidence: actual.SourceConfidence,
		ObservedAt:       actual.ObservedAt,
		IsTombstone:      actual.IsTombstone,
		Payload:          actual.Payload,
	})
	if err != nil {
		t.Fatalf("DecodeSemanticDocumentationObservation() error = %v, want nil", err)
	}
	if got, want := decoded.ObservationType, "runtime_readiness_summary"; got != want {
		t.Fatalf("decoded ObservationType = %q, want %q", got, want)
	}
}

func goldenDocumentationObservationEnvelope(t *testing.T) facts.Envelope {
	t.Helper()

	observedAt := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-default",
			ProviderKind:      ProviderKindMock,
			ModelID:           "mock-semantic-docs-v1",
		},
		ObservedAt: func() time.Time { return observedAt },
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	envelopes, err := emitter.Emit(context.Background(), doctruth.SectionInput{
		ScopeID:      "semantic-extraction:runtime-readiness",
		GenerationID: "semantic-extraction-generation:runtime-readiness",
		SourceSystem: "semantic_extraction",
		DocumentID:   "documentation-document:runtime-readiness",
		RevisionID:   "sha256:d52ad31d38be8eddd6cb6154c4e22bd2eeafa14e21d3d0cc87ebf3ba75c9433c",
		SectionID:    "documentation-section:runtime-readiness",
		CanonicalURI: "https://docs.example.invalid/runtime-readiness#summary",
		ExcerptHash:  "sha256:4dc0b1c0290e09f963155932ea25afc8243f805a2f194c6d32846d6c5c7feffe",
		Text:         "Runtime readiness requires deployed validation evidence.",
		ObservedAt:   observedAt,
	}, []MockObservation{{
		ObservationType:     "runtime_readiness_summary",
		ObservationText:     "Runtime readiness requires deployed validation evidence.",
		Confidence:          facts.SemanticConfidenceMedium,
		ConfidenceRationale: "bounded fixture section states the evidence requirement",
	}})
	if err != nil {
		t.Fatalf("Emitter.Emit() error = %v, want nil", err)
	}
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("emitted fact count = %d, want %d", got, want)
	}
	return envelopes[0]
}

func assertJSONEquivalent(t *testing.T, got, want any) {
	t.Helper()

	normalize := func(value any) any {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("json.Marshal(%T) error = %v", value, err)
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(%T) error = %v", value, err)
		}
		return decoded
	}
	if normalizedGot, normalizedWant := normalize(got), normalize(want); !reflect.DeepEqual(normalizedGot, normalizedWant) {
		gotJSON, _ := json.MarshalIndent(normalizedGot, "", "  ")
		wantJSON, _ := json.MarshalIndent(normalizedWant, "", "  ")
		t.Fatalf("payload JSON differs\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
}
