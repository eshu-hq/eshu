// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"
)

// TestParseJSONTag covers the tag-splitting helper the key-set derivation
// relies on: the skip tag, the "-," escape for a field literally named "-", an
// empty tag name defaulting to the Go field name, and omitempty appearing in
// any option position (json.Marshal does not require it last).
func TestParseJSONTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		tag           string
		fieldName     string
		wantName      string
		wantOmitEmpty bool
		wantSkip      bool
	}{
		{name: "plain", tag: "region", fieldName: "Region", wantName: "region"},
		{name: "omitempty", tag: "name,omitempty", fieldName: "Name", wantName: "name", wantOmitEmpty: true},
		{name: "omitempty_not_last", tag: "name,omitempty,string", fieldName: "Name", wantName: "name", wantOmitEmpty: true},
		{name: "string_then_omitempty", tag: "name,string,omitempty", fieldName: "Name", wantName: "name", wantOmitEmpty: true},
		{name: "skip", tag: "-", fieldName: "Ignored", wantSkip: true},
		{name: "dash_name_escape", tag: "-,", fieldName: "Dash", wantName: "-"},
		{name: "empty_tag_defaults_to_field", tag: "", fieldName: "AccountID", wantName: "AccountID"},
		{name: "empty_name_with_option", tag: ",omitempty", fieldName: "Tags", wantName: "Tags", wantOmitEmpty: true},
		{name: "string_option_ignored", tag: "count,string", fieldName: "Count", wantName: "count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			name, omitEmpty, skip := parseJSONTag(tc.tag, tc.fieldName)
			if name != tc.wantName {
				t.Fatalf("parseJSONTag(%q, %q) name = %q, want %q", tc.tag, tc.fieldName, name, tc.wantName)
			}
			if omitEmpty != tc.wantOmitEmpty {
				t.Fatalf("parseJSONTag(%q, %q) omitEmpty = %v, want %v", tc.tag, tc.fieldName, omitEmpty, tc.wantOmitEmpty)
			}
			if skip != tc.wantSkip {
				t.Fatalf("parseJSONTag(%q, %q) skip = %v, want %v", tc.tag, tc.fieldName, skip, tc.wantSkip)
			}
		})
	}
}

// TestPayloadKeySetOf_DerivesRequiredAndKnown proves the helper splits fields
// into required (no omitempty) and known (all serialized) sets and skips both
// unexported and json:"-" fields, in declared order.
func TestPayloadKeySetOf_DerivesRequiredAndKnown(t *testing.T) {
	t.Parallel()

	type sample struct {
		Req        string  `json:"req"`
		Opt        *string `json:"opt,omitempty"`
		Renamed    int     `json:"count,string"`
		Defaulted  string
		Skipped    string `json:"-"`
		unexported string //nolint:unused // present to prove unexported fields are skipped
	}
	_ = sample{}.unexported

	ks := payloadKeySetOf(reflect.TypeOf(sample{}))

	wantKnown := []string{"req", "opt", "count", "Defaulted"}
	if !reflect.DeepEqual(ks.Known, wantKnown) {
		t.Fatalf("Known = %v, want %v", ks.Known, wantKnown)
	}
	wantRequired := []string{"req", "count", "Defaulted"}
	if !reflect.DeepEqual(ks.Required, wantRequired) {
		t.Fatalf("Required = %v, want %v", ks.Required, wantRequired)
	}
}

// TestPayloadKeySetOf_Cached proves repeated calls return an equal set and the
// cache is populated, so the reflection walk happens once per type.
func TestPayloadKeySetOf_Cached(t *testing.T) {
	t.Parallel()

	type cachedSample struct {
		A string `json:"a"`
	}
	typ := reflect.TypeOf(cachedSample{})

	first := payloadKeySetOf(typ)
	if _, ok := payloadKeySets.Load(typ); !ok {
		t.Fatalf("payloadKeySetOf did not cache the result for %s", typ)
	}
	second := payloadKeySetOf(typ)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached result %v differs from first %v", second, first)
	}
}

// TestPayloadKeySetOf_PanicsOnNonStruct proves the programming-error guard
// fires for a non-struct type rather than returning a meaningless empty set.
func TestPayloadKeySetOf_PanicsOnNonStruct(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatalf("payloadKeySetOf(non-struct) did not panic")
		}
	}()
	payloadKeySetOf(reflect.TypeOf("not a struct"))
}

// TestPayloadKeySetOf_PanicsOnEmbeddedField proves an anonymous (embedded)
// field is rejected rather than silently mis-derived: supporting embedding
// would require reimplementing encoding/json's field promotion, a
// silent-divergence risk the flat-struct rule exists to avoid.
func TestPayloadKeySetOf_PanicsOnEmbeddedField(t *testing.T) {
	t.Parallel()

	type embedded struct {
		Inner string `json:"inner"`
	}
	type outer struct {
		embedded
		Outer string `json:"outer"`
	}

	defer func() {
		if recover() == nil {
			t.Fatalf("payloadKeySetOf(struct with embedded field) did not panic")
		}
	}()
	payloadKeySetOf(reflect.TypeOf(outer{}))
}
