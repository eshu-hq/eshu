// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "strings"

// Reasons a write statement is reported by UnanalyzedIndexWrites. The set is
// closed so it can label a metric.
const (
	// UnanalyzedReasonUnboundLabel means a schema-indexed label appears in a
	// write position the node-pattern reader never bound to a variable.
	UnanalyzedReasonUnboundLabel = "unbound_label"
	// UnanalyzedReasonUnresolvedValue means the statement writes an indexed
	// property (or a whole property map) from a value the guard cannot
	// measure: a nested UNWIND element, an unresolved WITH alias.
	UnanalyzedReasonUnresolvedValue = "unresolved_value"
	// UnanalyzedReasonUnparsedWrite means a property map entry or SET item
	// on a schema-labeled node is outside the recognized forms.
	UnanalyzedReasonUnparsedWrite = "unparsed_write"
)

// UnanalyzedIndexWrite names one schema-indexed label a statement writes in a
// way GuardIndexKeyWrites could not read, so oversized values written that
// way are neither dropped nor counted by the guard.
type UnanalyzedIndexWrite struct {
	// Label is a schema-indexed node label, so the set is closed.
	Label string
	// Reason is one of the UnanalyzedReason constants.
	Reason string
}

// UnanalyzedIndexWrites reports the schema-indexed labels cypher writes in a
// shape GuardIndexKeyWrites does not read. It is the fail-loud half of the
// guard: the analyzer covers the recognized write shapes, and this makes every
// other write to an indexed label visible instead of silently unguarded. Rows
// are not dropped for these statements, because the guard cannot reason about
// their values.
//
// The returned slice is shared with the statement plan cache and must not be
// modified. first is true exactly once per cached statement, so a caller can
// log a WARN once per distinct statement; it stays false when the plan cache
// is full, and the caller should then rely on the per-call metric.
func UnanalyzedIndexWrites(cypher string) (refs []UnanalyzedIndexWrite, first bool) {
	plan, cached := indexWritePlanFor(cypher)
	if len(plan.unanalyzed) == 0 {
		return nil, false
	}
	return plan.unanalyzed, cached && plan.warned.CompareAndSwap(false, true)
}

// unanalyzedWrites lists the schema-indexed labels the statement writes that
// the parse could not fully read.
func unanalyzedWrites(cypher string, keywords []cypherKeyword, vars map[string]*indexWriteVar, order []string) []UnanalyzedIndexWrite {
	byLabel := SchemaIndexKeysByLabel()
	var out []UnanalyzedIndexWrite
	add := func(label, reason string) {
		for _, u := range out {
			if u.Label == label && u.Reason == reason {
				return
			}
		}
		out = append(out, UnanalyzedIndexWrite{Label: label, Reason: reason})
	}

	writtenLabels := map[string]bool{}
	for _, name := range order {
		v := vars[name]
		for _, label := range v.labels {
			keys, indexed := byLabel[label]
			if !indexed {
				continue
			}
			switch {
			case v.opaque:
				add(label, UnanalyzedReasonUnparsedWrite)
			case v.written && hasUnresolvedIndexedValue(v, keys, vars):
				add(label, UnanalyzedReasonUnresolvedValue)
			}
			if v.written {
				writtenLabels[label] = true
			}
		}
	}

	// A label in a write clause that no variable carries was never read at
	// all (a node pattern the reader could not see).
	for _, label := range writeClauseLabels(cypher, keywords) {
		if _, indexed := byLabel[label]; indexed && !writtenLabels[label] {
			add(label, UnanalyzedReasonUnboundLabel)
		}
	}
	return out
}

// hasUnresolvedIndexedValue reports whether v writes a property map or an
// indexed property from a value the analyzer cannot measure. Identifiers that
// name another graph variable are bounded by what is already stored.
func hasUnresolvedIndexedValue(v *indexWriteVar, keys [][]string, vars map[string]*indexWriteVar) bool {
	unresolved := func(e indexWriteExpr) bool {
		for _, name := range e.unknown {
			if vars[name] == nil {
				return true
			}
		}
		return false
	}
	for _, e := range v.merges {
		if unresolved(e) {
			return true
		}
	}
	for prop, e := range v.assigns {
		if unresolved(e) && isKeyProperty(keys, prop) {
			return true
		}
	}
	return false
}

func isKeyProperty(keys [][]string, prop string) bool {
	for _, key := range keys {
		if containsString(key, prop) {
			return true
		}
	}
	return false
}

// writeClauseLabels returns the labels named inside MERGE, CREATE, and SET
// clauses, read independently of the node-pattern reader: relationship
// brackets, property maps, and string literals are stripped first.
func writeClauseLabels(cypher string, keywords []cypherKeyword) []string {
	var labels []string
	for i, kw := range keywords {
		if kw.word != "MERGE" && kw.word != "CREATE" && kw.word != "SET" {
			continue
		}
		end := len(cypher)
		if i+1 < len(keywords) {
			end = keywords[i+1].pos
		}
		for _, m := range cypherWriteLabel.FindAllStringSubmatch(stripNested(cypher[kw.end:end]), -1) {
			labels = append(labels, strings.Trim(m[1], "`"))
		}
	}
	return labels
}

// stripNested blanks quoted strings and the contents of {...} and [...].
func stripNested(s string) string {
	b := []byte(maskQuoted(s))
	depth := 0
	for i, c := range b {
		switch {
		case c == '{' || c == '[':
			depth++
			b[i] = ' '
		case (c == '}' || c == ']') && depth > 0:
			depth--
			b[i] = ' '
		case depth > 0:
			b[i] = ' '
		}
	}
	return string(b)
}
