// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package authsafe

import "testing"

// One suite for the check that used to live in three places. Each rejection
// case names the escape it closes, so a future tightening can tell which cases
// are load-bearing from which are incidental.
func TestReturnPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain path", "/dashboard", "/dashboard"},
		{"path with query", "/dashboard?tab=1", "/dashboard?tab=1"},
		{"path with fragment", "/dashboard#top", "/dashboard#top"},
		{"root", "/", "/"},
		{"surrounding whitespace is trimmed, not rejected", "  /dashboard  ", "/dashboard"},

		{"empty", "", ""},
		{"whitespace only", "   ", ""},

		// Leaving the origin.
		{"absolute https URL", "https://evil.test/pwn", ""},
		{"absolute http URL", "http://evil.test/pwn", ""},
		{"protocol-relative host", "//evil.test/pwn", ""},
		{"protocol-relative with whitespace", "  //evil.test  ", ""},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"mailto scheme", "mailto:a@b.test", ""},
		{"bare relative path", "dashboard", ""},

		// Header injection.
		{"CR", "/dashboard\rLocation: https://evil.test", ""},
		{"LF", "/dashboard\nLocation: https://evil.test", ""},
		{"CRLF", "/dashboard\r\nSet-Cookie: a=b", ""},
		{"TAB", "/dash\tboard", ""},

		// Documented non-goal: traversal stays inside this origin, so it is the
		// router's problem rather than the redirect's. Pinned so that if #5388's
		// suggested ".." tightening ever lands, this expectation is what changes
		// — deliberately, in one place.
		{"dot-dot traversal is allowed today", "/app/../admin", "/app/../admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ReturnPath(tt.in); got != tt.want {
				t.Errorf("ReturnPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A rejected value must be distinguishable from a valid one by the caller, and
// "" is the only rejection signal. A caller that redirected to the returned
// value without checking would send the browser to "" — so the contract is that
// callers substitute their configured default.
func TestReturnPathRejectionIsAlwaysEmptyString(t *testing.T) {
	t.Parallel()

	for _, hostile := range []string{
		"https://evil.test", "//evil.test", "javascript:alert(1)",
		"/x\r\nSet-Cookie: a=b", "", "   ", "relative",
	} {
		if got := ReturnPath(hostile); got != "" {
			t.Errorf("ReturnPath(%q) = %q, want the empty rejection signal", hostile, got)
		}
	}
}
