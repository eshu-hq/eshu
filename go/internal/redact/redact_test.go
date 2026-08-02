// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("deployment-redaction-pepper"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	return key
}

func TestNewKeyRejectsBlankMaterial(t *testing.T) {
	t.Parallel()

	if _, err := redact.NewKey([]byte(" ")); err == nil {
		t.Fatal("NewKey() error = nil, want non-nil")
	}
}

func TestKeyIsZeroReportsMissingMaterial(t *testing.T) {
	t.Parallel()

	var empty redact.Key
	if !empty.IsZero() {
		t.Fatal("empty Key IsZero() = false, want true")
	}
	if testKey(t).IsZero() {
		t.Fatal("configured Key IsZero() = true, want false")
	}
}

func TestStringReturnsDeterministicMarkerWithEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		reason     string
		source     string
		wantReason string
		wantSource string
	}{
		{
			name:       "sensitive output",
			raw:        "super-secret",
			reason:     "sensitive_output",
			source:     "terraform_state_output.db_password",
			wantReason: "sensitive_output",
			wantSource: "terraform_state_output.db_password",
		},
		{
			name:       "empty value fails closed",
			raw:        "",
			reason:     "unknown_sensitive_value",
			source:     "terraform_state_attribute.token",
			wantReason: "unknown_sensitive_value",
			wantSource: "terraform_state_attribute.token",
		},
		{
			name:       "blank evidence normalizes",
			raw:        "secret",
			reason:     " ",
			source:     "",
			wantReason: "unknown",
			wantSource: "unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			key := testKey(t)
			first := redact.String(test.raw, test.reason, test.source, key)
			second := redact.String(test.raw, test.reason, test.source, key)

			if first != second {
				t.Fatalf("String() = %#v, want deterministic %#v", second, first)
			}
			if first.Marker == "" {
				t.Fatalf("String().Marker is empty")
			}
			if strings.Contains(first.Marker, test.raw) && test.raw != "" {
				t.Fatalf("String().Marker = %q, leaked raw value", first.Marker)
			}
			if strings.Contains(first.Marker, test.wantSource) {
				t.Fatalf("String().Marker = %q, leaked source context", first.Marker)
			}
			if got := first.Reason; got != test.wantReason {
				t.Fatalf("String().Reason = %q, want %q", got, test.wantReason)
			}
			if got := first.Source; got != test.wantSource {
				t.Fatalf("String().Source = %q, want %q", got, test.wantSource)
			}
		})
	}
}

func TestRedactedMarkerDoesNotLeakRawValue(t *testing.T) {
	t.Parallel()

	redacted := redact.String("super-secret", "known_sensitive_key", "aws_db_instance.password", testKey(t))

	if strings.Contains(redacted.Marker, "super-secret") {
		t.Fatalf("String().Marker = %q, leaked raw value", redacted.Marker)
	}
	if got, wantPrefix := redacted.Marker, "redacted:hmac-sha256:"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("String().Marker = %q, want prefix %q", got, wantPrefix)
	}
}

func TestMarkerIncludesReasonAndSourceInDigest(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	base := redact.String("same-secret", "sensitive_output", "terraform_state_output.api_token", key)
	otherReason := redact.String("same-secret", "known_sensitive_key", "terraform_state_output.api_token", key)
	otherSource := redact.String("same-secret", "sensitive_output", "terraform_state_output.db_password", key)

	if base.Marker == otherReason.Marker {
		t.Fatalf("String() marker did not change when reason changed: %q", base.Marker)
	}
	if base.Marker == otherSource.Marker {
		t.Fatalf("String() marker did not change when source changed: %q", base.Marker)
	}
}

func TestMarkerIncludesKeyInDigest(t *testing.T) {
	t.Parallel()

	firstKey, err := redact.NewKey([]byte("deployment-redaction-pepper-a"))
	if err != nil {
		t.Fatalf("NewKey(first) error = %v, want nil", err)
	}
	secondKey, err := redact.NewKey([]byte("deployment-redaction-pepper-b"))
	if err != nil {
		t.Fatalf("NewKey(second) error = %v, want nil", err)
	}

	first := redact.String("same-secret", "known_sensitive_key", "aws_instance.secret", firstKey)
	second := redact.String("same-secret", "known_sensitive_key", "aws_instance.secret", secondKey)

	if first.Marker == second.Marker {
		t.Fatalf("String() marker did not change when key changed: %q", first.Marker)
	}
}

func TestBytesAndScalarUseSameCanonicalBytes(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	fromString := redact.String("42", "known_sensitive_key", "aws_instance.secret", key)
	fromBytes := redact.Bytes([]byte("42"), "known_sensitive_key", "aws_instance.secret", key)
	fromScalar := redact.Scalar(42, "known_sensitive_key", "aws_instance.secret", key)

	if fromString.Marker != fromBytes.Marker {
		t.Fatalf("Bytes().Marker = %q, want String().Marker %q", fromBytes.Marker, fromString.Marker)
	}
	if fromScalar.Marker != fromString.Marker {
		t.Fatalf("Scalar().Marker = %q, want String().Marker %q", fromScalar.Marker, fromString.Marker)
	}
}

func TestScalarJSONNumberUsesNumberBytes(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	fromNumber := redact.Scalar(json.Number("42"), "known_sensitive_key", "aws_instance.secret", key)
	fromString := redact.String("42", "known_sensitive_key", "aws_instance.secret", key)
	otherNumber := redact.Scalar(json.Number("43"), "known_sensitive_key", "aws_instance.secret", key)

	if fromNumber.Marker != fromString.Marker {
		t.Fatalf("Scalar(json.Number).Marker = %q, want String marker %q", fromNumber.Marker, fromString.Marker)
	}
	if fromNumber.Marker == otherNumber.Marker {
		t.Fatalf("Scalar(json.Number) marker did not change with number value: %q", fromNumber.Marker)
	}
}

func TestScalarDoesNotLeakUnsupportedValues(t *testing.T) {
	t.Parallel()

	raw := struct {
		Secret string
	}{Secret: "do-not-serialize"}

	redacted := redact.Scalar(raw, "unknown_sensitive_value", "unknown_provider_schema", testKey(t))

	if redacted.Marker == "" {
		t.Fatalf("Scalar() unsupported value returned empty marker")
	}
	if strings.Contains(redacted.Marker, raw.Secret) {
		t.Fatalf("Scalar().Marker = %q, leaked unsupported raw value", redacted.Marker)
	}
}

// jsonRoundTripAny encodes v to JSON and decodes it back into `any`, the
// same path a Postgres-stored fact payload takes before a decoder reads its
// attributes generically. This is what turns a redact.Value struct into a
// map[string]any in the first place.
func jsonRoundTripAny(t *testing.T, v any) any {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%#v) error = %v, want nil", v, err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v, want nil", encoded, err)
	}
	return decoded
}

// TestIsRedactedValueRecognizesJSONRoundTrippedMarker is the #5859
// regression: a collector-produced redact.Value survives storage only as a
// generic map after a JSON round-trip (the typed struct is gone), and a
// downstream decoder reading attributes as `any` must still recognize it as
// redacted rather than treating it as comparable data.
func TestIsRedactedValueRecognizesJSONRoundTrippedMarker(t *testing.T) {
	t.Parallel()

	value := redact.String("ami-0123456789abcdef0", "unknown_provider_schema", "resources.*.attributes.ami", testKey(t))
	decoded := jsonRoundTripAny(t, value)

	if !redact.IsRedactedValue(decoded) {
		t.Fatalf("IsRedactedValue(%#v) = false, want true for a JSON round-tripped redact.Value", decoded)
	}
}

// TestIsRedactedValueRejectsPlainMapWithoutMarkerPrefix proves the check is
// shape- AND prefix-specific: an ordinary map that happens to have a
// "marker" key with unrelated text must not be misclassified as redacted.
func TestIsRedactedValueRejectsPlainMapWithoutMarkerPrefix(t *testing.T) {
	t.Parallel()

	notAMarker := map[string]any{"marker": "some-unrelated-value", "reason": "n/a", "source": "n/a"}
	if redact.IsRedactedValue(notAMarker) {
		t.Fatalf("IsRedactedValue(%#v) = true, want false: no redact marker prefix present", notAMarker)
	}
}

// TestIsRedactedValueRejectsIncompleteMarkerShape proves the shape check
// requires the complete {marker,reason,source} object the package contract
// promises (AGENTS.md:52-55), not merely a "marker" field with the expected
// prefix. A map that carries only "marker" is not the JSON round-trip shape
// of a redact.Value -- it never came from String/Bytes/Scalar -- so treating
// it as redacted would be over-broad: callers either drop the whole object in
// flattenStateAttributes or suppress a resource's entire comparable
// attribute set, turning real map data into absent evidence and changing
// drift truth the same false-negative direction as the bug #5859 fixes.
// Each field is covered on its own, not only in combination. A single
// "marker alone" case cannot tell the two field checks apart: dropping either
// one leaves the other still rejecting that input, so the case passes while
// half the contract goes unenforced. Verified by mutation -- deleting the
// "reason" check alone left the earlier single-case version green.
func TestIsRedactedValueRejectsIncompleteMarkerShape(t *testing.T) {
	t.Parallel()

	validMarker := "redacted:hmac-sha256:" + strings.Repeat("0", 64)
	for name, incomplete := range map[string]map[string]any{
		"missing reason only": {
			"marker": validMarker,
			"source": "resources.*.attributes.ami",
		},
		"missing source only": {
			"marker": validMarker,
			"reason": "unknown_provider_schema",
		},
		"missing both": {
			"marker": validMarker,
		},
	} {
		if redact.IsRedactedValue(incomplete) {
			t.Fatalf("IsRedactedValue(%s = %#v) = true, want false: an incomplete shape is not a round-tripped redact.Value",
				name, incomplete)
		}
	}
}

// TestIsRedactedValueRejectsNonMarkerShapes proves ordinary scalar and
// composite values used elsewhere in decoded attributes never trip the
// marker check, so IsRedactedValue is safe to call unconditionally on any
// decoded JSON leaf.
func TestIsRedactedValueRejectsNonMarkerShapes(t *testing.T) {
	t.Parallel()

	cases := map[string]any{
		"nil":                nil,
		"plain string":       "ami-0123456789abcdef0",
		"marker-like string": "redacted:hmac-sha256:deadbeef",
		"number":             float64(42),
		"bool":               true,
		"slice":              []any{"a", "b"},
		"map without marker": map[string]any{"reason": "unknown_provider_schema", "source": "attributes.ami"},
	}
	for name, value := range cases {
		if redact.IsRedactedValue(value) {
			t.Fatalf("IsRedactedValue(%s = %#v) = true, want false", name, value)
		}
	}
}
