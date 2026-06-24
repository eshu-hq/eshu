// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gomod parses Go module manifests (`go.mod`) and module-checksum
// files (`go.sum`) into repository dependency facts.
//
// Parse reads one file at a time and returns the parser payload shape the
// parent parser engine expects. A `go.mod` file emits one
// `config_kind: "dependency"` row per require entry — direct and indirect
// stay on separate sections so the supply-chain consumption reducer can
// scope to source-declared dependencies. Replace and exclude directives are
// emitted as `config_kind: "dependency_replace"` and
// `config_kind: "dependency_exclude"` rows so they remain auditable without
// being admitted as consumption.
//
// A `go.sum` file emits `config_kind: "dependency_checksum"` rows tagged
// `ambiguous: true`. go.sum records every module version any tool has ever
// verified; on its own it cannot prove which version is currently selected,
// so the reducer must not treat these rows as exact observed installed
// evidence. The parser preserves the verbatim h1 hash and a `checksum_kind`
// of `"module"` or `"gomod"` for cross-file corroboration.
//
// Malformed go.mod files produce a `gomod_state` envelope with the upstream
// modfile parser error and zero dependency rows; missing or ambiguous
// module evidence stays missing rather than degrading to silently affected
// or silently safe.
package gomod
