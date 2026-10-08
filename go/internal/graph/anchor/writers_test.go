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
