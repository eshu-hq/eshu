// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeDocumentationSectionMatchesLegacyJSONShape(t *testing.T) {
	t.Parallel()

	payload := documentationSectionEncodeFixture()
	got, err := EncodeDocumentationSection(payload)
	if err != nil {
		t.Fatalf("EncodeDocumentationSection() error = %v, want nil", err)
	}
	want := mustJSONPayloadMap(t, payload)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeDocumentationSection() = %#v, want legacy JSON shape %#v", got, want)
	}
}

func TestEncodeSemanticDocumentationObservationMatchesLegacyJSONShape(t *testing.T) {
	t.Parallel()

	payload := semanticDocumentationObservationFixture()
	got, err := EncodeSemanticDocumentationObservation(payload)
	if err != nil {
		t.Fatalf("EncodeSemanticDocumentationObservation() error = %v, want nil", err)
	}
	want := mustJSONPayloadMap(t, payload)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeSemanticDocumentationObservation() = %#v, want legacy JSON shape %#v", got, want)
	}
}

func BenchmarkDocumentationSectionEncodeNoRegression(b *testing.B) {
	payload := documentationSectionEncodeFixture()
	b.Run("legacy_json_roundtrip", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := jsonPayloadMap(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("factschema_direct_bridge", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := EncodeDocumentationSection(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSemanticDocumentationObservationEncodeNoRegression(b *testing.B) {
	payload := semanticDocumentationObservationFixture()
	b.Run("legacy_json_roundtrip", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := jsonPayloadMap(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("factschema_direct_bridge", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := EncodeSemanticDocumentationObservation(payload); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func documentationSectionEncodeFixture() DocumentationSectionPayload {
	return DocumentationSectionPayload{
		DocumentID:       "doc:confluence:12345",
		RevisionID:       "17",
		SectionID:        "section:deployment",
		ParentSectionID:  "section:overview",
		SectionAnchor:    "deployment",
		HeadingText:      "Deployment",
		OrdinalPath:      []int{2, 1},
		Content:          "<h2>Deployment</h2><p>Ship payment service with the Helm release.</p>",
		ContentFormat:    "storage",
		TextHash:         "sha256:section-text",
		ExcerptHash:      "sha256:bounded-excerpt",
		SourceStartRef:   "block:10",
		SourceEndRef:     "block:12",
		SourceMetadata:   map[string]string{"space_key": "PLAT", "page_id": "12345"},
		ContainsWarnings: true,
	}
}

func mustJSONPayloadMap(t *testing.T, payload any) map[string]any {
	t.Helper()
	out, err := jsonPayloadMap(payload)
	if err != nil {
		t.Fatalf("jsonPayloadMap() error = %v, want nil", err)
	}
	return out
}

func jsonPayloadMap(payload any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}
