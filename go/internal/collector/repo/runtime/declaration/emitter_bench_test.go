// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

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

// BenchmarkEmitPackageJSON measures one typical package.json with all three
// declaring sections: five declarations.
func BenchmarkEmitPackageJSON(b *testing.B) {
	ctx := testContext("package.json")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", "package.json", prettyPackageJSON); len(got) != 5 {
			b.Fatalf("Emit = %d envelopes, want 5", len(got))
		}
	}
}

// BenchmarkExtractPackageJSONNoDeclaration measures the common case: a
// package.json with none of the three sections, which the walk reads and drops.
func BenchmarkExtractPackageJSONNoDeclaration(b *testing.B) {
	body := "{\n  \"name\": \"lib\",\n  \"version\": \"1.0.0\",\n  \"main\": \"index.js\",\n" +
		"  \"scripts\": {\"build\": \"tsc\", \"test\": \"jest\"},\n  \"dependencies\": {\"express\": \"^4.18.2\", \"zod\": \"^3.22.4\"}\n}\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e, ok := Extract("lib/package.json", body); !ok || e.Len() != 0 {
			b.Fatalf("Extract = %d declarations, ok %v", e.Len(), ok)
		}
	}
}

// BenchmarkEmitPackageJSONAtByteCap measures the largest body the emitter
// reads: a valid package.json just under MaxManifestBytesPerFile. The scan
// allocates for none of the skipped values, so B/op stays flat in the body.
func BenchmarkEmitPackageJSONAtByteCap(b *testing.B) {
	body := hostilePackageJSON(MaxManifestBytesPerFile - 1024)
	ctx := testContext("package.json")
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", "package.json", body); len(got) != 2 {
			b.Fatalf("Emit = %d envelopes, want 2", len(got))
		}
	}
}

// BenchmarkEmitGoMod measures one typical go.mod: a handful of requires around
// the go and toolchain directives.
func BenchmarkEmitGoMod(b *testing.B) {
	body := realisticGoMod
	ctx := testContext("go.mod")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", "go.mod", body); len(got) != 2 {
			b.Fatalf("Emit = %d envelopes, want 2", len(got))
		}
	}
}

// BenchmarkEmitGoModLarge measures a go.mod with 400 require lines, the size
// of a large service: the require and replace lines are scanned and dropped
// without an allocation each.
func BenchmarkEmitGoModLarge(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("module example.com/big\n\ngo 1.22.1\n\ntoolchain go1.22.3\n\nrequire (\n")
	for i := 0; i < 400; i++ {
		sb.WriteString("\texample.com/some/module/path")
		sb.WriteString(strings.Repeat("x", i%9))
		sb.WriteString(" v1.2.3 // indirect\n")
	}
	sb.WriteString(")\n")
	body, ctx := sb.String(), testContext("go.mod")
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := Emit(ctx, "repo-1", "go.mod", body); len(got) != 2 {
			b.Fatalf("Emit = %d envelopes, want 2", len(got))
		}
	}
}
