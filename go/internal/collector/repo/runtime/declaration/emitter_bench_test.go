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
