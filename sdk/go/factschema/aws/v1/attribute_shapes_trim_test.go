// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "testing"

// TestAttributeStringTrimsPaddedValue guards #5243 (codex review on #4631): the
// typed accessors must preserve the pre-typing payloadString normalization
// (strings.TrimSpace), so a valid-but-padded scalar like " CUSTOMER " normalizes
// to "CUSTOMER" — otherwise the downstream key_manager == "CUSTOMER" check would
// flip a customer-managed KMS key to AWS-managed.
func TestAttributeStringTrimsPaddedValue(t *testing.T) {
	t.Parallel()

	got, err := attributeString(map[string]any{"key_manager": "  CUSTOMER  "}, "key_manager", "key_manager")
	if err != nil {
		t.Fatalf("attributeString returned an unexpected error: %v", err)
	}
	if got != "CUSTOMER" {
		t.Fatalf("attributeString did not trim padding: got %q, want %q", got, "CUSTOMER")
	}
}

// TestAttributeStringSliceTrimsAndDropsEmpty guards #5243: slice attributes must
// trim each entry and drop empty-after-trim entries, matching the pre-typing
// payloadStrings normalization.
func TestAttributeStringSliceTrimsAndDropsEmpty(t *testing.T) {
	t.Parallel()

	got, err := attributeStringSlice(
		map[string]any{"role_arns": []any{"  arn:aws:iam::1:role/a  ", "   ", "arn:aws:iam::1:role/b"}},
		"role_arns", "role_arns",
	)
	if err != nil {
		t.Fatalf("attributeStringSlice returned an unexpected error: %v", err)
	}
	want := []string{"arn:aws:iam::1:role/a", "arn:aws:iam::1:role/b"}
	if len(got) != len(want) {
		t.Fatalf("attributeStringSlice did not trim/drop-empty: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attributeStringSlice entry %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
