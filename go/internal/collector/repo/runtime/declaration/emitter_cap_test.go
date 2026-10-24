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

	atCap, skip := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", toolVersionsWithTokens(MaxTokensPerFile))
	if len(atCap) != MaxTokensPerFile || skip.Skipped() {
		t.Fatalf("at cap: %d envelopes, skip %+v; want %d, none", len(atCap), skip, MaxTokensPerFile)
	}
	body := toolVersionsWithTokens(MaxTokensPerFile + 1)
	over, skip := EmitFile(testContext(".tool-versions"), "repo-1", ".tool-versions", body)
	want := Skip{Reason: SkipReasonTokenCap, ByteSize: len(body), TokenCount: MaxTokensPerFile + 1}
	if len(over) != 0 || skip != want {
		t.Fatalf("over cap: %d envelopes, skip %+v; want 0, %+v", len(over), skip, want)
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

// TestEmitDecodesInvalidUTF8ToTheReplacementRune pins a documented behaviour:
// the payload is JSON, which cannot carry an invalid byte, so an invalid UTF-8
// byte in a pin file reaches raw_spec as U+FFFD. raw_spec is verbatim only for
// valid UTF-8.
func TestEmitDecodesInvalidUTF8ToTheReplacementRune(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, ".nvmrc", "20\xff1\n")
	if len(got) != 1 || got[0].RawSpec != "20�1" {
		t.Fatalf("raw_spec = %+v, want one fact with %q", got, "20�1")
	}
}
