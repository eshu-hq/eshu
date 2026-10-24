// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration is the Git collector hook for
// "runtime_declaration.version" facts (issue #8815).
//
// It sits between the Git collector's content stream and the sibling
// go/internal/collector/repo/runtime/declaration parser. NoteRuntimeDeclarationCandidate
// records the body of every admitted version-pin file (.nvmrc, .tool-versions,
// ...) at any depth while the stream walks content files on both its
// two-phase and legacy branches. EmitRuntimeDeclarationFactsForCandidates then
// sends the facts once, in path order, after both branches close.
//
// Each pin file is independent, so no winner resolution is needed and a delta
// generation re-emits only the pin files it changed.
package declaration
