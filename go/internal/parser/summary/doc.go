// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package summary builds durable, content-versioned function summaries and
// recomposes them incrementally: changing one function recomputes only the
// summaries that transitively depend on it.
//
// A summary captures a function's structural taint effects (the TITO model:
// which parameters flow to the return, to a sink, or into a callee's argument,
// and which internal sources flow to the return) independently of its callees'
// versions. The content version of a summary is
// hash(own structural facts ∪ sorted versions of callees outside its
// strongly-connected component). Excluding same-component callee versions from
// the hash keeps the version fixpoint well-defined under recursion, so Upsert
// always terminates.
//
// Identity is generation-independent: a FunctionID is derived from durable
// attributes (repository, package, receiver, name), never from a commit or
// generation, so a summary's identity is stable across runs and only its version
// changes when its effects or dependencies change. That stable identity is what
// makes the Store reloadable across runs (via Snapshot and Load) and lets a
// reducer recompose only what changed — the incremental piece reference designs
// model but leave unwired.
//
// The Store is not safe for concurrent use; a reducer serializes access or
// shards the store by conflict key.
package summary
