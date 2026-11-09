// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
)

// SemanticSearchReadAdmissionContractVersion versions the #9669 O6
// admission-contract shape for the semantic-search read (POST
// /api/v0/search/semantic). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned index, hybrid,
// scope, and watermark readers satisfy jointly, composed by the
// consumer. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const SemanticSearchReadAdmissionContractVersion = "v0"

// SemanticSearchReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the four live handles. It
// declares exactly the five reads POST /api/v0/search/semantic admits
// through — the persisted-index search (IndexSearch, mirroring
// SemanticSearchIndexStore.Search), the local hybrid search
// (HybridSearch, mirroring SemanticSearchHybridStore.Search), the
// scope resolution pair (ResolveScope and ResolveRepositoryForScope,
// mirroring SemanticSearchScopeResolver), and the search-vector build
// sweep watermark probe (VectorReadyWatermark, mirroring
// SemanticSearchVectorReadyReader.SearchVectorReadyWatermark) — so the
// boundary is held honest by the compile-time mirror below plus the
// frozen method-set contract test. It will back the eshu-api
// extraction handoff for this read. The seam is read-only on purpose:
// every declared method is a bounded read and the port carries no
// mutation surface, so a dest consumer implementing only this contract
// can serve semantic search but cannot acquire a write it did not
// bargain for through it.
//
// D-wave consumer: O6 pin 4 (per the arbiter O6 ruling minting order —
// a blanket or shared-port pin for this route is forbidden). The
// satellite records this pin in a follow-up; a satellite PR consuming
// an unminted pin is invalid. Arbiter review of this pin precedes
// satellite termination of the semantic-search POST leg, with
// truth-freshness parity on the pending_search_vector cause.
//
// Narrow by construction, recorded not silently assumed: the five
// methods re-declare the live-surface signatures in this
// SemanticSearch-scoped shape instead of embedding or aliasing
// SemanticSearchIndexStore, SemanticSearchHybridStore,
// SemanticSearchScopeResolver, or SemanticSearchVectorReadyReader.
// This pin therefore grants nothing to the other families that read
// through shared ports — no other family may cite this pin as
// coverage.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The grant checks (RepositoryAccessFilter plus the scope grant
//     comparison) and the fixed failure messages are handler behavior,
//     not port shape: there is no method set to freeze, the same
//     standing as QueryProfile in this handler.
//   - The rerank, telemetry, and response-normalization legs consume no
//     port: pure request-body derivation and in-memory shaping.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type SemanticSearchReadAdmission interface {
	IndexSearch(ctx context.Context, query SemanticSearchIndexQuery) (SemanticSearchIndexResult, error)
	HybridSearch(ctx context.Context, query SemanticSearchIndexQuery) (SemanticSearchIndexResult, error)
	ResolveScope(ctx context.Context, repoID string) (string, error)
	ResolveRepositoryForScope(ctx context.Context, scopeID string) (string, error)
	VectorReadyWatermark(ctx context.Context) (SearchVectorReadyFreshness, error)
}

// semanticSearchReadAdmissionMirror keeps the SemanticSearch-scoped
// re-declaration honest, and it works in one direction only: removal,
// rename, or re-signing of any mirrored live-surface method fails
// compilation here. Live-surface addition deliberately does not widen
// this pin — a new live method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the four live surfaces only as the combination
// this pin mirrors, and no other family may cite this pin as coverage.
type semanticSearchReadAdmissionMirror struct {
	index     SemanticSearchIndexStore
	hybrid    SemanticSearchHybridStore
	scope     SemanticSearchScopeResolver
	watermark SemanticSearchVectorReadyReader
}

func (m semanticSearchReadAdmissionMirror) IndexSearch(ctx context.Context, query SemanticSearchIndexQuery) (SemanticSearchIndexResult, error) {
	return m.index.Search(ctx, query)
}

func (m semanticSearchReadAdmissionMirror) HybridSearch(ctx context.Context, query SemanticSearchIndexQuery) (SemanticSearchIndexResult, error) {
	return m.hybrid.Search(ctx, query)
}

func (m semanticSearchReadAdmissionMirror) ResolveScope(ctx context.Context, repoID string) (string, error) {
	return m.scope.ResolveSemanticSearchScope(ctx, repoID)
}

func (m semanticSearchReadAdmissionMirror) ResolveRepositoryForScope(ctx context.Context, scopeID string) (string, error) {
	return m.scope.ResolveSemanticSearchRepositoryForScope(ctx, scopeID)
}

func (m semanticSearchReadAdmissionMirror) VectorReadyWatermark(ctx context.Context) (SearchVectorReadyFreshness, error) {
	return m.watermark.SearchVectorReadyWatermark(ctx)
}

var _ SemanticSearchReadAdmission = semanticSearchReadAdmissionMirror{}
