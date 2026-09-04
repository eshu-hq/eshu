// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package searchvector runs the reducer's search-vector build sweep: a
// side-runner goroutine that builds derived embedding-vector rows for active
// curated search documents, feeding the semantic/hybrid search read path
// (issue #6061; originally issue #4233/#4430/#4673/#4885).
//
// [SearchVectorBuildRunner] owns the sweep loop. Each bounded sweep lists
// pending active scopes through the [SearchVectorBuildPendingLister] port,
// builds vectors through the [SearchVectorBuilder] port (or its
// [SearchVectorBatchBuilder] extension when the wired builder implements it),
// and continues through independent per-scope build failures, returning a
// joined error for operator visibility. The runner writes no graph truth: it
// has no canonical writer, ledger, or retraction path, only the derived
// vector-store side effect its builder port performs.
//
// A sweep that selects pending scopes but produces no durable output (no
// finalized scopes, documents, vectors, or disabled rows) backs off on the
// configured poll interval instead of re-looping immediately
// ([searchVectorBuildSweepMadeProgress]) — without this, a never-draining
// pending set hot-loops the sweep and pins Postgres with useless query load
// (issue #4885).
//
// When [SearchVectorBuildRunner.ScopeState] is wired, the runner drives the
// per-scope vector-scope-state lifecycle (issue #4233): BeginBuilding before
// any mutation captures a CAS fence, AdvanceDocumentCursor/ResetDocumentCursor
// track keyset progress across bounded document pages, and FinalizeReady
// CAS-publishes readiness once ScopeVectorComplete reports the scope done. A
// nil ScopeState disables the lifecycle and keeps legacy/local wiring
// byte-identical.
//
// When [SearchVectorBuildRunner.ReadyPublisher] is wired, a sweep that
// completes with zero pending scopes after a POST-build re-check (not the
// pre-build listing — draining the LAST pending scopes has a non-zero
// pre-build count but a truly caught-up post-build state, issue #4673)
// publishes the search_vector_ready completion signal for the runner's
// [SearchVectorBuildIdentity] tuple, so go/internal/query's
// pending_search_vector freshness cause can clear for that same identity. A
// ready publish for one identity tuple must never satisfy freshness for a
// different one — a provider, model, or index-version rollout (or two
// reducer/API configs sharing one Postgres) would otherwise serve
// stale-under-new-config as fresh. A publish failure is logged, not
// returned: the sweep itself already succeeded.
//
// The package never imports the reducer root. The command layer
// (go/cmd/reducer) adapts [SearchVectorBuildPendingLister],
// [SearchVectorBuilder]/[SearchVectorBatchBuilder],
// [SearchVectorBuildReadyPublisher], and [SearchVectorScopeStateManager] to
// the concrete internal/searchvector builder and internal/storage/postgres
// stores, keeping this package free of storage and embedding dependencies.
// The reducer root keeps a compatibility surface
// (search_vector_build_compat.go) that aliases the runner and its request/
// result/config types back for the root's own Service wiring field and
// TestServiceStartsSearchVectorBuildRunner, so the import direction stays
// one-way: root depends on searchvector, never the reverse.
package searchvector
