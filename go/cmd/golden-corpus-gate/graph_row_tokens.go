// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
)

// graphElementPropertyQueries read every node and relationship with its full
// property map. The token test runs in Go (gg.EvaluateUnresolvedRowTokens), not
// in a Cypher WHERE, for the same reason ListCorrelationEdgeProperty filters in
// Go: NornicDB does not reliably evaluate WHERE on these shapes. The gate corpus
// is small and bounded, so returning the maps is cheap.
var graphElementPropertyQueries = []struct {
	kind   string
	cypher string
}{
	{kind: "node", cypher: "MATCH (n) RETURN labels(n) AS name, properties(n) AS props"},
	{kind: "edge", cypher: "MATCH ()-[r]->() RETURN type(r) AS name, properties(r) AS props"},
}

// ListGraphElementProperties implements graphCounter: it returns every node
// (labels sorted and joined with ":") and every relationship (its type) with
// the element's property map.
func (b *boltGraphCounter) ListGraphElementProperties(ctx context.Context) ([]gg.GraphElementProperties, error) {
	var out []gg.GraphElementProperties
	for _, q := range graphElementPropertyQueries {
		rows, err := neo4j.ExecuteQuery(ctx, b.driver, q.cypher, nil,
			neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithDatabase(b.db))
		if err != nil {
			return nil, fmt.Errorf("list %s properties: %w", q.kind, err)
		}
		for _, rec := range rows.Records {
			name, _ := rec.Get("name")
			props, _ := rec.Get("props")
			propMap, _ := props.(map[string]any)
			out = append(out, gg.GraphElementProperties{
				Kind:       q.kind,
				Name:       graphElementName(name),
				Properties: propMap,
			})
		}
	}
	return out, nil
}

// graphElementName renders a relationship type or a node label list.
func graphElementName(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		labels := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				labels = append(labels, s)
			}
		}
		sort.Strings(labels)
		return strings.Join(labels, ":")
	}
	return ""
}

// checkUnresolvedRowTokens adds the corpus-size-independent, always-required
// finding that no node or edge property holds an unresolved `row.<key>` token
// (#6782). It catches a writer that omits an UNWIND row key on NornicDB even
// when no required correlation or query shape reads that property.
func checkUnresolvedRowTokens(ctx context.Context, c graphCounter, r *Report) error {
	elements, err := c.ListGraphElementProperties(ctx)
	if err != nil {
		return fmt.Errorf("list graph element properties: %w", err)
	}
	r.Add(gg.EvaluateUnresolvedRowTokens(elements))
	return nil
}
