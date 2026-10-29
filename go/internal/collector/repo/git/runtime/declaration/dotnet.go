// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"

// NoteParsedDotNetCandidate stores guarded parser evidence without a body.
func NoteParsedDotNetCandidate(candidates map[string]Candidate, relativePath string, extraction runtimedeclaration.Extraction) {
	if runtimedeclaration.ClassifyPath(relativePath) != runtimedeclaration.FileManifest {
		return
	}
	if extraction.Len() == 0 && !extraction.Skip().Skipped() {
		return
	}
	candidates[relativePath] = Candidate{Manifest: true, Extraction: extraction}
}

// NoteUnavailableDotNetCandidate records a parser miss for a retry.
func NoteUnavailableDotNetCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.UnavailableDotNetExtraction(byteSize)}
}

// NoteCappedDotNetCandidate records an over-cap file without retaining its body.
func NoteCappedDotNetCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.CappedDotNetExtraction(byteSize)}
}
