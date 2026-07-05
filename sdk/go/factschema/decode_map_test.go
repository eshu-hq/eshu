// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"
)

// TestAnyToStringMap_CoercesJSONBNativeShape proves the map[string]string fast
// path (assignField's Map and Ptr-to-Map cases) accepts the two shapes the
// JSONB decode path actually produces — a string-valued map[string]any (the
// encoding/json default for a JSONB object) and an already-typed
// map[string]string — and rejects a map carrying a non-string value so the
// caller falls back to the marshal/unmarshal path rather than silently
// dropping or coercing a value.
//
// This is the accuracy/equivalence proof for the kubernetes_live wave's
// No-Regression Evidence: the fast path must produce byte-identical output to
// the jsonRoundTripValue fallback for every value the fast path accepts.
func TestAnyToStringMap_CoercesJSONBNativeShape(t *testing.T) {
	t.Parallel()

	t.Run("map[string]any with string values", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{"app": "checkout", "team": "payments"}
		got, ok := anyToStringMap(raw)
		if !ok {
			t.Fatal("ok = false, want true for a string-valued map[string]any")
		}
		want := map[string]string{"app": "checkout", "team": "payments"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("anyToStringMap() = %v, want %v", got, want)
		}
	})

	t.Run("already-typed map[string]string", func(t *testing.T) {
		t.Parallel()
		raw := map[string]string{"app": "checkout"}
		got, ok := anyToStringMap(raw)
		if !ok {
			t.Fatal("ok = false, want true for an already-typed map[string]string")
		}
		if !reflect.DeepEqual(got, raw) {
			t.Fatalf("anyToStringMap() = %v, want %v", got, raw)
		}
	})

	t.Run("non-string value falls back", func(t *testing.T) {
		t.Parallel()
		raw := map[string]any{"app": "checkout", "replicas": float64(3)}
		_, ok := anyToStringMap(raw)
		if ok {
			t.Fatal("ok = true, want false for a map carrying a non-string value (caller must fall back to jsonRoundTripValue)")
		}
	})

	t.Run("non-map value falls back", func(t *testing.T) {
		t.Parallel()
		_, ok := anyToStringMap("not-a-map")
		if ok {
			t.Fatal("ok = true, want false for a non-map input")
		}
	})

	t.Run("empty map decodes to empty, non-nil map", func(t *testing.T) {
		t.Parallel()
		got, ok := anyToStringMap(map[string]any{})
		if !ok {
			t.Fatal("ok = false, want true for an empty map[string]any")
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("anyToStringMap() = %#v, want a non-nil empty map", got)
		}
	})
}

// TestDecodeMapInto_MapStringStringFastPath proves decodeMapInto's
// map[string]string fast path decodes a pod-template-shaped selector field
// correctly through the full decodeMapInto entry point, not just the
// anyToStringMap helper in isolation — the same production seam
// kubernetesWorkloadNodeRow calls.
func TestDecodeMapInto_MapStringStringFastPath(t *testing.T) {
	t.Parallel()

	type selectorHolder struct {
		Selector map[string]string `json:"selector,omitempty"`
	}

	payload := map[string]any{
		"selector": map[string]any{"app": "checkout", "tier": "backend"},
	}

	var out selectorHolder
	if err := decodeMapInto(payload, &out); err != nil {
		t.Fatalf("decodeMapInto() error = %v, want nil", err)
	}
	want := map[string]string{"app": "checkout", "tier": "backend"}
	if !reflect.DeepEqual(out.Selector, want) {
		t.Fatalf("Selector = %v, want %v", out.Selector, want)
	}
}

// TestDecodeMapInto_Float64FastPath proves decodeMapInto's float64/*float64
// fast path (added for the vulnerability_intelligence wave's
// vulnerability.cve CVSSScore field) decodes a JSONB-native float64 value
// directly via type assertion, both for a plain float64 field and a *float64
// field, rather than falling through to the marshal-free reflection decoder's
// jsonRoundTripValue path — the same gap the kubernetes_live wave found and
// fixed for map[string]string. A non-numeric value must still fall back to
// jsonRoundTripValue (which itself fails closed) rather than silently
// coercing or zeroing the field.
func TestDecodeMapInto_Float64FastPath(t *testing.T) {
	t.Parallel()

	t.Run("plain float64 field", func(t *testing.T) {
		t.Parallel()
		type scoreHolder struct {
			Score float64 `json:"score"`
		}
		var out scoreHolder
		if err := decodeMapInto(map[string]any{"score": 7.5}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Score != 7.5 {
			t.Fatalf("Score = %v, want 7.5", out.Score)
		}
	})

	t.Run("optional *float64 field present", func(t *testing.T) {
		t.Parallel()
		type scoreHolder struct {
			Score *float64 `json:"score,omitempty"`
		}
		var out scoreHolder
		if err := decodeMapInto(map[string]any{"score": 9.8}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Score == nil || *out.Score != 9.8 {
			t.Fatalf("Score = %v, want *9.8", out.Score)
		}
	})

	t.Run("optional *float64 field absent stays nil", func(t *testing.T) {
		t.Parallel()
		type scoreHolder struct {
			Score *float64 `json:"score,omitempty"`
		}
		var out scoreHolder
		if err := decodeMapInto(map[string]any{}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Score != nil {
			t.Fatalf("Score = %v, want nil for an absent key", out.Score)
		}
	})

	t.Run("non-numeric value fails closed, not silently zeroed", func(t *testing.T) {
		t.Parallel()
		type scoreHolder struct {
			Score *float64 `json:"score,omitempty"`
		}
		var out scoreHolder
		err := decodeMapInto(map[string]any{"score": "not-a-number"}, &out)
		if err == nil {
			t.Fatal("decodeMapInto() error = nil, want an error for a non-numeric score value")
		}
	})
}
