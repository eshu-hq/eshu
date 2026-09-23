// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entity holds the entity-name search contract and the exact graph
// entity resolution helpers that the query handler families share.
//
// EntityNameSearch and EntityNameSearcher describe a bounded,
// authorization-aware content lookup by entity name; the content reader in
// package query implements it and the code, entity and impact handlers call
// it. ResolveExactGraphEntityCandidates and SelectExactGraphEntityCandidate
// turn a symbol name into one graph entity, failing closed on ambiguity with
// FormatAmbiguousEntityMatches. ClearResolvedEntityRepoProjectionPlaceholders
// strips graph-projected repo placeholders before repo identity is hydrated.
//
// The package imports its parent querycontract for EntityContent, ContentStore
// and RepositoryAccessFilterFromContext. The parent does not import it back,
// which is what keeps the split acyclic.
package entity
