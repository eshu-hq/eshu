// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"

// NoteParsedSwiftCandidate stores guarded native Package.swift evidence
// without a body.
func NoteParsedSwiftCandidate(candidates map[string]Candidate, relativePath string, extraction runtimedeclaration.Extraction) {
	if runtimedeclaration.ClassifyPath(relativePath) != runtimedeclaration.FileManifest {
		return
	}
	if extraction.Len() == 0 && !extraction.Skip().Skipped() {
		return
	}
	candidates[relativePath] = Candidate{Manifest: true, Extraction: extraction}
}

// NoteUnavailableSwiftCandidate records a malformed Package.swift for retry.
func NoteUnavailableSwiftCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.UnavailableSwiftExtraction(byteSize)}
}

// NoteCappedSwiftCandidate records an over-cap Package.swift without
// retaining its body.
func NoteCappedSwiftCandidate(candidates map[string]Candidate, relativePath string, byteSize int) {
	candidates[relativePath] = Candidate{Manifest: true, Extraction: runtimedeclaration.CappedSwiftExtraction(byteSize)}
}
