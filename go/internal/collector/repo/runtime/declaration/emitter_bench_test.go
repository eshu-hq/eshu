// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

// BenchmarkEmitToolVersions measures emission for a typical multi-tool
// .tool-versions file: eight tokens on six lines. A repository carries a handful
// of pin files, so this bounds the added cost per generation.
func BenchmarkEmitToolVersions(b *testing.B) {
	body := "# managed by asdf\nnodejs 20.1.0 18.0.0\nruby 3.2 system\ngolang 1.22.1\npython 3.12\nrust stable\nterraform ${TF_VERSION}\n"
	ctx := testContext(".tool-versions")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", ".tool-versions", body); len(got) != 8 {
			b.Fatalf("Emit = %d envelopes, want 8", len(got))
		}
	}
}

// BenchmarkEmitAtTokenCap measures a pin file at MaxTokensPerFile, the most a
// single file can emit.
func BenchmarkEmitAtTokenCap(b *testing.B) {
	body := toolVersionsWithTokens(MaxTokensPerFile)
	ctx := testContext(".tool-versions")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", ".tool-versions", body); len(got) != MaxTokensPerFile {
			b.Fatalf("Emit = %d envelopes, want %d", len(got), MaxTokensPerFile)
		}
	}
}

// BenchmarkEmitOverTokenCapUnderByteCap measures the count-only skip path: a
// 60 KiB file of one-byte tokens is over MaxTokensPerFile but under
// MaxBytesPerFile, so it is tokenized and counted but no declaration past the
// cap is stored. allocs/op and B/op stay flat in the token count.
func BenchmarkEmitOverTokenCapUnderByteCap(b *testing.B) {
	body := hostileToolVersions(60 << 10)
	ctx := testContext(".tool-versions")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, skip := EmitFile(ctx, "repo-1", ".tool-versions", body); len(got) != 0 || skip.Reason != SkipReasonTokenCap {
			b.Fatalf("EmitFile = %d envelopes, skip %+v", len(got), skip)
		}
	}
}

// BenchmarkEmitOverByteCap measures the byte-cap skip path for a 10 MB hostile
// file: the length check rejects it before any parsing, so B/op and allocs/op
// are zero regardless of the body size.
func BenchmarkEmitOverByteCap(b *testing.B) {
	body := hostileToolVersions(10 << 20)
	ctx := testContext(".tool-versions")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, skip := EmitFile(ctx, "repo-1", ".tool-versions", body); len(got) != 0 || skip.Reason != SkipReasonByteCap {
			b.Fatalf("EmitFile = %d envelopes, skip %+v", len(got), skip)
		}
	}
}
