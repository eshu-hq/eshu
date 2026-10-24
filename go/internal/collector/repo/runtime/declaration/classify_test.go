// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/json"
	"reflect"
	"testing"
)

func intp(v int) *int       { return &v }
func strp(v string) *string { return &v }
func boolp(v bool) *bool    { return &v }

// TestClassify pins the version-string classifier. Every row is a binding
// ruling case from issue #8815: an alias or unresolved reference must never be
// guessed into a version, and a range never fills version_major.
func TestClassify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want Spec
	}{
		{"exact", "20.11.1", Spec{Kind: SpecKindExact, Major: intp(20), Minor: intp(11), Patch: intp(1)}},
		{"exact with v prefix drops the v from parts", "v20.11.1", Spec{Kind: SpecKindExact, Major: intp(20), Minor: intp(11), Patch: intp(1)}},
		{"exact with prerelease tail", "3.12.0rc1", Spec{Kind: SpecKindExact, Major: intp(3), Minor: intp(12), Patch: intp(0)}},
		{"partial major only", "20", Spec{Kind: SpecKindPartial, Major: intp(20)}},
		{"partial major minor", "3.2", Spec{Kind: SpecKindPartial, Major: intp(3), Minor: intp(2)}},
		{"partial wildcard", "20.x", Spec{Kind: SpecKindPartial, Major: intp(20)}},
		{"partial wildcard star", "3.9.*", Spec{Kind: SpecKindPartial, Major: intp(3), Minor: intp(9)}},
		{"equals operator is exact", "=20.1.0", Spec{Kind: SpecKindExact, Major: intp(20), Minor: intp(1), Patch: intp(0)}},
		{
			"range two comparators",
			">=18 <21",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp(">=18"), RangeLowerMajor: intp(18), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("<21"), RangeUpperMajor: intp(21), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range comma separated",
			">=3.9,<4",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp(">=3.9"), RangeLowerMajor: intp(3), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("<4"), RangeUpperMajor: intp(4), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range operator separated from version",
			">= 18 < 21",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp(">=18"), RangeLowerMajor: intp(18), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("<21"), RangeUpperMajor: intp(21), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range compatible release",
			"~=3.9",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("~=3.9"), RangeLowerMajor: intp(3), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("~=3.9"), RangeUpperMajor: intp(4), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range disjunction keeps only the first lower major",
			"^14 || >=16",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("^14 || >=16"), RangeLowerMajor: intp(14), RangeLowerInclusive: boolp(true),
			},
		},
		{
			"range only an upper bound",
			"<21",
			Spec{
				Kind:       SpecKindRange,
				RangeUpper: strp("<21"), RangeUpperMajor: intp(21), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range hyphen",
			"18 - 20",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("18"), RangeLowerMajor: intp(18), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("20"), RangeUpperMajor: intp(20), RangeUpperInclusive: boolp(true),
			},
		},
		{
			"range caret bumps the next major",
			"^18.2",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("^18.2"), RangeLowerMajor: intp(18), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("^18.2"), RangeUpperMajor: intp(19), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range caret on a zero major stays inside it",
			"^0.2.3",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("^0.2.3"), RangeLowerMajor: intp(0), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("^0.2.3"), RangeUpperMajor: intp(0), RangeUpperInclusive: boolp(true),
			},
		},
		{
			"range npm tilde with a minor stays inside the major",
			"~1.2.3",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("~1.2.3"), RangeLowerMajor: intp(1), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("~1.2.3"), RangeUpperMajor: intp(1), RangeUpperInclusive: boolp(true),
			},
		},
		{
			"range pessimistic operator with two components bumps the major",
			"~> 3.2",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp("~>3.2"), RangeLowerMajor: intp(3), RangeLowerInclusive: boolp(true),
				RangeUpper: strp("~>3.2"), RangeUpperMajor: intp(4), RangeUpperInclusive: boolp(false),
			},
		},
		{
			"range strictly greater excludes its major",
			">18",
			Spec{
				Kind:       SpecKindRange,
				RangeLower: strp(">18"), RangeLowerMajor: intp(18), RangeLowerInclusive: boolp(false),
			},
		},
		{
			"range named bound is an alias, not a guess",
			">=latest",
			Spec{Kind: SpecKindAlias},
		},
		{"alias lts name", "lts/iron", Spec{Kind: SpecKindAlias}},
		{"alias lts star", "lts/*", Spec{Kind: SpecKindAlias}},
		{"alias node", "node", Spec{Kind: SpecKindAlias}},
		{"alias system", "system", Spec{Kind: SpecKindAlias}},
		{"alias stable", "stable", Spec{Kind: SpecKindAlias}},
		{"alias dated nightly", "nightly-2024-01-01", Spec{Kind: SpecKindAlias}},
		{"alias latest with constraint is not a range", "latest:^1.5", Spec{Kind: SpecKindAlias}},
		{"alias bare wildcard", "*", Spec{Kind: SpecKindAlias}},
		{"unresolved shell variable", "${X}", Spec{Kind: SpecKindUnresolved}},
		{"unresolved bare dollar", "$NODE_VERSION", Spec{Kind: SpecKindUnresolved}},
		{"unresolved ci expression", "${{ inputs.node-version }}", Spec{Kind: SpecKindUnresolved}},
		{"unresolved template", "{{ node_version }}", Spec{Kind: SpecKindUnresolved}},
		{"unresolved inside a version", "20.${MINOR}", Spec{Kind: SpecKindUnresolved}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Classify(%q) = %s, want %s", tc.raw, describe(got), describe(tc.want))
			}
		})
	}
}

// describe renders a Spec as JSON so a failure shows dereferenced pointers.
func describe(s Spec) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return err.Error()
	}
	return string(raw)
}
