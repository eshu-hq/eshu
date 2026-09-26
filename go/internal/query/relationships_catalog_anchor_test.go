// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestRelationshipEdgeQueriesRequireIndexableSourceAnchor checks every catalog
// verb and dispatch variant. Neo4j can use the source-property range index for
// the ordered page only when the query excludes null anchor values.
func TestRelationshipEdgeQueriesRequireIndexableSourceAnchor(t *testing.T) {
	t.Parallel()

	accesses := map[string]querycontract.RepositoryAccessFilter{
		"all":    {AllScopes: true},
		"scoped": {AllowedRepositoryIDs: []string{"repository:test"}},
	}
	for _, entry := range relationshipVerbCatalog {
		for accessName, access := range accesses {
			queries := map[string]string{
				"unfiltered": relationshipEdgesCypher(entry, access),
				"filtered":   relationshipEdgesCypherFilteredWithAnchor(entry, access, true),
			}
			for variant, query := range queries {
				name := entry.verb + "/" + accessName + "/" + variant
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					predicate := "s." + entry.sourceProperty + " IS NOT NULL"
					if strings.Count(query, predicate) != 1 {
						t.Fatalf("source anchor predicate %q occurs %d times in %s", predicate, strings.Count(query, predicate), query)
					}
					if strings.Index(query, predicate) > strings.Index(query, "RETURN ") || strings.Index(query, predicate) < strings.Index(query, "WHERE ") {
						t.Fatalf("source anchor predicate is outside WHERE clause: %s", query)
					}
				})
			}
		}
	}
}
