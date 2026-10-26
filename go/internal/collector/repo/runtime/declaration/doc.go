// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration turns the version-pin files a repository commits, the
// engines, volta and packageManager sections of its package.json files, the
// go and toolchain directives of its go.mod files, the FROM base-image tags
// of its Dockerfiles and the setup with: version inputs of its GitHub Actions
// workflows into "runtime_declaration.version" facts (issue #8815, epic
// #8807; Dockerfile and workflow sources issue #8818).
//
// The package is the producer half of the contract defined by
// facts.RuntimeDeclarationVersionFactKind and the typed
// sdk/go/factschema/runtimedeclaration/v1.Version payload. Four pieces
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
//     .swift-version, rust-toolchain, rust-toolchain.toml). Credential
//     carriers such as .npmrc are not on it, so they emit nothing. A
//     whitespace-split value keeps a spaced comparator with its version
//     (">= 18" is one declaration) and reports a value with an operator-only
//     token left over ("18 - 20") as one alias, never as fragments.
//   - ClassifyPath, Extract and EmitExtraction read one package.json, go.mod,
//     Dockerfile or workflow body. source_kind names the section
//     (package_json_engines, package_json_volta, package_json_package_manager,
//     go_mod_go, go_mod_toolchain, dockerfile_from, workflow_setup_version).
//     The package.json scan is a bounded, allocation-free JSON walk that fails
//     closed on malformed input; the Dockerfile reader reuses the Dockerfile
//     parser's ARG substitution; the workflow reader is a line-preserving YAML
//     node walk over job and step with: maps. Every value passes a credential
//     guard before it can become a raw_spec: a value that is not short ASCII
//     version text is dropped and counted, never emitted.
//   - ScopeDir reports the declaring file's directory in the contract's form.
//
// This package does not select which files reach it, decide scope or
// generation identifiers, or call the reducer or query packages. The Git
// collector (go/internal/collector/repo/git/runtime/declaration) accumulates
// pin file bodies and manifest extractions during its content stream and emits
// them once per generation. The reducer projection (#8816) and query tool
// (#8817) consume the facts later.
package declaration
