// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"math"
	"strings"
)

// graphEntityKindCountsCypher returns every curated facet count in one graph
// round trip. Each scalar subquery remains anchored on one concrete label and
// returns one count column, including for a zero-match label. The plain outer
// projection avoids NornicDB's corrupt grouped-literal and outer-aggregation
// paths.
func graphEntityKindCountsCypher(kinds []graphEntityKind) string {
	subqueries := make([]string, 0, len(kinds))
	columns := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		subqueries = append(subqueries, fmt.Sprintf(
			"CALL {\nMATCH (n:%s) RETURN count(n) AS %s\n}",
			kind.label, kind.key,
		))
		columns = append(columns, kind.key)
	}
	return strings.Join(subqueries, "\n") + "\nRETURN " + strings.Join(columns, ", ")
}

// decodeGraphEntityKindCounts validates the fixed backend row contract and
// restores the curated display order. Missing, extra, malformed, or negative
// count columns fail closed instead of publishing plausible but incomplete
// facet totals after a backend projection regression.
func decodeGraphEntityKindCounts(rows []map[string]any) ([]map[string]any, int, error) {
	if len(rows) != 1 {
		return nil, 0, fmt.Errorf("graph entity facet count returned %d rows, want 1", len(rows))
	}
	row := rows[0]
	expected := make(map[string]graphEntityKind, len(graphEntityKinds))
	for _, kind := range graphEntityKinds {
		expected[kind.key] = kind
	}
	for key := range row {
		_, ok := expected[key]
		if !ok {
			return nil, 0, fmt.Errorf("graph entity facet count returned unknown key %q", key)
		}
	}

	kindCounts := make([]map[string]any, 0, len(graphEntityKinds))
	total := 0
	maxInt := int(^uint(0) >> 1)
	for _, kind := range graphEntityKinds {
		count, err := graphEntityFacetCount(row, kind.key)
		if err != nil {
			return nil, 0, fmt.Errorf("graph entity facet %q: %w", kind.key, err)
		}
		if count < 0 {
			return nil, 0, fmt.Errorf("graph entity facet %q returned negative count %d", kind.key, count)
		}
		if count > maxInt-total {
			return nil, 0, fmt.Errorf("graph entity facet total overflows int at %q", kind.key)
		}
		total += count
		kindCounts = append(kindCounts, map[string]any{
			"kind":  kind.key,
			"label": kind.label,
			"count": count,
		})
	}
	return kindCounts, total, nil
}

func graphEntityFacetCount(row map[string]any, key string) (int, error) {
	raw, ok := row[key]
	if !ok {
		return 0, fmt.Errorf("count is missing")
	}
	switch value := raw.(type) {
	case int:
		return value, nil
	case int32:
		return int(value), nil
	case int64:
		count := int(value)
		if int64(count) != value {
			return 0, fmt.Errorf("count %d overflows int", value)
		}
		return count, nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
			return 0, fmt.Errorf("count %v is not a finite integer", value)
		}
		count := int(value)
		if float64(count) != value {
			return 0, fmt.Errorf("count %v overflows int", value)
		}
		return count, nil
	default:
		return 0, fmt.Errorf("count has type %T, want integer", raw)
	}
}
