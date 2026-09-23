// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package taxonomy holds the query surface's naming vocabulary: how a
// language name normalizes (CanonicalLanguage, NormalizedLanguageVariants,
// CoverageLanguageMaps), which user-facing entity types are graph-backed or
// content-backed and how a graph label maps to a content entity type
// (GraphBackedEntityTypes, ContentBackedEntityTypes and their resolve and
// graph-first variants, the Elixir semantic types), how a graph row's optional
// semantic metadata is projected (GraphResultMetadata), and the shapes a
// language-scoped entity search uses (LanguageEntitySearch,
// LanguageEntityContentSearcher, LanguageResultMatchKey).
//
// The maps are package-level vars shared by every handler family; callers
// read them and must never write to them.
//
// It imports its parent querycontract for EntityContent and
// RepositoryLanguageCount, and querycontract/rowvalue for row decoding. The
// parent does not import it back.
package taxonomy
