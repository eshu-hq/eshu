// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration is the Git collector hook for
// "runtime_declaration.version" facts (issue #8815).
//
// It sits between the Git collector's content stream and the sibling
// go/internal/collector/repo/runtime/declaration parser. NoteRuntimeDeclarationCandidate
// records every admitted version-pin file (.nvmrc, .tool-versions, ...) and
// every package.json and go.mod outside a dependency directory, at any depth,
// while the stream walks content files on both its two-phase and legacy
// branches. A pin body over the parser's byte cap is recorded by size only, so
// the walk never retains it. A manifest is parsed on the spot and only its few
// declarations are kept, never its body. EmitRuntimeDeclarationFactsForCandidates
// then sends the facts once, in path order, after both branches close.
//
// A file over the parser's per-file byte or token cap, and a package.json that
// is not readable JSON, emits nothing and is reported through the onSkip
// callback so the collector can log it. A manifest that lost values to the
// credential guard emits the rest and is reported the same way.
//
// Each file is independent, so no winner resolution is needed and a delta
// generation re-emits only the files it changed.
package declaration
