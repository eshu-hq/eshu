// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration turns the version-pin files a repository commits
// into "runtime_declaration.version" facts (issue #8815, epic #8807).
//
// The package is the producer half of the contract defined by
// facts.RuntimeDeclarationVersionFactKind and the typed
// sdk/go/factschema/runtimedeclaration/v1.Version payload. Three pieces
// compose it:
//
//   - Classify reads one declared version string into a Spec: exact, partial,
//     range, alias or unresolved. It never guesses. A named reference such as
//     "lts/iron", "node" or "system" is an alias and is never resolved offline;
//     a string holding an unexpanded placeholder such as "${X}" is
//     unresolved; only a numeric version or a comparator expression yields
//     parsed numbers.
//   - SourceKindForPath and Emit read one admitted pin file body into one fact
//     per (line, token). The admitted base names are an explicit list (.nvmrc,
//     .node-version, .python-version, .ruby-version, .java-version,
//     .go-version, .terraform-version, .tool-versions, .sdkmanrc,
//     rust-toolchain, rust-toolchain.toml). Credential carriers such as .npmrc
//     are not on it, so they emit nothing.
//   - ScopeDir reports the declaring file's directory in the contract's form.
//
// This package does not select which files reach it, decide scope or
// generation identifiers, or call the reducer or query packages. The Git
// collector (go/internal/collector/repo/git/runtime/declaration) accumulates
// candidate bodies during its content stream and calls Emit once per
// generation. package.json, go.mod, Dockerfile and CI sources are separate
// changes; the reducer projection (#8816) and query tool (#8817) consume the
// facts later.
package declaration
