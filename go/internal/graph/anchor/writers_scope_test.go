// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import "testing"

// Scope, SET-target, and label-removal shapes: valid Cypher that leaves an id on a node with
// no anchor label. Each row must be a finding of the named kind; the clean rows
// beside them keep the stricter parser from turning production writes red.
func TestCheckWritersScopeTargetAndRemovalShapes(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		params string
		kind   string
	}{
		// (a) a relationship name reused as a node variable after scope loss.
		{"relationship name reused as a node after WITH", "MATCH ()-[n]->() WITH count(n) AS c CREATE (n:Unconstrained {id: $id})", "", KindMapKey},
		{"relationship name reused as a node after UNION", "MATCH (a)-[n]->(b) RETURN a.x AS x UNION CREATE (n:Unconstrained {id: $id}) RETURN 1 AS x", "", KindMapKey},
		{"relationship name reused as a matched node", "MATCH ()-[n]->() WITH count(n) AS c MATCH (n:Unconstrained) SET n.id = 1", "", KindSetProperty},
		// (b) SET targets that are not a plain ASCII variable.
		{"backtick variable whole map", "CREATE (`n`:Unconstrained) SET `n` += {id: $id}", "", KindDynamicMap},
		{"backtick variable replace map", "CREATE (`n`:Unconstrained) SET `n` = {id: $id}", "", KindDynamicMap},
		{"backtick variable parameter map with id", "CREATE (`n`:Unconstrained) SET `n` += $props", `{"props":{"id":"x"}}`, KindDynamicMap},
		{"backtick variable dynamic key", "CREATE (`n`:Unconstrained) SET `n`[$k] = $v", `{"k":"id","v":"x"}`, KindDynamicMap},
		{"non-ASCII variable whole map", "CREATE (é:Unconstrained) SET é += {id: $id}", "", KindDynamicMap},
		// (c) removing an anchor label strands the id.
		{"REMOVE the anchor label then SET id", "MATCH (n:Function {uid: $u}) REMOVE n:Function SET n.id = $u", "", KindLabelRemoval},
		{"SET id then REMOVE the anchor label", "MATCH (n:Function {uid: $u}) SET n.id = $u REMOVE n:Function", "", KindLabelRemoval},
		{"CREATE with id then REMOVE the label", "CREATE (n:Function {id: $id}) REMOVE n:Function", "", KindLabelRemoval},
		{"REMOVE every anchor label of an id-written node", "MATCH (n:Function:Repository {uid: $u}) SET n.id = $u REMOVE n:Function, n:Repository", "", KindLabelRemoval},
		// D: an unlabeled re-declaration after a WITH that does not project the name.
		{"unlabeled MATCH after WITH drops n", "MATCH (n:Function {uid: $u}) WITH n.uid AS u MATCH (n) WHERE n.uid = u SET n.id = u", "", KindSetProperty},
		{"unlabeled MATCH after UNION", "MATCH (n:Function) RETURN n.uid AS u UNION MATCH (n) SET n.id = 1 RETURN n.uid AS u", "", KindSetProperty},
		{"unlabeled CREATE map after WITH count", "MATCH (n:Function) WITH count(n) AS c CREATE (n {id: $id})", "", KindMapKey},
		{"unlabeled CREATE then SET after WITH count", "MATCH (n:Function) WITH count(n) AS c CREATE (n) SET n.id = $id", "", KindSetProperty},
		// F: a clause keyword inside a backtick identifier.
		{"keyword WHERE inside a backtick path name", "CREATE `p where` = (n:Unconstrained {id: $id})", "", KindMapKey},
		{"keyword MATCH inside a backtick path name", "CREATE `x match` = (n:Unconstrained {id: $id})", "", KindMapKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := check(t, tc.text, tc.params)
			for _, f := range report.Findings {
				if f.Kind == tc.kind {
					return
				}
			}
			t.Fatalf("no %s finding for %q (report %+v)", tc.kind, tc.text, report)
		})
	}
}

func TestCheckWritersScopeTargetAndRemovalKeepProductionClean(t *testing.T) {
	clean := []struct{ name, text, params string }{
		{"relationship id write is still skipped", "MATCH (a:Function)-[r:R]->(b:Function) SET r.id = $id", ""},
		{"relationship id map is still skipped", "MATCH (a:Function), (b:Function) MERGE (a)-[r:LINKS {id: $id}]->(b)", ""},
		{"backtick property of a plain variable", "MERGE (n:Function {uid: $u}) SET n.`my prop` = 1, n.name = $x", ""},
		{"non-ASCII property of a plain variable", "MERGE (n:Function {uid: $u}) SET n.é = 1", ""},
		{"label swap that writes no id (the tfstate migration shape)", "MATCH (r:Function) WHERE r.uid IN $uids AND r.evidence_source = 'projector/tfstate' SET r:Repository REMOVE r:Function", ""},
		{"REMOVE of one anchor label while another stays on an id-written node", "MATCH (n:Function:Repository {uid: $u}) SET n.id = $u REMOVE n:Function", ""},
		{"REMOVE of an anchor label in a statement that writes no id", "MATCH (n:Function) REMOVE n:Function", ""},
		{"REMOVE of a non-anchor label", "MATCH (n:Unconstrained) REMOVE n:Unconstrained", ""},
		{"REMOVE of a property", "MATCH (n:Function) REMOVE n.stale", ""},
		{"WITH projects the variable", "MATCH (n:Function {uid: $u}) WITH n, count(*) AS c SET n.id = $u", ""},
		{"WITH star projects everything", "MATCH (n:Function {uid: $u}) WITH * SET n.id = $u", ""},
		{"WITH DISTINCT projects the variable", "MATCH (n:Function {uid: $u}) WITH DISTINCT n SET n.id = $u", ""},
		{"UNWIND then MATCH keeps its own labels", "UNWIND $rows AS row MATCH (n:Function {uid: row.uid}) SET n.id = row.uid", ""},
		{"keyword text inside a backtick property key", "CREATE (n:Function {`where match`: 1, id: $id})", ""},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			if report := check(t, tc.text, tc.params); len(report.Findings) != 0 {
				t.Fatalf("clean statement flagged: %+v", report.Findings)
			}
		})
	}
}
