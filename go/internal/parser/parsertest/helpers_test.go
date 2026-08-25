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
