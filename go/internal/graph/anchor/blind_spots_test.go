// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import "testing"

// These tables record the known blind spots (not exhaustive) of the heuristic
// pre-filter. Each row is valid Cypher or Go that writes an id on a node with no
// anchor label and reports nothing today. The required graph/anchor_census check
// after the replay, and the reducer gauge on each deployment, are the authority
// for them. A row that starts reporting means the pre-filter improved: flip it
// into the matching finding table and update the docs, which name these classes.

func TestCheckWritersKnownBlindSpots(t *testing.T) {
	rows := []struct{ name, text, params string }{
		{
			"scope loss: a CALL subquery re-declares an outer name without a label",
			"MATCH (n:Function), (m:Unconstrained) CALL (m) { MATCH (n) WHERE n = m SET n.id = 1 } RETURN 1", "",
		},
		{
			"procedure writer outside the named apoc families (vector property)",
			"MATCH (n:Unconstrained {uid: $u}) CALL db.create.setNodeVectorProperty(n, 'id', $v) RETURN n", "",
		},
		{
			"procedure writer outside the named apoc families (atomic add)",
			"MATCH (n:Unconstrained {uid: $u}) CALL apoc.atomic.add(n, 'id', 1) YIELD oldValue RETURN oldValue", "",
		},
		{
			"UNWIND alias rebound by a YIELD column of the same name",
			"UNWIND $rows AS row CALL my.proc() YIELD row MERGE (n:Unconstrained {uid: $u}) SET n += row", `{"rows":[{"a":1}]}`,
		},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			if report := check(t, tc.text, tc.params); len(report.Findings) != 0 {
				t.Fatalf("%q now reports %+v: it is no longer a blind spot; move it to a finding table and update the docs", tc.text, report.Findings)
			}
		})
	}
}

func TestSweepKnownBlindSpots(t *testing.T) {
	rows := []struct{ name, source string }{
		{
			"a package var in a plus chain",
			"package planted\n\nvar head = \"MERGE (n:Unconstrained {uid: $u}) \"\n\nvar statement = head + \"SET n.id = $u\"\n",
		},
		{
			"a cross-package constant in a plus chain",
			"package planted\n\nimport \"example.test/other\"\n\nvar statement = other.Head + \"SET n.id = $u\"\n",
		},
		{
			"a function call operand in a plus chain",
			"package planted\n\nfunc head() string { return \"MERGE (n:Unconstrained {uid: $u}) \" }\n\nvar statement = head() + \"SET n.id = $u\"\n",
		},
		{
			"strings.Builder assembly",
			"package planted\n\nimport \"strings\"\n\nfunc build() string {\n\tvar b strings.Builder\n\tb.WriteString(\"MERGE (n:Unconstrained {uid: $u}) \")\n\tb.WriteString(\"SET n.id = $u\")\n\treturn b.String()\n}\n",
		},
		{
			"an fmt verb that supplies the id key",
			"package planted\n\nimport \"fmt\"\n\nvar statement = fmt.Sprintf(\"MERGE (n:Unconstrained {%s: $v})\", \"id\")\n",
		},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			result := sweepSource(t, plantedTree(t, tc.source, ""), Labels())
			if len(result.Failures) != 0 || len(result.Dynamic) != 0 {
				t.Fatalf("the sweep now sees %q (failures %v, dynamic %d): it is no longer a blind spot; move it to a finding table and update the docs",
					tc.name, result.Failures, len(result.Dynamic))
			}
		})
	}
}
