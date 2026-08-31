// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parsertest

import (
	"reflect"
	"testing"
)

func TestAssertBucketItemByNameReturnsMatchingItem(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"name":                 "handler",
		"dead_code_root_kinds": []string{"c.callback_argument_target"},
	}
	payload := map[string]any{
		"functions": []map[string]any{
			{"name": "other"},
			want,
		},
	}

	if got := AssertBucketItemByName(t, payload, "functions", "handler"); !reflect.DeepEqual(got, want) {
		t.Fatalf("matched item = %#v, want %#v", got, want)
	}
}

func TestAssertStringSliceContainsAcceptsExistingValue(t *testing.T) {
	t.Parallel()

	item := map[string]any{
		"dead_code_root_kinds": []string{
			"c.signal_handler",
			"c.callback_argument_target",
		},
	}

	AssertStringSliceContains(t, item, "dead_code_root_kinds", "c.callback_argument_target")
}

func TestAssertPrescanContainsAcceptsExistingPath(t *testing.T) {
	t.Parallel()

	importsMap := map[string][]string{
		"Widget": {"/repo/src/widget.cpp", "/repo/src/widget.hpp"},
	}

	AssertPrescanContains(t, importsMap, "Widget", "/repo/src/widget.hpp")
}

func TestAssertStringSliceNotContainsAcceptsMissingValue(t *testing.T) {
	t.Parallel()

	item := map[string]any{
		"dead_code_root_kinds": []string{"swift.protocol_method"},
	}

	AssertStringSliceNotContains(t, item, "dead_code_root_kinds", "swift.constructor")
}

func TestAssertStringSliceNotContainsAcceptsMissingField(t *testing.T) {
	t.Parallel()

	AssertStringSliceNotContains(t, map[string]any{}, "dead_code_root_kinds", "swift.constructor")
}

func TestAssertStringSliceEqualsAcceptsMatchingOrder(t *testing.T) {
	t.Parallel()

	item := map[string]any{"args": []string{"id", "name"}}

	AssertStringSliceEquals(t, item, "args", []string{"id", "name"})
}

func TestAssertIntFieldValueAcceptsMatchingInt(t *testing.T) {
	t.Parallel()

	item := map[string]any{"line_number": 26}

	AssertIntFieldValue(t, item, "line_number", 26)
}

func TestAssertFunctionByNameAndClassReturnsMatchingItem(t *testing.T) {
	t.Parallel()

	want := map[string]any{"name": "run", "class_context": "Worker"}
	payload := map[string]any{
		"functions": []map[string]any{
			{"name": "run", "class_context": "Runnable"},
			want,
		},
	}

	if got := AssertFunctionByNameAndClass(t, payload, "run", "Worker"); !reflect.DeepEqual(got, want) {
		t.Fatalf("matched item = %#v, want %#v", got, want)
	}
}

func TestStringSliceNotContainsRejectsMalformedField(t *testing.T) {
	// A present field of the wrong type used to return silently, which let a
	// negative assertion pass over a fixture whose shape had drifted. The
	// predicate is tested directly so the failure path is proven, not assumed.
	for name, value := range map[string]any{
		"string instead of slice": "swift.constructor",
		"slice of any":            []any{"swift.constructor"},
		"nil value":               nil,
	} {
		t.Run(name, func(t *testing.T) {
			if err := stringSliceNotContains(map[string]any{"kinds": value}, "kinds", "swift.constructor"); err == nil {
				t.Fatalf("malformed %s passed the negative assertion; want an error", name)
			}
		})
	}
}

func TestStringSliceNotContainsRejectsPresentValue(t *testing.T) {
	item := map[string]any{"kinds": []string{"swift.main_function", "swift.constructor"}}
	if err := stringSliceNotContains(item, "kinds", "swift.constructor"); err == nil {
		t.Fatal("a slice containing the value passed the negative assertion; want an error")
	}
}

func TestStringSliceNotContainsAcceptsAbsentFieldAndMissingValue(t *testing.T) {
	if err := stringSliceNotContains(map[string]any{}, "kinds", "swift.constructor"); err != nil {
		t.Fatalf("absent field must pass: %v", err)
	}
	item := map[string]any{"kinds": []string{"swift.main_function"}}
	if err := stringSliceNotContains(item, "kinds", "swift.constructor"); err != nil {
		t.Fatalf("slice without the value must pass: %v", err)
	}
}
