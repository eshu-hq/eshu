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
	oversizedValuePrefixBytes = 64
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
		if rec, over := v.oversized(nil, params); over {
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
	check := func(row map[string]any) []OversizedIndexWrite {
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
		return filterRowsOf(rows, func(r map[string]any) map[string]any { return r }, check)
	case []any:
		return filterRowsOf(rows, func(r any) map[string]any { m, _ := r.(map[string]any); return m }, check)
	case []map[string]string:
		return filterRowsOf(rows, func(r map[string]string) map[string]any {
			m := make(map[string]any, len(r))
			for k, v := range r {
				m[k] = v
			}
			return m
		}, check)
	}
	return nil, nil
}

func filterRowsOf[T any](rows []T, asMap func(T) map[string]any, check func(map[string]any) []OversizedIndexWrite) (any, []OversizedIndexWrite) {
	var recs []OversizedIndexWrite
	var kept []T
	for i, row := range rows {
		m := asMap(row)
		var rowRecs []OversizedIndexWrite
		if m != nil {
			rowRecs = check(m)
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
func (v *indexWriteVar) oversized(row, params map[string]any) (OversizedIndexWrite, bool) {
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
				ValuePrefix: indexValuePrefix(maxValue),
			}
		}
	}
	if worst.KeyBytes == 0 {
		return worst, false
	}
	// Triage context lives on the row or, for SET n += row.props writers,
	// inside the merged property map.
	sources := []map[string]any{row}
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
func (v *indexWriteVar) propSize(prop string, row, params map[string]any) (int, string) {
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
		if row != nil {
			for _, f := range e.fields {
				consider(row[f])
			}
			if e.wholeRow {
				consider(row)
			}
		}
		for _, p := range e.params {
			consider(params[p])
		}
	}
	return best, bestValue
}

func (e indexWriteExpr) size(row, params map[string]any) (int, string) {
	total, best, bestValue := 0, 0, ""
	add := func(val any) {
		size, value := valueSize(val)
		total += size
		if size > best {
			best, bestValue = size, value
		}
	}
	if row != nil {
		for _, f := range e.fields {
			add(row[f])
		}
	}
	for _, p := range e.params {
		add(params[p])
	}
	if e.sum {
		return total + e.constBytes, bestValue
	}
	return best, bestValue
}

func (e indexWriteExpr) maps(row, params map[string]any) []map[string]any {
	var out []map[string]any
	if row != nil {
		for _, f := range e.fields {
			if m, ok := row[f].(map[string]any); ok {
				out = append(out, m)
			}
		}
		if e.wholeRow {
			out = append(out, row)
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
				return indexValuePrefix(s)
			}
		}
	}
	return ""
}

// indexValuePrefix returns at most oversizedValuePrefixBytes of s without
// splitting a UTF-8 sequence.
func indexValuePrefix(s string) string {
	if len(s) <= oversizedValuePrefixBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > oversizedValuePrefixBytes {
			break
		}
		end = i
	}
	return s[:end]
}
