// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rust parses Rust source files into Eshu parser payloads.
//
// Parse receives a caller-owned tree-sitter parser and emits deterministic
// buckets for functions, type declarations, modules, traits, impl blocks,
// expanded imports, constants, statics, type aliases, macro definitions and
// invocations, calls, selected dead-code root evidence, attributes, derives,
// conditional derives, nested field and enum-variant annotations, generic
// parameter evidence, trait declaration context, impl-context evidence, direct
// parameter receiver type evidence, and structured where-clause evidence. Bare
// Rust main roots require a Cargo entrypoint path such as src/main.rs, src/bin,
// examples, or build.rs, or direct runtime macro evidence, so library functions
// named main do not become roots by name alone. Public API roots require exact pub
// visibility, and benchmark roots require file-local Criterion identifier
// targets or direct benchmark attributes; generated or expression-based
// benchmark targets remain raw macro evidence. Trait declaration methods carry
// their trait context. Trait implementation methods, including unsafe impl
// blocks, carry rust.trait_impl_method root evidence with their trait context so
// dead-code callers can keep runtime-dispatched trait surfaces conservative.
// Module declaration path candidates stay relative to the current file
// directory instead of probing the filesystem, except explicit path attributes,
// which replace the candidate list with the declared path;
// ResolveModuleRowFileCandidates exposes the same
// candidate calculation without filesystem probing. Bounded Cargo helpers scan
// package names, workspace members, feature names, default feature members, and
// target cfg dependency sections for later cfg resolution work, while ignoring
// dynamic TOML instead of guessing. The same package parses Cargo.toml and
// Cargo.lock exact-name inputs into dependency evidence rows: manifests preserve
// direct ranges, dev/build/runtime scope, target-specific sections,
// workspace-inherited dependencies, and renamed package identity; lockfiles
// preserve exact crate versions and dependency paths only when the lock graph
// proves reachability from a workspace root package, including source-qualified
// edge resolution when Cargo names a parenthesized source. The parent parser engine may
// probe repo-bounded module candidates and attach bounded
// module_resolution_status metadata when parsing a concrete repo path. Existing
// files outside the repo root are not treated as resolved modules. Item
// attributes may be
// multiline or share the item line, while nested field and enum-variant
// attributes stay on owned annotation rows. Cfg-gated items and macro-origin
// module/import rows name exactness blockers so dead-code callers can explain
// why Rust remains derived. Direct method calls on function parameters may carry
// inferred_obj_type; local variables, fields, expressions, and macros do not.
// Impl targets keep the receiver type and trim any trailing where clause.
// PreScan derives repository symbol names from the same payload path so parent
// parser pre-scan and full parse agree. The package
// preserves raw attributes and generic clauses as evidence without inferring
// reachability from arbitrary macro expansion, derives, conditional attributes,
// Cargo feature selection, manifest-to-lockfile feature resolution, or cfg
// evaluation.
package rust
