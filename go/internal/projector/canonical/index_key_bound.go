// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import "github.com/eshu-hq/eshu/go/internal/graph"

// MaxIndexedKeyBytes is the largest UTF-8 byte length the canonical write
// admits for the string part of one graph index key. It is
// graph.MaxIndexKeyBytes, whose doc carries the measured Neo4j limits (#7058).
//
// DropOversizedIndexKeys applies it to the canonical materialization so a
// dropped node takes its name-keyed dependent rows (IMPORTS, HAS_PARAMETER,
// class and nested CONTAINS) with it. The schema-derived statement guard in
// storage/cypher (GuardStatementIndexKeys, run by InstrumentedExecutor on
// every graph write) covers every other label and indexed property.
const MaxIndexedKeyBytes = graph.MaxIndexKeyBytes

// oversizedValuePrefixBytes bounds the value excerpt carried in an
// OversizedIndexKey so a skip log line stays small.
const oversizedValuePrefixBytes = 64

// OversizedIndexKey records one node row DropOversizedIndexKeys removed
// because its indexed key exceeded MaxIndexedKeyBytes.
type OversizedIndexKey struct {
	// Label is the graph node label the row would have written, for example
	// "Module", "Function", or "Parameter". It is a closed set.
	Label string
	// Property is the largest indexed property in the key: "name" or "path".
	Property string
	// KeyBytes is the UTF-8 byte length of the measured key.
	KeyBytes int
	// EntityID is the canonical entity uid, empty for Module and Parameter.
	EntityID string
	// FilePath is the source file of the row, empty for Module.
	FilePath string
	// ValuePrefix is at most oversizedValuePrefixBytes of the oversized
	// property's value, cut on a rune boundary, for operator triage.
	ValuePrefix string
}

// DropOversizedIndexKeys removes every row whose indexed graph key would exceed
// MaxIndexedKeyBytes and returns the reduced materialization plus one record
// per removed node row.
//
// Node rows checked: Module (name), entities (name plus path, the node-key
// constraint shape), and Parameter (name plus path). Edge rows that MATCH an
// oversized key (IMPORTS, HAS_PARAMETER, class and nested CONTAINS) are dropped
// with them so they cannot attach to a stale node carrying the same value, but
// they are not reported separately: the node skip already names the cause.
//
// The node is absent from the graph rather than written with a truncated or
// hashed value, so the graph never holds a corrupted identity. When nothing
// exceeds the limit, mat is returned unchanged with its original slices.
func DropOversizedIndexKeys(mat CanonicalMaterialization) (CanonicalMaterialization, []OversizedIndexKey) {
	var dropped []OversizedIndexKey

	mat.Modules = filterRows(mat.Modules, func(m ModuleRow) bool {
		if len(m.Name) <= MaxIndexedKeyBytes {
			return true
		}
		dropped = append(dropped, OversizedIndexKey{
			Label: "Module", Property: "name", KeyBytes: len(m.Name), ValuePrefix: valuePrefix(m.Name),
		})
		return false
	})
	mat.Entities = filterRows(mat.Entities, func(e EntityRow) bool {
		if !nameAndPathOversized(e.EntityName, e.FilePath) {
			return true
		}
		dropped = append(dropped, nameAndPathRecord(e.Label, e.EntityName, e.FilePath, e.EntityID))
		return false
	})
	mat.Parameters = filterRows(mat.Parameters, func(p ParameterRow) bool {
		if nameAndPathOversized(p.ParamName, p.FilePath) {
			dropped = append(dropped, nameAndPathRecord("Parameter", p.ParamName, p.FilePath, ""))
			return false
		}
		return !nameAndPathOversized(p.FunctionName, p.FilePath)
	})
	mat.Imports = filterRows(mat.Imports, func(i ImportRow) bool {
		return len(i.ModuleName) <= MaxIndexedKeyBytes
	})
	mat.ClassMembers = filterRows(mat.ClassMembers, func(c ClassMemberRow) bool {
		return !nameAndPathOversized(c.ClassName, c.FilePath) && !nameAndPathOversized(c.FunctionName, c.FilePath)
	})
	mat.NestedFuncs = filterRows(mat.NestedFuncs, func(n NestedFunctionRow) bool {
		return !nameAndPathOversized(n.OuterName, n.FilePath) && !nameAndPathOversized(n.InnerName, n.FilePath)
	})
	return mat, dropped
}

// filterRows returns rows unchanged when keep accepts every row, and a new
// slice of the accepted rows otherwise. keep is called exactly once per row.
func filterRows[T any](rows []T, keep func(T) bool) []T {
	for i := range rows {
		if keep(rows[i]) {
			continue
		}
		out := make([]T, i, len(rows)-1)
		copy(out, rows[:i])
		for _, row := range rows[i+1:] {
			if keep(row) {
				out = append(out, row)
			}
		}
		return out
	}
	return rows
}

func nameAndPathOversized(name, path string) bool {
	return len(name)+len(path) > MaxIndexedKeyBytes
}

func nameAndPathRecord(label, name, path, entityID string) OversizedIndexKey {
	property, value := "name", name
	if len(path) > len(name) {
		property, value = "path", path
	}
	return OversizedIndexKey{
		Label:       label,
		Property:    property,
		KeyBytes:    len(name) + len(path),
		EntityID:    entityID,
		FilePath:    path,
		ValuePrefix: valuePrefix(value),
	}
}

// valuePrefix returns at most oversizedValuePrefixBytes of s without splitting
// a UTF-8 sequence.
func valuePrefix(s string) string {
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
