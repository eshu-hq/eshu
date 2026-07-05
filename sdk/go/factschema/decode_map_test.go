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

// TestDecodeMapInto_IntFastPath locks the *int fast path's accept/reject
// contract (Wave 4d, secrets_iam VAULT/K8S lanes: VaultACLPolicyRule.PathDepth
// and VaultKVMetadata.PathDepth were the first *int-shaped payload fields any
// migrated family decoded through this seam): a present *int field and an
// absent *int field (stays nil) both decode correctly, while a non-numeric
// value fails closed with an error rather than silently zeroing the field —
// mirroring TestDecodeMapInto_Float64FastPath's contract for the *float64 fast
// path Wave 4c added.
func TestDecodeMapInto_IntFastPath(t *testing.T) {
	t.Parallel()

	t.Run("plain int field", func(t *testing.T) {
		t.Parallel()
		type depthHolder struct {
			Depth int `json:"depth"`
		}
		var out depthHolder
		if err := decodeMapInto(map[string]any{"depth": float64(3)}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Depth != 3 {
			t.Fatalf("Depth = %v, want 3", out.Depth)
		}
	})

	t.Run("optional *int field present", func(t *testing.T) {
		t.Parallel()
		type depthHolder struct {
			Depth *int `json:"depth,omitempty"`
		}
		var out depthHolder
		if err := decodeMapInto(map[string]any{"depth": float64(4)}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Depth == nil || *out.Depth != 4 {
			t.Fatalf("Depth = %v, want *4", out.Depth)
		}
	})

	t.Run("optional *int field absent stays nil", func(t *testing.T) {
		t.Parallel()
		type depthHolder struct {
			Depth *int `json:"depth,omitempty"`
		}
		var out depthHolder
		if err := decodeMapInto(map[string]any{}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.Depth != nil {
			t.Fatalf("Depth = %v, want nil for an absent key", out.Depth)
		}
	})

	t.Run("non-integral float fails closed, not silently truncated", func(t *testing.T) {
		t.Parallel()
		type depthHolder struct {
			Depth *int `json:"depth,omitempty"`
		}
		var out depthHolder
		err := decodeMapInto(map[string]any{"depth": 3.5}, &out)
		if err == nil {
			t.Fatal("decodeMapInto() error = nil, want an error for a non-integral depth value")
		}
	})

	t.Run("out-of-range integral float fails closed, not silently wrapped", func(t *testing.T) {
		t.Parallel()
		// An integral float64 that exceeds int64 range (math.MaxInt64 is not
		// exactly representable as float64, so 1e300 is a value encoding/json
		// would hand jsonNumberToInt as a valid-looking whole number). A bare
		// int(n) cast on a value like this is implementation-defined and would
		// silently wrap/truncate a path_depth into a wrong small or negative
		// number instead of dead-lettering the fact as input_invalid.
		type depthHolder struct {
			Depth *int `json:"depth,omitempty"`
		}
		var out depthHolder
		err := decodeMapInto(map[string]any{"depth": 1e300}, &out)
		if err == nil {
			t.Fatalf("decodeMapInto() error = nil, want an error for an out-of-range depth value; got Depth = %v", out.Depth)
		}
	})

	t.Run("non-numeric value fails closed, not silently zeroed", func(t *testing.T) {
		t.Parallel()
		type depthHolder struct {
			Depth *int `json:"depth,omitempty"`
		}
		var out depthHolder
		err := decodeMapInto(map[string]any{"depth": "not-a-number"}, &out)
		if err == nil {
			t.Fatal("decodeMapInto() error = nil, want an error for a non-numeric depth value")
		}
	})
}

// TestDecodeMapInto_StringMapSliceFastPath proves decodeMapInto's
// []map[string]string fast path (added for the security_alert wave's
// security_alert.repository_alert CWEs field, and shared with gcp's
// IAMPolicyObservation.Members) decodes a JSONB-native []any of string-valued
// object maps directly via type assertion, rather than falling through to the
// jsonRoundTripValue marshal/unmarshal path — the same gap class the
// kubernetes_live (map[string]string) and vulnerability (float64) waves closed.
// A slice whose elements are not string-valued object maps must still fall back
// to jsonRoundTripValue (which fails closed) rather than silently coercing.
func TestDecodeMapInto_StringMapSliceFastPath(t *testing.T) {
	t.Parallel()

	type cweHolder struct {
		CWEs []map[string]string `json:"cwes,omitempty"`
	}

	t.Run("JSONB-native []any of object maps", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{
			"cwes": []any{
				map[string]any{"cwe_id": "CWE-400", "name": "Uncontrolled Resource Consumption"},
				map[string]any{"cwe_id": "CWE-770", "name": "Allocation Without Limits"},
			},
		}
		var out cweHolder
		if err := decodeMapInto(payload, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		want := []map[string]string{
			{"cwe_id": "CWE-400", "name": "Uncontrolled Resource Consumption"},
			{"cwe_id": "CWE-770", "name": "Allocation Without Limits"},
		}
		if !reflect.DeepEqual(out.CWEs, want) {
			t.Fatalf("CWEs = %v, want %v", out.CWEs, want)
		}
	})

	t.Run("already-typed []map[string]string", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{
			"cwes": []map[string]string{{"cwe_id": "CWE-79"}},
		}
		var out cweHolder
		if err := decodeMapInto(payload, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		want := []map[string]string{{"cwe_id": "CWE-79"}}
		if !reflect.DeepEqual(out.CWEs, want) {
			t.Fatalf("CWEs = %v, want %v", out.CWEs, want)
		}
	})

	t.Run("empty slice stays empty non-nil", func(t *testing.T) {
		t.Parallel()
		var out cweHolder
		if err := decodeMapInto(map[string]any{"cwes": []any{}}, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		if out.CWEs == nil || len(out.CWEs) != 0 {
			t.Fatalf("CWEs = %v, want empty non-nil slice", out.CWEs)
		}
	})

	t.Run("non-string-valued object map falls back and fails closed", func(t *testing.T) {
		t.Parallel()
		payload := map[string]any{
			"cwes": []any{map[string]any{"cwe_id": 400}},
		}
		var out cweHolder
		// A non-string value can't coerce to map[string]string; the fast path
		// returns ok=false and jsonRoundTripValue takes over, which unmarshals
		// the JSON number 400 into the string field's slot — encoding/json
		// rejects that with a type error rather than silently coercing.
		if err := decodeMapInto(payload, &out); err == nil {
			t.Fatal("decodeMapInto() error = nil, want an error for a non-string CWE value")
		}
	})
}

// TestDecodeMapInto_TypedMapInputNotMutated proves the decode-map fast paths
// return a fresh, non-aliasing copy for already-typed map[string]string and
// []map[string]string inputs, so a downstream consumer that normalizes the
// decoded value in place (as the security_alert reducer does for epss/cwes)
// cannot mutate the caller's original env.Payload maps. This is the Copilot
// decode_map.go:345 / :358 and codex :249 finding: the JSONB path always
// allocates, but an in-memory caller supplying typed maps would otherwise be
// aliased.
func TestDecodeMapInto_TypedMapInputNotMutated(t *testing.T) {
	t.Parallel()

	t.Run("map[string]string input is copied", func(t *testing.T) {
		t.Parallel()
		type holder struct {
			EPSS map[string]string `json:"epss,omitempty"`
		}
		original := map[string]string{"percentage": "0.0123", "  padded  ": "  x  "}
		payload := map[string]any{"epss": original}

		var out holder
		if err := decodeMapInto(payload, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		// Mutating the decoded map must not touch the original payload map.
		for key := range out.EPSS {
			delete(out.EPSS, key)
		}
		out.EPSS["mutated"] = "yes"
		if _, aliased := original["mutated"]; aliased {
			t.Fatal("decoded map aliases the original payload map; mutation leaked back")
		}
		if len(original) != 2 {
			t.Fatalf("original payload map was mutated: len = %d, want 2", len(original))
		}
	})

	t.Run("[]map[string]string input is copied", func(t *testing.T) {
		t.Parallel()
		type holder struct {
			CWEs []map[string]string `json:"cwes,omitempty"`
		}
		originalElem := map[string]string{"cwe_id": "CWE-400", "name": "DoS"}
		original := []map[string]string{originalElem}
		payload := map[string]any{"cwes": original}

		var out holder
		if err := decodeMapInto(payload, &out); err != nil {
			t.Fatalf("decodeMapInto() error = %v, want nil", err)
		}
		// Mutating a decoded element map must not touch the original element.
		for key := range out.CWEs[0] {
			delete(out.CWEs[0], key)
		}
		out.CWEs[0]["mutated"] = "yes"
		if _, aliased := originalElem["mutated"]; aliased {
			t.Fatal("decoded slice element aliases the original payload element map; mutation leaked back")
		}
		if len(originalElem) != 2 {
			t.Fatalf("original element map was mutated: len = %d, want 2", len(originalElem))
		}
	})
}
