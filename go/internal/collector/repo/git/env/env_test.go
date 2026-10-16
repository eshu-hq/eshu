// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package env

import "testing"

// TestCoercionsArePure spot-checks the shared coercions the config loader
// moved here: first-non-empty wins, nil formats empty, and truthy parses
// case-insensitively.
func TestCoercionsArePure(t *testing.T) {
	t.Parallel()

	if got := FirstNonEmpty(" ", "a", "b"); got != "a" {
		t.Fatalf("FirstNonEmpty() = %q, want a", got)
	}
	if got := FirstNonEmpty(" ", "\t"); got != "" {
		t.Fatalf("FirstNonEmpty() = %q, want empty", got)
	}
	if got := StringValue(nil); got != "" {
		t.Fatalf("StringValue(nil) = %q, want empty", got)
	}
	if got := StringValue(" x "); got != "x" {
		t.Fatalf("StringValue() = %q, want trimmed", got)
	}
	for _, truthy := range []string{"1", "true", "YES", "On"} {
		if !BoolFromEnv(truthy) {
			t.Fatalf("BoolFromEnv(%q) = false, want true", truthy)
		}
	}
	for _, falsy := range []string{"", "0", "false", "no"} {
		if BoolFromEnv(falsy) {
			t.Fatalf("BoolFromEnv(%q) = true, want false", falsy)
		}
	}
}
