// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"strings"
	"testing"
)

// testAnchor is a small stand-in for the schema-derived label set, so the
// cases below stay readable. Labels() itself is pinned against the schema in
// TestLabelsIsTheUnionOfBothConstraintSets.
var testAnchor = map[string]bool{"Function": true, "Repository": true, "Endpoint": true, "File": true}

func check(t *testing.T, text, params string) Report {
	t.Helper()
	return CheckWriters([]Statement{{Text: text, Parameters: params, Callsite: "x.go:build"}}, testAnchor)
}

func TestCheckWriters(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		params     string
		wantWrites int
		wantKinds  []string
	}{
		{
			name:       "planted MERGE with id on an unconstrained label is a finding",
			text:       "MERGE (n:Unconstrained {id: $entity_id}) SET n.name = $name",
			wantWrites: 1,
			wantKinds:  []string{"map_key"},
		},
		{
			name:       "MERGE with id on a constrained label is clean",
			text:       "MERGE (r:Repository {id: $repo_id}) SET r.name = $name",
			wantWrites: 1,
		},
		{
			name:       "SET id on an unconstrained label is a finding",
			text:       "UNWIND $rows AS row MERGE (n:Unconstrained {uid: row.uid}) SET n.id = row.uid",
			wantWrites: 1,
			wantKinds:  []string{"set_property"},
		},
		{
			name:       "SET id on a uid-constrained label is clean",
			text:       "UNWIND $rows AS row MERGE (n:Function {uid: row.uid}) SET n.id = row.uid, n.name = row.name",
			wantWrites: 1,
		},
		{
			name:       "ON CREATE SET id on an unconstrained label is a finding",
			text:       "MERGE (n:Unconstrained {uid: $uid}) ON CREATE SET n.id = $uid",
			wantWrites: 1,
			wantKinds:  []string{"set_property"},
		},
		{
			name:       "one constrained label among several covers the node",
			text:       "MERGE (n:Unconstrained:Function {id: $id})",
			wantWrites: 1,
		},
		{
			name:       "label bound by an earlier MATCH covers a later SET",
			text:       "MATCH (n:Function {uid: $uid}) SET n.id = $uid",
			wantWrites: 1,
		},
		{
			name:       "a MATCH id predicate is a read, not a write",
			text:       "MATCH (n:Unconstrained {id: $id}) WHERE n.id = $id RETURN n",
			wantWrites: 0,
		},
		{
			name:       "a WHERE clause after SET does not make the predicate a write",
			text:       "MATCH (n:Unconstrained) SET n.seen = true WITH n WHERE n.id = $id RETURN n",
			wantWrites: 0,
		},
		{
			name:       "a relationship id is not a node id",
			text:       "MATCH (a:Unconstrained), (b:Unconstrained) MERGE (a)-[r:LINKS {id: $id}]->(b) SET r.id = $id",
			wantWrites: 0,
		},
		{
			name:       "an unlabeled variable cannot be proven covered",
			text:       "MATCH (n) WHERE n.k = $k SET n.id = $id",
			wantWrites: 1,
			wantKinds:  []string{"set_property"},
		},
		{
			name:       "lowercase keywords are still seen",
			text:       "merge (n:Unconstrained {id: $id})",
			wantWrites: 1,
			wantKinds:  []string{"map_key"},
		},
		{
			name:       "a label spelled like a keyword is a label",
			text:       "MERGE (n:Set {id: $id})",
			wantWrites: 1,
			wantKinds:  []string{"map_key"},
		},
		{
			name:       "text inside a string literal is not a write",
			text:       "MATCH (n:Function) SET n.note = 'SET n.id = 1 MERGE (x:Bad {id: 2})'",
			wantWrites: 0,
		},
		{
			name:       "a dynamic map on an unconstrained label fails closed without parameters",
			text:       "UNWIND $rows AS row MERGE (n:Unconstrained {uid: row.uid}) SET n += row.props",
			wantWrites: 1,
			wantKinds:  []string{"dynamic_map"},
		},
		{
			name:       "a dynamic map fails when one row carries an id key",
			text:       "UNWIND $rows AS row MERGE (n:Unconstrained {uid: row.uid}) SET n += row.props",
			params:     `{"rows":[{"uid":"u1","props":{"name":"a"}},{"uid":"u2","props":{"name":"b","id":null}}]}`,
			wantWrites: 1,
			wantKinds:  []string{"dynamic_map"}, // the second row carries an id key
		},
		{
			name:       "a dynamic map is clean when every row proves no id key",
			text:       "UNWIND $rows AS row MERGE (n:Unconstrained {uid: row.uid}) SET n += row.props",
			params:     `{"rows":[{"uid":"u1","id":"u1","props":{"name":"a"}},{"uid":"u2","props":{"name":"b"}}]}`,
			wantWrites: 0,
		},
		{
			name:       "a dynamic map on a constrained label needs no proof",
			text:       "UNWIND $rows AS row MERGE (n:Function {uid: row.uid}) SET n += row.props",
			wantWrites: 1,
		},
		{
			name:       "a plain parameter map is resolved by its name",
			text:       "MERGE (n:Unconstrained {uid: $uid}) SET n += $props",
			params:     `{"uid":"u","props":{"name":"a"}}`,
			wantWrites: 0,
		},
		{
			name:       "an unresolvable dynamic map expression fails closed",
			text:       "MERGE (n:Unconstrained {uid: $uid}) SET n += apoc.map.merge($a, $b)",
			params:     `{"uid":"u","a":{},"b":{}}`,
			wantWrites: 1,
			wantKinds:  []string{"dynamic_map"},
		},
		{
			name:       "a map literal with an id key is an id write",
			text:       "MERGE (n:Unconstrained {uid: $uid}) SET n += {id: $uid, name: $name}",
			wantWrites: 1,
			wantKinds:  []string{"dynamic_map"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := check(t, tc.text, tc.params)
			var gotKinds []string
			for _, f := range report.Findings {
				gotKinds = append(gotKinds, f.Kind)
			}
			if strings.Join(gotKinds, ",") != strings.Join(tc.wantKinds, ",") {
				t.Fatalf("finding kinds = %v, want %v (report %+v)", gotKinds, tc.wantKinds, report)
			}
			wantWrites := tc.wantWrites
			if report.IDWrites != wantWrites {
				t.Fatalf("IDWrites = %d, want %d", report.IDWrites, wantWrites)
			}
		})
	}
}

func TestCheckWritersDeduplicatesRepeatedStatements(t *testing.T) {
	st := Statement{Text: "MERGE (n:Unconstrained {id: $id})", Callsite: "x.go:build"}
	report := CheckWriters([]Statement{st, st, st}, testAnchor)
	if report.Statements != 1 || report.IDWrites != 1 || len(report.Findings) != 1 {
		t.Fatalf("report = %+v, want one statement, one id write, one finding", report)
	}
	if report.Findings[0].Callsite != "x.go:build" || report.Findings[0].Variable != "n" {
		t.Fatalf("finding = %+v, want callsite and variable carried", report.Findings[0])
	}
}

func TestLabelsIsTheUnionOfBothConstraintSets(t *testing.T) {
	labels := Labels()
	if len(labels) != len(UIDLabels())+len(IDLabels()) {
		t.Fatalf("union size %d != uid %d + id %d (sets must be disjoint)", len(labels), len(UIDLabels()), len(IDLabels()))
	}
	for _, want := range []string{"Function", "Repository", "Endpoint", "EvidenceArtifact", "CloudAction", "Platform"} {
		if !labels[want] {
			t.Errorf("anchor labels miss %q", want)
		}
	}
	if labels["Directory"] {
		t.Error("Directory has neither constraint and must not be an anchor label")
	}
}

// Shapes the first parser read wrongly and failed open on (review F4). Each is
// a real Cypher write of an id on an uncovered label; each must be a finding.
func TestCheckWritersFailsClosedOnShapesItCannotParse(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		params string
		kind   string
	}{
		{
			name: "a WHERE inside a list comprehension does not end the SET item",
			text: "MERGE (n:Unconstrained {uid: $u}) SET n.tags = [t IN $tags WHERE t <> ''], n.id = $id",
			kind: KindSetProperty,
		},
		{
			name: "a WHERE inside a list comprehension in a SET map literal",
			text: "MERGE (n:Unconstrained {uid: $u}) SET n += {tags: [t IN $tags WHERE t <> ''], id: $id}",
			kind: KindDynamicMap,
		},
		{
			name: "an EXISTS subquery inside a SET item",
			text: "MERGE (n:Unconstrained {uid: $u}) SET n.flag = EXISTS { MATCH (n)-[:R]->() }, n.id = $u",
			kind: KindSetProperty,
		},
		{
			name: "a list literal of a node is not a relationship variable",
			text: "MERGE (n:Unconstrained {id: $id}) WITH [n] AS ns RETURN ns",
			kind: KindMapKey,
		},
		{
			name: "a list index is not a relationship variable",
			text: "UNWIND $rows AS row UNWIND range(0, 1) AS i CREATE (i:Unconstrained {id: row.ids[i]})",
			kind: KindMapKey,
		},
		{
			name: "a label conjunction with an ampersand",
			text: "CREATE (n:Unconstrained&Other {id: $id})",
			kind: KindMapKey,
		},
		{
			name: "a label disjunction on a bound variable is unknown",
			text: "MATCH (n:Function|Unconstrained {uid: $u}) SET n.id = $u",
			kind: KindSetProperty,
		},
		{
			name:   "a dynamic property key that is id",
			text:   "MERGE (n:Unconstrained {uid: $u}) SET n[$k] = $v",
			params: `{"u":"u","k":"id","v":"x"}`,
			kind:   KindDynamicMap,
		},
		{
			name: "a dynamic property key without parameters fails closed",
			text: "MERGE (n:Unconstrained {uid: $u}) SET n[$k] = $v",
			kind: KindDynamicMap,
		},
		{
			name: "a property map with a nested map is not placed, and its id key still fails closed",
			text: "MERGE (n:Function {id: $id, meta: {a: 1}})",
			kind: KindMapKey,
		},
		{
			name: "a pattern the parser cannot place still carries an id key",
			text: "MERGE (a:Unconstrained)-[:R]->(b:Unconstrained) MERGE p = (c:Unconstrained {id: $id}) RETURN p",
			kind: KindMapKey,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := check(t, tc.text, tc.params)
			if len(report.Findings) == 0 {
				t.Fatalf("no finding for %q (report %+v)", tc.text, report)
			}
			found := false
			for _, f := range report.Findings {
				if f.Kind == tc.kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("findings %+v lack kind %s", report.Findings, tc.kind)
			}
		})
	}
}

// The counterpart rows: the same shapes on covered labels or with proof stay
// clean, so the stricter parser does not turn production writes red.
func TestCheckWritersStricterParserKeepsCoveredWritesClean(t *testing.T) {
	clean := []struct{ name, text, params string }{
		{"list comprehension in SET on a covered label", "MERGE (n:Function {uid: $u}) SET n.tags = [t IN $tags WHERE t <> ''], n.id = $id", ""},
		{"EXISTS in SET on a covered label", "MERGE (n:Function {uid: $u}) SET n.flag = EXISTS { MATCH (n)-[:R]->() }, n.id = $u", ""},
		{"conjunction with a covered label", "CREATE (n:Function&Other {id: $id})", ""},
		{"relationship id map is not a node write", "MATCH (a:Function), (b:Function) MERGE (a)-[r:LINKS {id: $id}]->(b)", ""},
		{"relationship id with a bracketed index", "MATCH (a:Function), (b:Function) MERGE (a)-[r:LINKS {id: $ids[0]}]->(b)", ""},
		{"dynamic key proven not to be id", "MERGE (n:Unconstrained {uid: $u}) SET n[$k] = $v", `{"u":"u","k":"name","v":"x"}`},
		{"a list literal in a read", "MATCH (n:Unconstrained) WITH [n] AS ns RETURN ns", ""},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			report := check(t, tc.text, tc.params)
			if len(report.Findings) != 0 {
				t.Fatalf("clean statement flagged: %+v", report.Findings)
			}
		})
	}
}
