// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// elixirSemanticEntityTypes maps Elixir semantic entity types to their
// graph/metadata resolution. The implementation moved to querycontract for
// #6060; this alias keeps root callers unchanged.
var elixirSemanticEntityTypes = querycontract.ElixirSemanticEntityTypes

func contentEntityTypeFilter(entityType string, nextArg int) (string, []any, int) {
	if semanticType, ok := elixirSemanticEntityTypes[entityType]; ok {
		clause := fmt.Sprintf(
			"(entity_type = $%d AND coalesce(metadata ->> '%s', '') = $%d)",
			nextArg,
			semanticType.MetadataKey,
			nextArg+1,
		)
		return clause, []any{semanticType.BaseType, semanticType.MetadataValue}, nextArg + 2
	}
	if entityType == "" {
		return "", nil, nextArg
	}
	return fmt.Sprintf("entity_type = $%d", nextArg), []any{entityType}, nextArg + 1
}
