// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gomod parses Go module manifests (`go.mod`), module-checksum
// files (`go.sum`), and workspace files (`go.work`) into repository
// dependency facts.
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
// A `go.work` file emits one `config_kind: "workspace_use"` row per `use`
// entry. The row `value` is the raw path, untrimmed; the `name` is the raw
// path too, except that a path blank after trimming (`use ""`, the workspace
// root) is named by its Go-quoted spelling because the content writers reject
// a blank entity name. `godebug` directives are accepted and not recorded. A
// go.work reuses the go.mod `config_kind: "dependency_replace"` row for each
// workspace `replace`. A workspace declares no requirements, so it emits no
// `dependency` rows, and its `gomod_state` carries `go_version`, `toolchain`,
// `use_count`, `replace_count`, and `replaced_modules` but no `module_path`: a
// workspace is not a module declaration. `go.work.sum` is not parsed.
//
// Malformed go.mod and go.work files produce a `gomod_state` envelope with the
// upstream modfile parser error and zero dependency rows; missing or ambiguous
// module evidence stays missing rather than degrading to silently affected
// or silently safe.
package gomod
