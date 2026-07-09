// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestIdentityHashMatchesUnexportedImplementation proves the exported
// IdentityHash (the form go/cmd/api/seed_initial_admin.go and
// go/cmd/eshu/admin_initial_credential.go call) produces byte-identical
// output to the package's own internal localIdentityHash, so every caller
// inside and outside this package always agrees on the same hash for the
// same input.
func TestIdentityHashMatchesUnexportedImplementation(t *testing.T) {
	cases := []string{"owner", "  padded  ", "", "bcrypt", "tenant-a:workspace-a"}
	for _, value := range cases {
		if got, want := IdentityHash(value), localIdentityHash(value); got != want {
			t.Fatalf("IdentityHash(%q) = %q, want %q (localIdentityHash)", value, got, want)
		}
	}
}

// TestIdentityHashIsDeterministicAndTrimsWhitespace proves the exported
// contract callers outside this package rely on: same input always hashes
// to the same "sha256:<hex>" value, leading/trailing whitespace never
// changes the hash, and an empty (or whitespace-only) input hashes to "".
func TestIdentityHashIsDeterministicAndTrimsWhitespace(t *testing.T) {
	if got := IdentityHash("owner"); got == "" {
		t.Fatal("IdentityHash(\"owner\") returned empty")
	}
	if got, want := IdentityHash("  owner  "), IdentityHash("owner"); got != want {
		t.Fatalf("IdentityHash did not trim whitespace: %q != %q", got, want)
	}
	if got := IdentityHash(""); got != "" {
		t.Fatalf("IdentityHash(\"\") = %q, want \"\"", got)
	}
	if got := IdentityHash("   "); got != "" {
		t.Fatalf("IdentityHash(\"   \") = %q, want \"\"", got)
	}
}
