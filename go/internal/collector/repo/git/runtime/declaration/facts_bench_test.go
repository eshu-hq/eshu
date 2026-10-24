// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"testing"
)

// BenchmarkNoteRuntimeDeclarationCandidateNonPin measures the per-content-file
// cost the stream pays for every file that is not a version-pin file, which is
// nearly every file in a repository: one base-name map lookup and no
// allocation.
func BenchmarkNoteRuntimeDeclarationCandidateNonPin(b *testing.B) {
	paths := make([]string, 1024)
	for i := range paths {
		paths[i] = fmt.Sprintf("services/svc%d/internal/handler_%d.go", i%32, i)
	}
	candidates := map[string]Candidate{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NoteRuntimeDeclarationCandidate(candidates, paths[i%len(paths)], "package x\n")
	}
	if len(candidates) != 0 {
		b.Fatalf("non-pin paths were recorded: %d", len(candidates))
	}
}
