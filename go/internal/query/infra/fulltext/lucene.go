// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// LuceneParam is the statement parameter that carries the Lucene query
// Lucene builds.
const LuceneParam = "infra_search_lucene"

// luceneSpecials are the characters the Lucene classic query parser treats as
// syntax. Each one is backslash-escaped so it matches itself.
const luceneSpecials = `\+-!():^[]"{}~*?|&/`

// Escape backslash-escapes every Lucene query syntax character and every
// Unicode white-space rune in q, so the whole of q is one literal inside a
// wildcard term. The keyword analyzer does not lowercase, so the match stays
// case sensitive, as CONTAINS is.
func Escape(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	for _, r := range q {
		if strings.ContainsRune(luceneSpecials, r) || unicode.IsSpace(r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Lucene returns the index query for q: every node whose stored key contains
// q, or whose key overflowed and was not stored.
func Lucene(q string) string {
	return graph.InfraSearchKeyProperty + ":*" + Escape(q) + "* OR " +
		graph.InfraSearchOverflowProperty + ":" + graph.InfraSearchOverflowYes
}
