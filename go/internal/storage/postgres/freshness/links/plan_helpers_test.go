// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// planNode is one node of an EXPLAIN (FORMAT JSON) plan.
type planNode map[string]any

func (n planNode) str(key string) string {
	v, _ := n[key].(string)
	return v
}

func (n planNode) num(key string) float64 {
	v, _ := n[key].(float64)
	return v
}

// planNodes flattens an EXPLAIN (FORMAT JSON) document: every plan node,
// including the plans of CTEs and subplans, plus the top-level triggers.
func planNodes(t *testing.T, raw string) (nodes []planNode, triggers []any) {
	t.Helper()
	var doc []map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if len(doc) != 1 {
		t.Fatalf("plan document has %d entries", len(doc))
	}
	triggers, _ = doc[0]["Triggers"].([]any)
	var walk func(any)
	walk = func(v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		nodes = append(nodes, planNode(m))
		if children, ok := m["Plans"].([]any); ok {
			for _, child := range children {
				walk(child)
			}
		}
	}
	walk(doc[0]["Plan"])
	return nodes, triggers
}

// relationAccess describes how one plan reads one relation: the scan node,
// and for a bitmap heap scan the index node beneath it.
type relationAccess struct {
	nodeType  string
	indexName string
	indexCond string
	rows      float64
	buffers   float64
}

// accessesOf returns every access of relation in the plan.
func accessesOf(nodes []planNode, relation string) []relationAccess {
	var out []relationAccess
	for _, n := range nodes {
		// ModifyTable names its target relation but reads nothing itself.
		if n.str("Relation Name") != relation || n.str("Node Type") == "ModifyTable" {
			continue
		}
		access := relationAccess{
			nodeType:  n.str("Node Type"),
			indexName: n.str("Index Name"),
			indexCond: n.str("Index Cond") + n.str("Recheck Cond"),
			rows:      n.num("Actual Rows") * max(n.num("Actual Loops"), 1),
			buffers:   n.num("Shared Hit Blocks") + n.num("Shared Read Blocks"),
		}
		if access.nodeType == "Bitmap Heap Scan" {
			if children, ok := n["Plans"].([]any); ok && len(children) > 0 {
				if child, ok := children[0].(map[string]any); ok {
					access.indexName = planNode(child).str("Index Name")
					access.indexCond = planNode(child).str("Index Cond")
				}
			}
		}
		out = append(out, access)
	}
	return out
}

// indexAccessFailures lists why an access of relation is not a read of index
// with every column of columns in its condition: a sequential scan, another
// index, a missing column, or more than maxBuffersPerRow buffers per row
// (0 skips the buffer bound). No failures means the shape holds.
func indexAccessFailures(nodes []planNode, relation, index string, columns []string, maxBuffersPerRow float64) []string {
	accesses := accessesOf(nodes, relation)
	if len(accesses) == 0 {
		return []string{fmt.Sprintf("no access of %s in the plan", relation)}
	}
	var failures []string
	for _, a := range accesses {
		if a.nodeType == "Seq Scan" {
			failures = append(failures, fmt.Sprintf("Seq Scan on %s", relation))
			continue
		}
		if a.indexName != index {
			failures = append(failures, fmt.Sprintf("%s on %s uses index %q, want %s", a.nodeType, relation, a.indexName, index))
		}
		for _, column := range columns {
			if !strings.Contains(a.indexCond, column) {
				failures = append(failures, fmt.Sprintf("%s on %s: index condition %q does not name %s", a.nodeType, relation, a.indexCond, column))
			}
		}
		if maxBuffersPerRow > 0 && a.buffers > maxBuffersPerRow*a.rows+64 {
			failures = append(failures, fmt.Sprintf("%s on %s touched %.0f buffers for %.0f rows", a.nodeType, relation, a.buffers, a.rows))
		}
	}
	return failures
}
