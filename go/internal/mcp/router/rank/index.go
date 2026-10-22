// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"reflect"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Field weights. A curated intent phrase is the strongest signal, the tool name
// is next, and the free-text description is the weakest because it carries the
// most incidental vocabulary. #8756's eval harness is the place to tune them.
const (
	weightIntent = 3.0
	weightName   = 2.0
	weightDesc   = 1.0
)

// unitKind says which curated or free-text source a Why candidate came from.
type unitKind int

const (
	unitIntent unitKind = iota
	unitSentence
)

// unit is one intent phrase or description sentence kept for the Why line.
type unit struct {
	text  string
	kind  unitKind
	terms map[string]struct{}
}

// doc is the immutable, precomputed form of one tool.
type doc struct {
	name      string
	cost      catalog.CostClass
	costRank  int
	canonical bool
	// tf maps a stemmed term to its field-weighted frequency in this tool.
	tf map[string]float64
	// length is the field-weighted token count, the BM25 document length.
	length    float64
	nameTerms map[string]struct{}
	units     []unit
	props     map[string]struct{}
	schema    map[string]any
}

// index is the immutable corpus Rank scores against. Build it once.
//
// It holds the tools slice and entries map it was built from. Holding them keeps
// the map alive, so its address cannot be recycled for a different map while the
// index exists, and Rank compares identity against these references.
type index struct {
	docs       []*doc
	tools      []toolcontract.ToolDefinition
	entries    map[string]catalog.Entry
	entriesLen int
}

// costRank orders cost classes cheap to expensive. Anything unrecognized,
// including a tool with no catalog entry, ranks as high, matching the catalog's
// conservative default for unannotated surfaces.
func costRank(c catalog.CostClass) int {
	switch c {
	case catalog.CostLow:
		return 0
	case catalog.CostModerate:
		return 1
	default:
		return 2
	}
}

// buildIndex tokenizes every tool once and strips registry-wide boilerplate.
func buildIndex(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) *index {
	idx := &index{
		docs:       make([]*doc, 0, len(tools)),
		tools:      tools,
		entries:    entries,
		entriesLen: len(entries),
	}
	boiler := findBoilerplate(tools)
	for _, t := range tools {
		idx.docs = append(idx.docs, buildDoc(t, entries[t.Name], boiler))
	}
	return idx
}

func buildDoc(t toolcontract.ToolDefinition, e catalog.Entry, boiler *boilerplate) *doc {
	d := &doc{
		name:      t.Name,
		cost:      e.Cost,
		canonical: e.Canonical,
		tf:        map[string]float64{},
		nameTerms: map[string]struct{}{},
		props:     map[string]struct{}{},
	}
	if e.Cost == "" {
		d.cost = catalog.CostHigh
	}
	d.costRank = costRank(d.cost)
	add := func(toks []string, w float64) map[string]struct{} {
		set := make(map[string]struct{}, len(toks))
		for _, tok := range toks {
			d.tf[tok] += w
			d.length += w
			set[tok] = struct{}{}
		}
		return set
	}
	d.nameTerms = add(tokens(t.Name), weightName)
	for _, phrase := range e.Intents {
		d.units = append(d.units, unit{text: phrase, kind: unitIntent, terms: add(tokens(phrase), weightIntent)})
	}
	for _, s := range sentences(t.Description) {
		kept := boiler.keep(s)
		if len(kept) == 0 {
			continue
		}
		d.units = append(d.units, unit{text: s, kind: unitSentence, terms: add(kept, weightDesc)})
	}
	if schema, ok := t.InputSchema.(map[string]any); ok {
		d.schema = schema
		for _, name := range propertyNames(schema) {
			d.props[name] = struct{}{}
		}
	}
	return d
}

// matches reports whether the caller handed Rank the very inputs the index was
// built from: the same tools slice (same backing array and length) and the same
// entries map (same map, same size). Identity, not equality, is the test, so a
// new slice or map is always ranked from a fresh index even when its names look
// the same. Editing a retained slice's elements or the retained map in place is
// not detected beyond a change in the entries map's size; callers must not.
func (x *index) matches(tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) bool {
	if len(tools) != len(x.tools) || len(entries) != x.entriesLen ||
		reflect.ValueOf(entries).Pointer() != reflect.ValueOf(x.entries).Pointer() {
		return false
	}
	return len(tools) == 0 || &tools[0] == &x.tools[0]
}
