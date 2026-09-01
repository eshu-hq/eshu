// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package eshusearch owns the curated search-document projection (design 430):
// it loads a repository scope's indexed content, curates it into
// EshuSearchDocument records, and writes the derived facts plus their search
// index terms into the Postgres search-lane read model. It performs no
// canonical graph write.
//
// [EshuSearchDocumentHandler.Handle] is the domain's Handle entrypoint. It begins
// a write session against [SearchDocumentWriter], streams source pages from
// [SearchDocumentSourceLoader] through [ProjectSearchDocuments], inserts each
// curated page, and finalizes once over the union keep-set so a page failure
// mid-stream can be cancelled without leaving the scope half-written (issue
// #3440, #3450). [PostgresEshuSearchDocumentWriter] is the production
// [SearchDocumentWriter]: it persists fact_records rows plus the BM25 search
// index terms those rows join on, and records the
// eshu_dp_search_index_mutations_total / eshu_dp_search_index_errors_total /
// eshu_dp_search_index_write_duration_seconds instruments.
//
// [DomainEshuSearchDocument] and [EshuSearchDocumentFactKind] name the domain
// and durable fact kind. The reducer root's registry and default-handler wiring
// reference both directly; this package keeps no compatibility aliases for
// them.
//
// # Why this is a leaf
//
// Before this package existed, the eshu_search_document family's 8 non-test
// files lived in the reducer root beside the worker, runner, and registry
// machinery that constructs and registers every domain. The root registers
// this domain (registry_additive_domains.go), assembles its handler
// (defaults_additive_domains_correlation.go), and declares its adapter fields
// (defaults_handlers.go) — so this package is imported BY the root rather than
// importing it. It reaches [reducercontract.Intent], [reducercontract.Result],
// and [reducercontract.ResultStatusSucceeded] through
// internal/reducer/contract, and reaches the writer's timestamp and
// collector-kind normalization through internal/reducer/factwrite and its
// dedup helper through internal/reducer/payloadcore — the same leaf packages
// the reducer root itself forwards to. None of those introduce a cycle back to
// the reducer root.
//
// internal/projector (the pending-scope sweeper) and
// internal/storage/postgres (the source loader) import this package directly
// for [DomainEshuSearchDocument] and [SearchDocumentProjectionInput]
// respectively; they held an unqualified `reducer.` reference to this family
// before the move and were repointed here in the same change.
package eshusearch
