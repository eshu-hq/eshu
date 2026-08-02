// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQueryLanguageQuery wraps POST /api/v0/code/language-query (#5761),
	// the language-specific entity-kind lookup route spanning the graph-backed,
	// graph-first-content, and content-only entity-type families. The span
	// carries the stable http.route and eshu.capability attributes
	// (symbol_graph.language_entities) so an operator can correlate this
	// route's latency and error rate to the capability the YAML matrix's
	// p95_latency_ms budgets describe.
	SpanQueryLanguageQuery = "query.language_query"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryCodeownersOwnership {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryLanguageQuery)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryLanguageQuery)
}
