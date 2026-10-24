// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// toolVersionsWithTokens builds a .tool-versions body of n version tokens
// spread over lines of 16 tokens each.
func toolVersionsWithTokens(n int) string {
	var b strings.Builder
	for emitted := 0; emitted < n; {
		b.WriteString("nodejs")
		for k := 0; k < 16 && emitted < n; k++ {
			b.WriteString(" 20.1.0")
			emitted++
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestEmitFileCapsTokensPerFile proves a file at the cap emits every token, and
// a file one token over the cap emits nothing and reports how many tokens it
// skipped, so an operator can see the skip and a partial list is never
// presented as the whole file.
func TestEmitFileCapsTokensPerFile(t *testing.T) {
	t.Parallel()

	atCap, skipped := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", toolVersionsWithTokens(MaxTokensPerFile))
	if len(atCap) != MaxTokensPerFile || skipped != 0 {
		t.Fatalf("at cap: %d envelopes, skipped %d; want %d, 0", len(atCap), skipped, MaxTokensPerFile)
	}
	over, skipped := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", toolVersionsWithTokens(MaxTokensPerFile+1))
	if len(over) != 0 || skipped != MaxTokensPerFile+1 {
		t.Fatalf("over cap: %d envelopes, skipped %d; want 0, %d", len(over), skipped, MaxTokensPerFile+1)
	}
}

// TestEmitDropsAFileOverTheTokenCap proves the Emit wrapper applies the same
// cap, so no caller can bypass it.
func TestEmitDropsAFileOverTheTokenCap(t *testing.T) {
	t.Parallel()

	if got := Emit(testContext(".tool-versions"), "repo-1", ".tool-versions", toolVersionsWithTokens(MaxTokensPerFile+1)); len(got) != 0 {
		t.Fatalf("Emit over cap = %d envelopes, want 0", len(got))
	}
}
