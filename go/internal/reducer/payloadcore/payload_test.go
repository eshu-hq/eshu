// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// TestStringAccessorsAreNotInterchangeable locks the differences between the
// three payload string readers. They share a signature, so a future change that
// "consolidates" them would compile and would silently alter projected truth.
// The cases below cover the three scenarios of the table in this package's
// README, plus whitespace trimming and an absent key, which the table omits.
func TestStringAccessorsAreNotInterchangeable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                   string
		payload                map[string]any
		key                    string
		str, string_, semantic string
	}{
		{
			name:    "non-string value is rendered by two of the three",
			payload: map[string]any{"k": 42},
			key:     "k",
			str:     "42", string_: "42", semantic: "",
		},
		{
			name:    "explicit nil value",
			payload: map[string]any{"k": nil},
			key:     "k",
			str:     "", string_: "", semantic: "",
		},
		{
			name:    "absent key",
			payload: map[string]any{},
			key:     "k",
			str:     "", string_: "", semantic: "",
		},
		{
			name:    "value rendering as the literal <nil> is dropped only by PayloadStr",
			payload: map[string]any{"k": (*string)(nil)},
			key:     "k",
			str:     "", string_: "<nil>", semantic: "",
		},
		{
			name:    "a string-typed <nil> is dropped only by PayloadStr",
			payload: map[string]any{"k": "<nil>"},
			key:     "k",
			str:     "", string_: "<nil>", semantic: "<nil>",
		},
		{
			name:    "surrounding whitespace is trimmed by all three",
			payload: map[string]any{"k": "  v  "},
			key:     "k",
			str:     "v", string_: "v", semantic: "v",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := payloadcore.PayloadStr(tc.payload, tc.key); got != tc.str {
				t.Errorf("PayloadStr = %q, want %q", got, tc.str)
			}
			if got := payloadcore.PayloadString(tc.payload, tc.key); got != tc.string_ {
				t.Errorf("PayloadString = %q, want %q", got, tc.string_)
			}
			if got := payloadcore.SemanticPayloadString(tc.payload, tc.key); got != tc.semantic {
				t.Errorf("SemanticPayloadString = %q, want %q", got, tc.semantic)
			}
		})
	}
}

// TestBoolAccessorsAreNotInterchangeable locks the one difference between the
// two boolean readers: PayloadBool accepts a "true"/"false" string, BoolPayload
// accepts only a real bool.
func TestBoolAccessorsAreNotInterchangeable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                   string
		value                  any
		payloadBool, boolValue bool
	}{
		{name: "real true", value: true, payloadBool: true, boolValue: true},
		{name: "real false", value: false, payloadBool: false, boolValue: false},
		{name: "true string counts only for PayloadBool", value: "true", payloadBool: true, boolValue: false},
		{name: "mixed-case true string", value: "TrUe", payloadBool: true, boolValue: false},
		{name: "false string", value: "false", payloadBool: false, boolValue: false},
		{name: "unparseable string is not true", value: "banana", payloadBool: false, boolValue: false},
		{name: "blank string", value: "  ", payloadBool: false, boolValue: false},
		{name: "number", value: 1, payloadBool: false, boolValue: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{"k": tc.value}
			if got := payloadcore.PayloadBool(payload, "k"); got != tc.payloadBool {
				t.Errorf("PayloadBool = %v, want %v", got, tc.payloadBool)
			}
			if got := payloadcore.BoolPayload(payload, "k"); got != tc.boolValue {
				t.Errorf("BoolPayload = %v, want %v", got, tc.boolValue)
			}
		})
	}
}

// TestPayloadBoolPointerValueDistinguishesAbsentFromFalse covers the reason the
// pointer-value form exists at all: an explicit false and an unusable value are
// different outcomes, and a caller storing a *bool needs to tell them apart.
func TestPayloadBoolPointerValueDistinguishesAbsentFromFalse(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		value         any
		want, present bool
		absentKey     bool
	}{
		{name: "explicit false is present", value: false, want: false, present: true},
		{name: "explicit true is present", value: true, want: true, present: true},
		{name: "false string is present", value: "false", want: false, present: true},
		{name: "unparseable string is present, not absent", value: "banana", want: false, present: true},
		{name: "blank string is absent", value: "  ", want: false, present: false},
		{name: "number is absent", value: 0, want: false, present: false},
		{name: "missing key is absent", absentKey: true, want: false, present: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{"k": tc.value}
			if tc.absentKey {
				payload = map[string]any{}
			}
			got, present := payloadcore.PayloadBoolPointerValue(payload, "k")
			if got != tc.want || present != tc.present {
				t.Errorf("PayloadBoolPointerValue = (%v, %v), want (%v, %v)", got, present, tc.want, tc.present)
			}
		})
	}
}

// TestUniqueSortedStringsNormalizes covers the trim, drop-empty, dedupe and sort
// behavior that ~34 families depend on.
func TestUniqueSortedStringsNormalizes(t *testing.T) {
	t.Parallel()

	if got := payloadcore.UniqueSortedStrings(nil); got != nil {
		t.Errorf("UniqueSortedStrings(nil) = %v, want nil", got)
	}
	if got := payloadcore.UniqueSortedStrings([]string{"  ", ""}); got != nil {
		t.Errorf("all-blank input = %v, want nil", got)
	}
	got := payloadcore.UniqueSortedStrings([]string{" b ", "a", "b", "", "a"})
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestRepositoryIDFromReducerScope covers the two accepted scope shapes and the
// rejection of everything else.
func TestRepositoryIDFromReducerScope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{in: "repository:abc", want: "repository:abc"},
		{in: "  repository:abc  ", want: "repository:abc"},
		{in: "git-repository-scope:abc", want: "abc"},
		{in: "workload:abc", want: ""},
		{in: "", want: ""},
	} {
		if got := payloadcore.RepositoryIDFromReducerScope(tc.in); got != tc.want {
			t.Errorf("RepositoryIDFromReducerScope(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestOCIRepositoryIDPrefersExplicitID covers the fallback composition and the
// refusal to compose from a partial pair.
func TestOCIRepositoryIDPrefersExplicitID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "explicit id wins", payload: map[string]any{"repository_id": "id", "registry": "r", "repository": "p"}, want: "id"},
		{name: "composed from the pair", payload: map[string]any{"registry": "r", "repository": "p"}, want: "oci-registry://r/p"},
		{name: "registry alone is not enough", payload: map[string]any{"registry": "r"}, want: ""},
		{name: "repository alone is not enough", payload: map[string]any{"repository": "p"}, want: ""},
		{name: "empty payload", payload: map[string]any{}, want: ""},
	} {
		if got := payloadcore.OCIRepositoryID(tc.payload); got != tc.want {
			t.Errorf("%s: OCIRepositoryID = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestNonNilStringsSubstitutesEmptySlice pins the nil -> [] substitution. It
// reaches API and MCP truth: a finding payload encoded from a nil slice carries
// null, and callers are promised they can range over the collection without a
// nil guard.
func TestNonNilStringsSubstitutesEmptySlice(t *testing.T) {
	t.Parallel()

	got := payloadcore.NonNilStrings(nil)
	if got == nil {
		t.Fatal("NonNilStrings(nil) returned nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("NonNilStrings(nil) = %v, want empty", got)
	}
	in := []string{"a"}
	if out := payloadcore.NonNilStrings(in); len(out) != 1 || out[0] != "a" {
		t.Fatalf("NonNilStrings(%v) = %v, want it unchanged", in, out)
	}
}

// TestDerefBoolReadsThePointedValue pins that a pointer to false reads as false.
// Treating a non-nil pointer as true would change projected incident-routing
// truth, because the emitter omits a false flag and nil means "not set".
func TestDerefBoolReadsThePointedValue(t *testing.T) {
	t.Parallel()

	no, yes := false, true
	if payloadcore.DerefBool(&no) {
		t.Error("DerefBool(&false) = true, want false")
	}
	if !payloadcore.DerefBool(&yes) {
		t.Error("DerefBool(&true) = false, want true")
	}
	if payloadcore.DerefBool(nil) {
		t.Error("DerefBool(nil) = true, want false")
	}
}

// TestCopyPayloadIsolatesTheCopy pins that the result does not alias the source.
// Returning the same map would defeat the isolation this helper exists for on
// the shared-projection intent path.
func TestCopyPayloadIsolatesTheCopy(t *testing.T) {
	t.Parallel()

	src := map[string]any{"k": "v"}
	cp := payloadcore.CopyPayload(src)
	cp["k"] = "mutated"
	cp["added"] = 1
	if src["k"] != "v" {
		t.Errorf("mutating the copy changed the source: src[k] = %v, want v", src["k"])
	}
	if _, ok := src["added"]; ok {
		t.Error("adding to the copy added to the source")
	}
}

// TestToStringSliceBranchesDifferInFiltering pins the asymmetry the doc comment
// describes: the []any and scalar forms drop empty and "<nil>" renderings, the
// []string form is returned untouched.
func TestToStringSliceBranchesDifferInFiltering(t *testing.T) {
	t.Parallel()

	got := payloadcore.ToStringSlice([]string{"", "<nil>", "a"})
	if len(got) != 3 {
		t.Errorf("[]string form = %v, want it returned unfiltered", got)
	}
	got = payloadcore.ToStringSlice([]any{"", "<nil>", "a"})
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("[]any form = %v, want only [a]", got)
	}
	if got = payloadcore.ToStringSlice("<nil>"); got != nil {
		t.Errorf("scalar <nil> = %v, want nil", got)
	}
}

// TestFormatTallyIsDeterministic pins the key sort. Without it the completion
// log line reorders between runs and an operator comparing two runs sees a
// difference that is not there.
func TestFormatTallyIsDeterministic(t *testing.T) {
	t.Parallel()

	counts := map[string]int{"z": 1, "a": 2, "m": 3}
	first := payloadcore.FormatTally(counts)
	for range 20 {
		if got := payloadcore.FormatTally(counts); got != first {
			t.Fatalf("FormatTally is not deterministic: %q then %q", first, got)
		}
	}
	if !strings.HasPrefix(first, "a=") {
		t.Errorf("FormatTally = %q, want it to start with the lowest key", first)
	}
}
