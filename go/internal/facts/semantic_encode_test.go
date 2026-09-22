// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"encoding/json"
	"reflect"
	"testing"
)

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
