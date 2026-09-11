// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved language family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/language"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// language_alias.go is the root alias shim for the language-query handler
// family (#6642, modelled on entity_alias.go and supply_chain_hub_alias.go).
// LanguageQueryHandler and its method files moved to language/. Names the
// rest of the program still spells `query.X` (handler wiring, cmd routers,
// staying root callers and tests) alias here so the move touches no caller
// outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import language directly.

// LanguageQueryHandler is the language-query handler family type. Its home
// is language/; this alias keeps cmd/api's and cmd/mcp-server's
// wiring_router.go struct literals, internal/mcp's dispatch tests, and
// staying root tests spelling query.LanguageQueryHandler unchanged.
type LanguageQueryHandler = language.Handler

// languageEntitySearch aliases language.EntitySearch. content_reader_entity_search.go
// (Part B, not moved) uses it to type the per-repository language-entity
// search struct passed into languageEntityContentSearcher. Its home is
// language/, exported there as EntitySearch because this alias is the
// caller that needs it.
type languageEntitySearch = language.EntitySearch

// normalizedLanguageVariants forwards to language.NormalizedVariants. Its
// home is language/; this wrapper keeps content_reader_entity_names.go,
// content_reader_entity_search.go, content_reader_structural_inventory.go,
// and content_reader_symbol_search.go (all Part B, not moved) calling the
// package-local name unchanged.
func normalizedLanguageVariants(lang string) []string {
	return language.NormalizedVariants(lang)
}

// SupportedEntityTypes returns the set of entity type names with
// language-query support. Its home is language/; this forwarder keeps
// entity_metadata_flux_test.go and the OpenAPI spec assembly calling the
// package-local name unchanged.
func SupportedEntityTypes() []string {
	return language.SupportedEntityTypes()
}

// SupportedLanguages returns the set of language names with language-query
// support. Its home is language/; this forwarder keeps staying root callers
// spelling the package-local name unchanged.
func SupportedLanguages() []string {
	return language.SupportedLanguages()
}

// sourceBackendForTruthBasis forwards to language.SourceBackendForTruthBasis.
// Its home is language/, exported there as SourceBackendForTruthBasis
// because language_query_source_backend_test.go (root, not moved: it also
// calls root-only OpenAPISpec(), which the leaf must never reach back for)
// is the caller that needs it.
func sourceBackendForTruthBasis(basis querycontract.TruthBasis) string {
	return language.SourceBackendForTruthBasis(basis)
}

// This compile-time pin proves *ContentReader still satisfies the port
// language.searchLanguageEntities type-asserts content stores against.
// *ContentReader is the only production content store (cmd/api/wiring.go and
// cmd/mcp-server/wiring.go both wire NewContentReader(db)); this line fails
// `go build`, not only `go test`, the moment it stops satisfying the port.
// It moved from language/metadata.go (#6642) because ContentReader is a
// later lane's family and the leaf must never name it.
var _ querycontract.LanguageEntityContentSearcher = (*ContentReader)(nil)
