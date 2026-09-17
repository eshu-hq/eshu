// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routecontract

import (
	"reflect"
	"testing"
)

func TestArgumentsPreserveDispatchCoercions(t *testing.T) {
	t.Parallel()

	anyValues := []any{"CALLS", "IMPORTS"}
	stringValues := []string{"INHERITS", "OVERRIDES"}
	var nilStrings []string
	args := Arguments{
		"string":            "checkout",
		"string_wrong":      7,
		"int":               11,
		"int64":             int64(12),
		"float64":           13.9,
		"float64_negative":  -13.9,
		"float32_wrong":     float32(14.5),
		"bool":              true,
		"bool_wrong":        "true",
		"optional_float64":  0.75,
		"optional_float32":  float32(0.5),
		"optional_int":      2,
		"optional_int64":    int64(3),
		"optional_wrong":    "0.25",
		"slice_any":         anyValues,
		"slice_strings":     stringValues,
		"slice_nil_strings": nilStrings,
		"slice_wrong":       "CALLS",
	}

	if got, want := args.String("string"), "checkout"; got != want {
		t.Fatalf("String(string) = %q, want %q", got, want)
	}
	for _, key := range []string{"missing", "string_wrong"} {
		if got := args.String(key); got != "" {
			t.Errorf("String(%s) = %q, want empty", key, got)
		}
	}

	for _, tt := range []struct {
		key  string
		want int
	}{
		{key: "int", want: 11},
		{key: "int64", want: 12},
		{key: "float64", want: 13},
		{key: "float64_negative", want: -13},
		{key: "float32_wrong", want: 99},
		{key: "missing", want: 99},
	} {
		if got := args.IntOr(tt.key, 99); got != tt.want {
			t.Errorf("IntOr(%s, 99) = %d, want %d", tt.key, got, tt.want)
		}
	}

	if got := args.BoolOr("bool", false); !got {
		t.Error("BoolOr(bool, false) = false, want true")
	}
	for _, key := range []string{"missing", "bool_wrong"} {
		if got := args.BoolOr(key, true); !got {
			t.Errorf("BoolOr(%s, true) = false, want fallback true", key)
		}
	}

	for _, tt := range []struct {
		key  string
		want float64
	}{
		{key: "optional_float64", want: 0.75},
		{key: "optional_float32", want: 0.5},
		{key: "optional_int", want: 2},
		{key: "optional_int64", want: 3},
	} {
		got, ok := args.OptionalFloat(tt.key)
		if !ok || got != tt.want {
			t.Errorf("OptionalFloat(%s) = (%v, %v), want (%v, true)", tt.key, got, ok, tt.want)
		}
	}
	for _, key := range []string{"missing", "optional_wrong"} {
		if got, ok := args.OptionalFloat(key); ok || got != 0 {
			t.Errorf("OptionalFloat(%s) = (%v, %v), want (0, false)", key, got, ok)
		}
	}

	gotAny := args.StringSlice("slice_any")
	if !reflect.DeepEqual(gotAny, anyValues) {
		t.Fatalf("StringSlice(slice_any) = %#v, want %#v", gotAny, anyValues)
	}
	gotAny[0] = "MUTATED"
	if got := anyValues[0]; got != "MUTATED" {
		t.Fatalf("StringSlice(slice_any) returned a copy; original[0] = %#v", got)
	}

	if got, want := args.StringSlice("slice_strings"), []any{"INHERITS", "OVERRIDES"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("StringSlice(slice_strings) = %#v, want %#v", got, want)
	}
	if got := args.StringSlice("slice_nil_strings"); got == nil || len(got) != 0 {
		t.Fatalf("StringSlice(slice_nil_strings) = %#v, want non-nil empty slice", got)
	}
	for _, key := range []string{"missing", "slice_wrong"} {
		if got := args.StringSlice(key); got != nil {
			t.Errorf("StringSlice(%s) = %#v, want nil", key, got)
		}
	}
}

func TestRequestCarriesDispatchShape(t *testing.T) {
	t.Parallel()

	body := map[string]any{"target": "checkout"}
	query := map[string]string{"limit": "25"}
	request := Request{
		Method: "POST",
		Path:   "/api/v0/code/relationships/story",
		Body:   body,
		Query:  query,
	}

	if request.Method != "POST" || request.Path != "/api/v0/code/relationships/story" {
		t.Fatalf("Request method/path = %s %s", request.Method, request.Path)
	}
	if !reflect.DeepEqual(request.Body, body) || !reflect.DeepEqual(request.Query, query) {
		t.Fatalf("Request body/query = %#v / %#v", request.Body, request.Query)
	}
}
