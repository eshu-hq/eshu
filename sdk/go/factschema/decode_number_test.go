// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

func TestPublicTypedDecoderPreservesJSONNumber(t *testing.T) {
	payload, ok := fixturepack.ValidPayload(FactKindWorkItemProjectMetadata)
	if !ok {
		t.Fatal("missing project metadata fixture")
	}
	payload["issue_count"] = json.Number("9007199254740993")
	got, err := DecodeWorkItemProjectMetadata(Envelope{
		FactKind: FactKindWorkItemProjectMetadata, SchemaVersion: "1.0.0", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IssueCount == nil || *got.IssueCount != int64(9007199254740993) {
		t.Fatalf("issue_count = %v", got.IssueCount)
	}
}

func TestNumberPreservingIntegerAssignment(t *testing.T) {
	type nested struct {
		Count int64 `json:"count"`
	}
	type fields struct {
		Plain  int      `json:"plain"`
		Wide   int64    `json:"wide"`
		Narrow int32    `json:"narrow"`
		Ptr    *int32   `json:"ptr"`
		Nested *nested  `json:"nested"`
		Items  []nested `json:"items"`
		Absent *int64   `json:"absent"`
		Null   *int64   `json:"null"`
	}
	var got fields
	err := decodeMapInto(map[string]any{
		"plain": json.Number("1.0"), "wide": json.Number("9007199254740993"),
		"narrow": json.Number("1e3"), "ptr": json.Number("1000e-3"),
		"nested": map[string]any{"count": json.Number("-0")},
		"items":  []any{map[string]any{"count": json.Number("9007199254740993")}},
		"null":   nil,
	}, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plain != 1 || got.Wide != 9007199254740993 || got.Narrow != 1000 ||
		got.Ptr == nil || *got.Ptr != 1 || got.Nested == nil || got.Nested.Count != 0 ||
		len(got.Items) != 1 || got.Items[0].Count != 9007199254740993 || got.Absent != nil || got.Null != nil {
		t.Fatalf("unexpected decoded fields: %+v", got)
	}
}

func TestNumberPreservingIntegerGrammarAndRange(t *testing.T) {
	valid := map[string]int64{
		"0": 0, "-0": 0, "-0e999999999999999999999": 0,
		"1.0": 1, "1e3": 1000, "1000e-3": 1,
		"9007199254740991":                   9007199254740991,
		"9007199254740992":                   9007199254740992,
		"9007199254740993":                   9007199254740993,
		"9223372036854775807":                math.MaxInt64,
		"-9223372036854775808":               math.MinInt64,
		"-9007199254740993":                  -9007199254740993,
		"1.2300e2":                           123,
		"0.0001e4":                           1,
		"123456789012345678900000000000e-11": 1234567890123456789,
	}
	for input, want := range valid {
		t.Run(input, func(t *testing.T) {
			got, err := jsonNumberToInt64(json.Number(input))
			if err != nil || got != want {
				t.Fatalf("got %d, %v; want %d", got, err, want)
			}
		})
	}
	invalid := []string{
		"", "+1", "01", "-01", "1.", ".1", "1e", "1e+", " 1", "1 ",
		"1_0", "0x10", "NaN", "Inf", "1.0000000000000000001", "1e-999999999999999999",
		"9223372036854775808", "-9223372036854775809", "1e99999999999999999",
		"0.00001", "1e-1000", "1e" + strings.Repeat("9", 10000) + "x",
	}
	for _, input := range invalid {
		t.Run("invalid/"+input, func(t *testing.T) {
			if _, err := jsonNumberToInt64(json.Number(input)); err == nil {
				t.Fatal("accepted invalid integer")
			}
		})
	}
	for _, tc := range []struct {
		input string
		want  int32
		ok    bool
	}{
		{"2147483647", math.MaxInt32, true},
		{"-2147483648", math.MinInt32, true},
		{"2147483648", 0, false},
		{"-2147483649", 0, false},
	} {
		got, err := jsonNumberToInt32(json.Number(tc.input))
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Fatalf("int32 %s = %d, %v", tc.input, got, err)
		}
	}
	if strconv.IntSize == 32 {
		if _, err := jsonNumberToInt(json.Number("2147483648")); err == nil {
			t.Fatal("platform int accepted int32 overflow")
		}
	} else if got, err := jsonNumberToInt(json.Number("9007199254740993")); err != nil || got != 9007199254740993 {
		t.Fatalf("platform int = %d, %v", got, err)
	}
	long := "1" + strings.Repeat("0", 10000) + "e-10000"
	if got, err := jsonNumberToInt64(json.Number(long)); err != nil || got != 1 {
		t.Fatalf("long compensating token = %d, %v", got, err)
	}
	if _, err := jsonNumberToInt64(json.Number(strings.Repeat("9", 10000))); err == nil || len(err.Error()) > 128 {
		t.Fatalf("unbounded or missing error: %v", err)
	}
}

func TestNumberPreservingPlainInt32KeepsLegacyFallback(t *testing.T) {
	type holder struct {
		Count int32 `json:"count"`
	}
	var got holder
	if err := decodeMapInto(map[string]any{"count": float32(7)}, &got); err != nil || got.Count != 7 {
		t.Fatalf("legacy float32 to plain int32 = %d, %v", got.Count, err)
	}
}

var (
	numberBenchmarkValue int64
	numberBenchmarkError error
)

func BenchmarkNumberCoercion(b *testing.B) {
	for _, tc := range []struct {
		name string
		raw  any
	}{
		{"float64", float64(42)},
		{"int64", int64(42)},
		{"long-invalid-number", json.Number(strings.Repeat("9", 10000))},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				numberBenchmarkValue, numberBenchmarkError = jsonNumberToInt64(tc.raw)
			}
		})
	}
}

func TestNumberPreservingFloatAssignment(t *testing.T) {
	type fields struct {
		Plain float64  `json:"plain"`
		Ptr   *float64 `json:"ptr"`
	}
	var got fields
	if err := decodeMapInto(map[string]any{"plain": json.Number("1.25"), "ptr": json.Number("-0e999")}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Plain != 1.25 || got.Ptr == nil || !math.Signbit(*got.Ptr) {
		t.Fatalf("float fields = %+v", got)
	}
	for _, input := range []string{"NaN", "Inf", "1e9999", "+1", "01", "1 ", "0x1p2"} {
		if _, err := jsonNumberToFloat64(json.Number(input)); err == nil {
			t.Fatalf("accepted float %q", input)
		}
	}
	if n, err := jsonNumberToFloat64(json.Number("1e-9999")); err != nil || n != 0 {
		t.Fatalf("underflow = %v, %v", n, err)
	}
	if n, err := jsonNumberToFloat64(json.Number("9007199254740993")); err != nil || n != 9007199254740992 {
		t.Fatalf("IEEE rounding = %v, %v", n, err)
	}
}

func TestNumberPreservingErrorsAreClassifiedAndBounded(t *testing.T) {
	payload, ok := fixturepack.ValidPayload(FactKindWorkItemProjectMetadata)
	if !ok {
		t.Fatal("missing project metadata fixture")
	}
	payload["issue_count"] = json.Number(strings.Repeat("9", 10000))
	_, err := DecodeWorkItemProjectMetadata(Envelope{FactKind: FactKindWorkItemProjectMetadata, SchemaVersion: "1.0.0", Payload: payload})
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) || decodeErr.Classification != ClassificationInputInvalid || len(err.Error()) > 256 {
		t.Fatalf("error classification/bound: %v", err)
	}
}
