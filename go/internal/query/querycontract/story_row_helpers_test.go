// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"math"
	"reflect"
	"testing"
	"time"
)

// TestLowerStrings proves lowercasing with sorted output.
func TestLowerStrings(t *testing.T) {
	t.Parallel()

	if got := LowerStrings([]string{"B", "a", "C"}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("LowerStrings = %#v, want [a b c]", got)
	}
}

// TestUniqueSortedStringsDirect proves blank and duplicate removal.
func TestUniqueSortedStringsDirect(t *testing.T) {
	t.Parallel()

	got := UniqueSortedStrings([]string{"b", " ", "a", "b", ""})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("UniqueSortedStrings = %#v, want [a b]", got)
	}
	if got := UniqueSortedStrings(nil); got != nil {
		t.Fatalf("nil UniqueSortedStrings = %#v, want nil", got)
	}
}

// TestJoinSentenceFragments proves English joining for every arity.
func TestJoinSentenceFragments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		parts []string
		want  string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b, and c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c, and d"},
	}
	for _, tc := range cases {
		if got := JoinSentenceFragments(tc.parts); got != tc.want {
			t.Fatalf("JoinSentenceFragments(%#v) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}

// TestFirstPositiveFloat proves the first positive candidate wins.
func TestFirstPositiveFloat(t *testing.T) {
	t.Parallel()

	if got := FirstPositiveFloat(0, -1, 2.5, 3); got != 2.5 {
		t.Fatalf("FirstPositiveFloat = %v, want 2.5", got)
	}
	if got := FirstPositiveFloat(0, -1); got != 0 {
		t.Fatalf("FirstPositiveFloat without positives = %v, want 0", got)
	}
}

// TestAppendUniqueString proves blank and duplicate candidates are
// refused while fresh ones append trimmed.
func TestAppendUniqueString(t *testing.T) {
	t.Parallel()

	values := []string{"a"}
	AppendUniqueString(&values, "  b  ")
	AppendUniqueString(&values, "a")
	AppendUniqueString(&values, "   ")
	if !reflect.DeepEqual(values, []string{"a", "b"}) {
		t.Fatalf("AppendUniqueString values = %#v, want [a b]", values)
	}
}

// TestContainsHelpers proves membership and substring conjunction.
func TestContainsHelpers(t *testing.T) {
	t.Parallel()

	if !ContainsString([]string{"a", "b"}, "b") {
		t.Fatal("ContainsString missed a member")
	}
	if ContainsString([]string{"a"}, "z") {
		t.Fatal("ContainsString hit a non-member")
	}
	if !ContainsAllSubstrings("the quick fox", "quick", "fox") {
		t.Fatal("ContainsAllSubstrings missed present parts")
	}
	if ContainsAllSubstrings("the quick fox", "quick", "wolf") {
		t.Fatal("ContainsAllSubstrings hit a missing part")
	}
	if !ContainsAllSubstrings("anything") {
		t.Fatal("empty parts must report true")
	}
}

// TestFirstNonEmptyString proves trimming with the first content win.
func TestFirstNonEmptyString(t *testing.T) {
	t.Parallel()

	if got := FirstNonEmptyString("", "  b  ", "c"); got != "b" {
		t.Fatalf("FirstNonEmptyString = %q, want b", got)
	}
	if got := FirstNonEmptyString("", "  "); got != "" {
		t.Fatalf("blank FirstNonEmptyString = %q, want empty", got)
	}
}

// TestFiniteGraphFloat proves finite values pass while NaN and Inf fail
// naming the subject and key.
func TestFiniteGraphFloat(t *testing.T) {
	t.Parallel()

	value, err := FiniteGraphFloat(map[string]any{"score": 0.5}, "score", "row")
	if err != nil || value != 0.5 {
		t.Fatalf("FiniteGraphFloat = (%v, %v), want (0.5, nil)", value, err)
	}
	if value, err := FiniteGraphFloat(map[string]any{}, "score", "row"); err != nil || value != 0 {
		t.Fatalf("missing FiniteGraphFloat = (%v, %v), want (0, nil)", value, err)
	}
	for name, raw := range map[string]any{"nan": math.NaN(), "inf": math.Inf(1)} {
		if _, err := FiniteGraphFloat(map[string]any{"score": raw}, "score", "row"); err == nil {
			t.Fatalf("FiniteGraphFloat(%s) succeeded, want an error", name)
		} else if got := err.Error(); got != "row score must be finite" {
			t.Fatalf("FiniteGraphFloat(%s) error = %q, want the subject/key message", name, got)
		}
	}
}

// TestCleanMetadataString proves trimming with empty and <nil> mapping.
func TestCleanMetadataString(t *testing.T) {
	t.Parallel()

	if got := CleanMetadataString("  v  "); got != "v" {
		t.Fatalf("CleanMetadataString = %q, want v", got)
	}
	for _, raw := range []string{"", "   ", "<nil>"} {
		if got := CleanMetadataString(raw); got != "" {
			t.Fatalf("CleanMetadataString(%q) = %q, want empty", raw, got)
		}
	}
	if value, ok := MetadataNonEmptyString(map[string]any{"k": " v "}, "k"); !ok || value != "v" {
		t.Fatalf("MetadataNonEmptyString = (%q, %v), want (v, true)", value, ok)
	}
	if _, ok := MetadataNonEmptyString(map[string]any{"k": "  "}, "k"); ok {
		t.Fatal("blank MetadataNonEmptyString reported ok, want false")
	}
	if _, ok := MetadataNonEmptyString(map[string]any{"k": 3}, "k"); ok {
		t.Fatal("non-string MetadataNonEmptyString reported ok, want false")
	}
}

// TestAddUniqueStringField proves row slices grow deduped and sorted,
// skipping blanks.
func TestAddUniqueStringField(t *testing.T) {
	t.Parallel()

	row := map[string]any{"tags": []string{"b"}}
	AddUniqueStringField(row, "tags", "a")
	AddUniqueStringField(row, "tags", "a")
	AddUniqueStringField(row, "tags", "  ")
	if got := row["tags"]; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("row tags = %#v, want [a b]", got)
	}
}

// TestSortStringFields proves in-place sorting of the named columns.
func TestSortStringFields(t *testing.T) {
	t.Parallel()

	row := map[string]any{"b": []string{"y", "x"}, "a": []string{"b", "a"}}
	SortStringFields(row, "a", "b", "missing")
	if !reflect.DeepEqual(row["a"], []string{"a", "b"}) || !reflect.DeepEqual(row["b"], []string{"x", "y"}) {
		t.Fatalf("sorted row = %#v, want sorted columns", row)
	}
}

// TestRowStringSliceValue proves slice, any, and foreign decoding.
func TestRowStringSliceValue(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"s": []string{"a", ""},
		"a": []any{" b ", "", 4},
		"n": 1,
	}
	if got := StringSliceValue(value, "s"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("string branch = %#v, want [a]", got)
	}
	if got := StringSliceValue(value, "a"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("any branch = %#v, want [b]", got)
	}
	if got := StringSliceValue(value, "n"); got != nil {
		t.Fatalf("scalar branch = %#v, want nil", got)
	}
	if got := StringSliceValue(map[string]any{}, "s"); got != nil {
		t.Fatalf("empty map branch = %#v, want nil", got)
	}
}

// TestFirstPositiveIntAndMax proves first-positive key decoding and the
// scalar maxima.
func TestFirstPositiveIntAndMax(t *testing.T) {
	t.Parallel()

	row := map[string]any{"a": 0, "b": 3}
	if got := FirstPositiveInt(row, "a", "b", "missing"); got != 3 {
		t.Fatalf("FirstPositiveInt = %d, want 3", got)
	}
	if got := FirstPositiveInt(row, "a", "missing"); got != 0 {
		t.Fatalf("FirstPositiveInt without positives = %d, want 0", got)
	}
	if got := MaxInt(2, 5); got != 5 {
		t.Fatalf("MaxInt = %d, want 5", got)
	}
	if got := MaxInt(5, 2); got != 5 {
		t.Fatalf("MaxInt reversed = %d, want 5", got)
	}
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if got := MaxTime(early, late); !got.Equal(late) {
		t.Fatalf("MaxTime = %v, want the later time", got)
	}
	if got := MaxTime(late, early); !got.Equal(late) {
		t.Fatalf("MaxTime reversed = %v, want the later time", got)
	}
}

// TestPrimaryEntityLabelAndBool proves first-label selection and strict
// boolean decoding.
func TestPrimaryEntityLabelAndBool(t *testing.T) {
	t.Parallel()

	entity := map[string]any{"labels": []string{"Service", "Workload"}}
	if got := PrimaryEntityLabel(entity); got != "Service" {
		t.Fatalf("PrimaryEntityLabel = %q, want Service", got)
	}
	if got := PrimaryEntityLabel(map[string]any{}); got != "" {
		t.Fatalf("labelless PrimaryEntityLabel = %q, want empty", got)
	}
	if !BoolValue(true) || BoolValue(false) || BoolValue("true") || BoolValue(nil) {
		t.Fatal("BoolValue must be true only for boolean true")
	}
}

// TestTrimGitHubActionsScalar proves single quote-layer stripping.
func TestTrimGitHubActionsScalar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want string
	}{
		{`"quoted"`, "quoted"},
		{`'quoted'`, "quoted"},
		{`  "spaced"  `, "spaced"},
		{`plain`, "plain"},
		{`x`, "x"},
		{`"mismatched'`, `"mismatched'`},
		{``, ""},
	}
	for _, tc := range cases {
		if got := TrimGitHubActionsScalar(tc.raw); got != tc.want {
			t.Fatalf("TrimGitHubActionsScalar(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestCopyStringSliceField proves non-empty slices copy while empty and
// missing ones leave the destination alone.
func TestCopyStringSliceField(t *testing.T) {
	t.Parallel()

	dst := map[string]any{}
	CopyStringSliceField(dst, map[string]any{"k": []string{"a"}}, "k")
	CopyStringSliceField(dst, map[string]any{"e": []string{}}, "e")
	CopyStringSliceField(dst, map[string]any{}, "missing")
	if !reflect.DeepEqual(dst, map[string]any{"k": []string{"a"}}) {
		t.Fatalf("CopyStringSliceField dst = %#v, want only k", dst)
	}
}

// TestSortedSetKeys proves blank skipping with sorted output.
func TestSortedSetKeys(t *testing.T) {
	t.Parallel()

	got := SortedSetKeys(map[string]struct{}{"b": {}, "": {}, "a": {}, "  ": {}})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("SortedSetKeys = %#v, want [a b]", got)
	}
}

// TestRowMetadataString proves strings pass through while foreign types
// and missing keys yield "".
func TestRowMetadataString(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"s": "v", "n": 3}
	if got := MetadataString(metadata, "s"); got != "v" {
		t.Fatalf("MetadataString = %q, want v", got)
	}
	if got := MetadataString(metadata, "n"); got != "" {
		t.Fatalf("non-string MetadataString = %q, want empty", got)
	}
	if got := MetadataString(metadata, "missing"); got != "" {
		t.Fatalf("missing MetadataString = %q, want empty", got)
	}
}

// TestCloneAndMergeStrings proves detached copies and ordered merges.
func TestCloneAndMergeStrings(t *testing.T) {
	t.Parallel()

	src := []string{"a", "b"}
	clone := CloneStrings(src)
	clone[0] = "z"
	if !reflect.DeepEqual(src, []string{"a", "b"}) {
		t.Fatalf("clone mutated the source: %#v", src)
	}
	if got := CloneStrings(nil); got != nil {
		t.Fatalf("nil CloneStrings = %#v, want nil", got)
	}
	got := MergeStringSets([]string{"a", "b"}, []string{"b", "", "c"})
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("MergeStringSets = %#v, want [a b c]", got)
	}
}
