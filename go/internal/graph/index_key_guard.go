// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"sync"
	"sync/atomic"
)

// OversizedIndexWrite records one write GuardIndexKeyWrites removed because it
// would have put more than MaxIndexKeyBytes into one schema index key.
type OversizedIndexWrite struct {
	// Label is the schema label whose index key was oversized. It comes from
	// the Go-owned schema, so the set is closed.
	Label string
	// Property is the largest contributor to the oversized key. It is a
	// schema property name, so the set is closed.
	Property string
	// KeyBytes is the measured string byte total of the key.
	KeyBytes int
	// Param is the statement parameter the row came from ("rows"), or the
	// scalar parameter that carried the value when the whole statement is
	// skipped.
	Param string
	// EntityID, RepoID, ScopeID, GenerationID, and FilePath are copied from
	// the row when it carries them, for operator triage. They may be empty.
	EntityID     string
	RepoID       string
	ScopeID      string
	GenerationID string
	FilePath     string
	// ValuePrefix is at most 64 bytes of the largest value, cut on a rune
	// boundary.
	ValuePrefix string
}

const (
	// IndexValuePrefixBytes bounds the value excerpt carried in an oversized
	// index-key record so a skip log line stays small.
	IndexValuePrefixBytes = 64
	// indexWritePlanCacheLimit bounds the per-process plan cache. Eshu writers
	// use a fixed set of statement templates; past the limit a statement is
	// analyzed on every call instead of growing the cache without bound.
	indexWritePlanCacheLimit = 4096
)

var (
	indexWritePlans     sync.Map // cypher -> *indexWritePlan
	indexWritePlanCount atomic.Int64
)

// GuardIndexKeyWrites removes every write that would put more than
// MaxIndexKeyBytes of string data into one schema index key, so a single
// oversized source value skips one node instead of failing the atomic write
// it rides in (#7058).
//
// The indexed keys come from the Go-owned schema (SchemaIndexKeysByLabel),
// so a new index is guarded without touching any writer. For UNWIND $param
// statements it drops the offending rows, and dropping a row drops everything
// that row writes: every node and every edge in the statement, not only the
// node whose key was oversized. An oversized row.environment on
// BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher, for example,
// also drops the EvidenceArtifact node and its HAS_DEPLOYMENT_EVIDENCE and
// EVIDENCES_REPOSITORY_RELATIONSHIP edges. Edges written by other statements
// MATCH the node and so cannot attach to a node that was never written. For
// statements whose indexed value comes from a scalar parameter it reports
// skip=true and the caller must not execute the statement.
//
// The analyzer reads the write shapes Eshu writers use (see
// index_key_guard_parse.go). It does not claim to read every Cypher spelling:
// a statement that writes a schema-indexed label in a shape it cannot read is
// left unchanged and reported by UnanalyzedIndexWrites, which the executor
// turns into a metric and a WARN. TestProductionCypherLiteralsAreGuarded fails
// when a production writer takes such a shape.
//
// params is never mutated. When nothing is oversized, params is returned
// unchanged and dropped is nil.
func GuardIndexKeyWrites(cypher string, params map[string]any) (out map[string]any, dropped []OversizedIndexWrite, skip bool) {
	plan, _ := indexWritePlanFor(cypher)
	if len(plan.vars) == 0 {
		return params, nil, false
	}

	// Scalar-only writes: measure once against the parameters.
	for _, v := range plan.vars {
		if v.rowVar() != "" {
			continue
		}
		if rec, over := v.oversized(indexRow{}, params); over {
			return params, []OversizedIndexWrite{rec}, true
		}
	}

	out = params
	for rowVar, param := range plan.rowParams {
		kept, recs := filterIndexWriteRows(plan, rowVar, param, params)
		if recs == nil {
			continue
		}
		if len(dropped) == 0 {
			out = make(map[string]any, len(params))
			for k, val := range params {
				out[k] = val
			}
		}
		out[param] = kept
		dropped = append(dropped, recs...)
	}
	return out, dropped, false
}

// indexWritePlanFor returns the analysis of cypher and whether it lives in
// the plan cache. A plan analyzed while the cache is full is returned
// uncached and analyzed again on the next call.
func indexWritePlanFor(cypher string) (plan *indexWritePlan, cached bool) {
	if hit, ok := indexWritePlans.Load(cypher); ok {
		return hit.(*indexWritePlan), true
	}
	plan = analyzeIndexWrites(cypher)
	if indexWritePlanCount.Load() >= indexWritePlanCacheLimit {
		return plan, false
	}
	actual, loaded := indexWritePlans.LoadOrStore(cypher, plan)
	if !loaded {
		indexWritePlanCount.Add(1)
	}
	return actual.(*indexWritePlan), true
}

// filterIndexWriteRows returns the kept rows of params[param] in their
// original slice type, or nil records when every row is kept.
func filterIndexWriteRows(plan *indexWritePlan, rowVar, param string, params map[string]any) (any, []OversizedIndexWrite) {
	var vars []*indexWriteVar
	for _, v := range plan.vars {
		if v.rowVar() == rowVar {
			vars = append(vars, v)
		}
	}
	if len(vars) == 0 {
		return nil, nil
	}
	check := func(row indexRow) []OversizedIndexWrite {
		var recs []OversizedIndexWrite
		for _, v := range vars {
			if rec, over := v.oversized(row, params); over {
				rec.Param = param
				recs = append(recs, rec)
			}
		}
		return recs
	}

	switch rows := params[param].(type) {
	case []map[string]any:
		return filterRowsOf(rows, func(r map[string]any) indexRow { return indexRow{any: r} }, check)
	case []any:
		return filterRowsOf(rows, func(r any) indexRow { m, _ := r.(map[string]any); return indexRow{any: m} }, check)
	case []map[string]string:
		// Sizes are read straight off the string map: converting each row to
		// map[string]any would allocate per row on every guarded write.
		return filterRowsOf(rows, func(r map[string]string) indexRow { return indexRow{str: r} }, check)
	}
	return nil, nil
}

func filterRowsOf[T any](rows []T, asRow func(T) indexRow, check func(indexRow) []OversizedIndexWrite) (any, []OversizedIndexWrite) {
	var recs []OversizedIndexWrite
	var kept []T
	for i, row := range rows {
		r := asRow(row)
		var rowRecs []OversizedIndexWrite
		if r.present() {
			rowRecs = check(r)
		}
		if len(rowRecs) == 0 {
			if kept != nil {
				kept = append(kept, row)
			}
			continue
		}
		if kept == nil {
			kept = make([]T, i, len(rows)-1)
			copy(kept, rows[:i])
		}
		recs = append(recs, rowRecs...)
	}
	if recs == nil {
		return nil, nil
	}
	return kept, recs
}

// indexRow is one UNWIND row as the guard reads it: a map[string]any row or a
// map[string]string row, never both. Reading each shape in place keeps the
// guard allocation-free for every row type Eshu writers pass as a top-level
// rows parameter. The zero value is the absent row of a scalar-only statement.
type indexRow struct {
	any map[string]any
	str map[string]string
}

// present reports whether the row exists, as opposed to the absent row of a
// scalar-only statement or a []any element that is not a map.
func (r indexRow) present() bool { return r.any != nil || r.str != nil }

// field returns the string byte size of the row's value for f and the value
// itself. A missing field measures zero.
func (r indexRow) field(f string) (int, string) {
	if r.any != nil {
		return valueSize(r.any[f])
	}
	s := r.str[f]
	return len(s), s
}

// asMap returns the row as a map[string]any. It copies a string row, so only
// the rare triage path calls it, after a row is already known to be oversized.
func (r indexRow) asMap() map[string]any {
	if r.any != nil || r.str == nil {
		return r.any
	}
	m := make(map[string]any, len(r.str))
	for k, v := range r.str {
		m[k] = v
	}
	return m
}

// rowVar returns the UNWIND variable the var's writes read from, or "".
func (v *indexWriteVar) rowVar() string {
	for _, e := range v.assigns {
		if e.rowVar != "" {
			return e.rowVar
		}
	}
	for _, e := range v.merges {
		if e.rowVar != "" {
			return e.rowVar
		}
	}
	return ""
}

// oversized reports whether any schema key of v exceeds MaxIndexKeyBytes for
// this row (nil for scalar-only statements), naming the worst key.
func (v *indexWriteVar) oversized(row indexRow, params map[string]any) (OversizedIndexWrite, bool) {
	var worst OversizedIndexWrite
	for _, key := range v.keys {
		total, maxSize := 0, -1
		var maxProp, maxValue string
		for _, prop := range key.props {
			size, value := v.propSize(prop, row, params)
			total += size
			if size > maxSize {
				maxSize, maxProp, maxValue = size, prop, value
			}
		}
		if total > MaxIndexKeyBytes && total > worst.KeyBytes {
			worst = OversizedIndexWrite{
				Label:       key.label,
				Property:    maxProp,
				KeyBytes:    total,
				ValuePrefix: IndexValuePrefix(maxValue),
			}
		}
	}
	if worst.KeyBytes == 0 {
		return worst, false
	}
	// Triage context lives on the row or, for SET n += row.props writers,
	// inside the merged property map.
	sources := []map[string]any{row.asMap()}
	for _, e := range v.merges {
		sources = append(sources, e.maps(row, params)...)
	}
	worst.EntityID = firstString(sources, "entity_id", "uid", "id")
	worst.RepoID = firstString(sources, "repo_id")
	worst.ScopeID = firstString(sources, "scope_id")
	worst.GenerationID = firstString(sources, "generation_id")
	worst.FilePath = firstString(sources, "file_path", "path")
	return worst, true
}

// propSize returns the string byte size the write assigns to prop and the
// largest single value, for the value prefix.
func (v *indexWriteVar) propSize(prop string, row indexRow, params map[string]any) (int, string) {
	if e, ok := v.assigns[prop]; ok {
		return e.size(row, params)
	}
	best, bestValue := 0, ""
	consider := func(src any) {
		if m, ok := src.(map[string]any); ok {
			if size, value := valueSize(m[prop]); size > best {
				best, bestValue = size, value
			}
		}
	}
	// Look the merged maps up in place rather than through maps(): this runs
	// per row and property on the canonical hot path and must not allocate.
	for _, e := range v.merges {
		if row.present() {
			// A string row holds no nested property maps, so only a
			// map[string]any row has anything to consider here.
			for _, f := range e.fields {
				consider(row.any[f])
			}
			if e.wholeRow {
				if size, value := row.field(prop); size > best {
					best, bestValue = size, value
				}
			}
		}
		for _, p := range e.params {
			consider(params[p])
		}
	}
	return best, bestValue
}

func (e indexWriteExpr) size(row indexRow, params map[string]any) (int, string) {
	total, best, bestValue := 0, 0, ""
	add := func(size int, value string) {
		total += size
		if size > best {
			best, bestValue = size, value
		}
	}
	if row.present() {
		for _, f := range e.fields {
			add(row.field(f))
		}
	}
	for _, p := range e.params {
		add(valueSize(params[p]))
	}
	if e.sum {
		return total + e.constBytes, bestValue
	}
	return best, bestValue
}

func (e indexWriteExpr) maps(row indexRow, params map[string]any) []map[string]any {
	var out []map[string]any
	if row.present() {
		for _, f := range e.fields {
			if m, ok := row.any[f].(map[string]any); ok {
				out = append(out, m)
			}
		}
		if e.wholeRow {
			out = append(out, row.asMap())
		}
	}
	for _, p := range e.params {
		if m, ok := params[p].(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// valueSize returns the string bytes an index key would store for val and
// the largest string in it. Non-string scalars are fixed-width and bounded.
func valueSize(val any) (int, string) {
	switch x := val.(type) {
	case string:
		return len(x), x
	case []string:
		total, best := 0, ""
		for _, s := range x {
			total += len(s)
			if len(s) > len(best) {
				best = s
			}
		}
		return total, best
	case []any:
		total, best := 0, ""
		for _, item := range x {
			if s, ok := item.(string); ok {
				total += len(s)
				if len(s) > len(best) {
					best = s
				}
			}
		}
		return total, best
	}
	return 0, ""
}

// firstString returns the first non-empty string value found for keys,
// trying each key across every source in order. File paths may be oversized
// themselves, so callers log them only as context next to the bounded prefix.
func firstString(sources []map[string]any, keys ...string) string {
	for _, k := range keys {
		for _, src := range sources {
			if s, ok := src[k].(string); ok && s != "" {
				return IndexValuePrefix(s)
			}
		}
	}
	return ""
}

// IndexValuePrefix returns at most IndexValuePrefixBytes of s without
// splitting a UTF-8 sequence, so an oversized index-key value can be logged as
// a bounded, valid-UTF-8 excerpt. The statement guard and the canonical
// projector's node-row bound both use it, so the bound and the cut rule stay
// in one place.
func IndexValuePrefix(s string) string {
	if len(s) <= IndexValuePrefixBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > IndexValuePrefixBytes {
			break
		}
		end = i
	}
	return s[:end]
}
