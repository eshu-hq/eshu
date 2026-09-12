// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"strings"
	"testing"
)

// TestIdentityHashUsesTheSha256Prefix proves IdentityHash's wire format
// (go/cmd/api/seed_initial_admin.go, go/cmd/api/seed_initial_admin_helpers.go,
// and go/internal/cli/admin/credential.go all parse this exact convention
// through root's local_identity_alias.go forwarder): a non-empty input hashes
// to "sha256:<64 lowercase hex chars>".
//
// Before the move (#6642) IdentityHash was a thin exported wrapper around an
// unexported localIdentityHash that did the actual work; the two were always
// the same computation, so they were merged into this one function (see
// IdentityHash's doc comment in helpers.go) instead of carrying two exported
// names forward. This test pins the merged function's output shape now that
// there is only one implementation to check.
func TestIdentityHashUsesTheSha256Prefix(t *testing.T) {
	got := IdentityHash("owner")
	const prefix = "sha256:"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("IdentityHash(%q) = %q, want prefix %q", "owner", got, prefix)
	}
	hex := strings.TrimPrefix(got, prefix)
	if len(hex) != 64 {
		t.Fatalf("IdentityHash(%q) hex part = %q (len %d), want 64 lowercase hex chars", "owner", hex, len(hex))
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("IdentityHash(%q) = %q, contains non-lowercase-hex char %q", "owner", got, c)
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
