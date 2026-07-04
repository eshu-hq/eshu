// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shared contains dependency-safe helper contracts for language-owned
// parser packages.
//
// The package exists so child parser packages can share payload helpers,
// tree-sitter node helpers, source reads, small value utilities, and parser
// options without importing the parent parser dispatcher. Its helpers are
// language-neutral and preserve the payload shape consumed by collector
// materialization. WalkNamed uses one tree-sitter cursor per traversal so
// repo-scale parser pre-scans do not allocate a NamedChildren slice at every
// node. Go semantic-root options preserve the empty-method-list
// convention for imported package interface escapes without known method sets,
// explicit method lists for same-repository package contracts, and qualified
// method-call roots for imported package receiver types. Options carry the
// opt-in value-flow gate plus the stable repository and package identity
// required for durable FunctionIDs, while bucket sorting keeps the parent
// parser's line-number then name ordering contract.
//
// CyclomaticComplexity is the shared McCabe complexity walker. It counts
// decision points from a BranchNodeSet so every tree-sitter language computes
// real complexity from data tables rather than per-language traversal code.
//
// ReadSource is the single physical-read chokepoint every child parser package
// calls. PrimeSource/ClearSource let Engine.ParsePath cache one file's bytes
// for the duration of a single call so the dispatched language parser and the
// engine's post-parse content-metadata inference share one os.ReadFile instead
// of each reading independently. Callers of PrimeSource MUST pair it with
// ClearSource (typically via defer) so the cache entry does not leak past the
// call that primed it and is not left stale for a later, non-overlapping
// parse of the same path. The cache is a mutex-guarded, reference-counted map
// keyed by absolute path: PrimeSource follows first-writer-wins (a second
// concurrent prime for the same path increments the refcount but never
// replaces the already-cached bytes) and ClearSource only deletes the entry
// once every reference has been released. This makes concurrent ParsePath
// calls on the SAME path torn-read-safe by design: they intentionally share
// one consistent snapshot for as long as any of them are in flight, and
// neither call can overwrite the other's snapshot or delete it out from
// under the other mid-parse.
package shared
