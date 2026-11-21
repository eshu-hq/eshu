// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// advisoryUpdateShape mirrors AdvisoryGroupNodeUpdateCypher (#11161): a keyed
// MATCH whose SET assigns only properties no schema key covers.
const advisoryUpdateShape = `MATCH (g:AdvisoryGroup {id: $id})
WITH g WHERE coalesce(g.cve_ids, []) <> $cve_ids
SET g.cve_ids = $cve_ids, g.advisory_ids = $advisory_ids, g.member_count = $member_count`

// TestAnalyzeIndexWritesClassifiesBoundNonKeyOnly pins the plan-level verdict
// the sweep consults: true only for a plain MATCH that names a full schema
// key and then SETs only non-key properties. Every variant that could write
// an unmeasured index key keeps the verdict false.
func TestAnalyzeIndexWritesClassifiesBoundNonKeyOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cypher string
		want   bool
	}{
		{"advisory update shape", advisoryUpdateShape, true},
		{"lowercase keywords", "match (g:AdvisoryGroup {id: $id}) set g.member_count = $n", true},
		{"UNWIND-driven keyed match", "UNWIND $rows AS row\nMATCH (n:Function {uid: row.uid})\nSET n.touched = row.at", true},
		{"relationship SET beside bound node SET", "MATCH (g:AdvisoryGroup {id: $id})\nMATCH (r:Repository {id: $rid})-[e:AFFECTED_BY]->(g)\nSET g.member_count = $n, e.purl = $purl", false},
		{"key property from nested UNWIND value", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (n:Function {uid: row.uid})\nSET n.uid = item.id", false},
		{"key property SET from a parameter", "MATCH (g:AdvisoryGroup {id: $id}) SET g.id = $new_id", false},
		{"map merge of an unresolved alias", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (n:Function {uid: row.uid})\nSET n += item.props", false},
		{"map replace", "MATCH (g:AdvisoryGroup {id: $id}) SET g = {member_count: $n}", false},
		{"opaque subscript SET", "MATCH (g:AdvisoryGroup {id: $id}) SET g['member_count'] = $n", false},
		{"opaque subscript SET beside a non-key SET", "MATCH (g:AdvisoryGroup {id: $id}) SET g.member_count = $n, g['name'] = $v", false},
		{"label added by SET", "MATCH (g:AdvisoryGroup {id: $id}) SET g:Module, g.member_count = $n", false},
		{"mixed recognized non-key SET and unreadable indexed write", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (g:AdvisoryGroup {id: $id})\nSET g.member_count = $n\nMERGE (x:Parameter {name: item.name, path: row.path, function_line_number: 1})", false},
		{"match names no full schema key", "MATCH (n:Function {signature: $sig}) SET n.touched = $t", false},
		{"match names only part of a composite key", "MATCH (n:Parameter {name: $name}) SET n.touched = $t", false},
		{"property that is a key only inside a composite key", "MATCH (n:Function {uid: $uid}) SET n.line_number = $line", false},
		{"property that is a single-property key", "MATCH (n:Function {uid: $uid}) SET n.name = $name", false},
		{"optional match is not a plain match", "OPTIONAL MATCH (g:AdvisoryGroup {id: $id}) SET g.member_count = $n", false},
		{"node also created by MERGE", "MERGE (g:AdvisoryGroup {id: $id})\nWITH g\nMATCH (g:AdvisoryGroup {id: $id})\nSET g.member_count = $n", false},
		{"SET target bound to no pattern", "MATCH (g:AdvisoryGroup {id: $id}) SET h.member_count = $n", false},
		{"no SET at all", "MATCH (g:AdvisoryGroup {id: $id}) RETURN g", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := analyzeIndexWrites(tc.cypher)
			if plan.boundNonKeyOnly != tc.want {
				t.Fatalf("boundNonKeyOnly = %v, want %v\nstatement:\n%s", plan.boundNonKeyOnly, tc.want, tc.cypher)
			}
		})
	}
}

// TestBoundNonKeyVerdictDoesNotTouchPlanVars pins the arbiter's invariant: the
// verdict is separate from plan.vars (only indexed-property writers), so the
// guard's zero-work fast path and per-row cost do not move.
func TestBoundNonKeyVerdictDoesNotTouchPlanVars(t *testing.T) {
	t.Parallel()

	plan := analyzeIndexWrites(advisoryUpdateShape)
	if len(plan.vars) != 0 {
		t.Fatalf("plan.vars = %d entries, want 0: a non-key SET must not enter vars", len(plan.vars))
	}
	if len(plan.unanalyzed) != 0 {
		t.Fatalf("unanalyzed = %+v, want none", plan.unanalyzed)
	}
}

// TestGuardIndexKeyWritesBehaviorAroundBoundNonKeyShape proves the verdict
// changed no guard behavior: an oversized $id on the MERGE create shape still
// skips, an oversized value on a non-key SET drops and skips nothing, and a
// MATCH + SET that writes the key property with an oversized value still skips.
func TestGuardIndexKeyWritesBehaviorAroundBoundNonKeyShape(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 9000)
	create := "MERGE (g:AdvisoryGroup {id: $id})\nON CREATE SET g.cve_ids = $cve_ids, g.member_count = $member_count"
	if _, dropped, skip := GuardIndexKeyWrites(create, map[string]any{"id": big, "cve_ids": []string{"c"}, "member_count": 1}); !skip || len(dropped) != 1 {
		t.Fatalf("oversized $id on create: skip=%v dropped=%+v, want skip=true with one record", skip, dropped)
	}
	params := map[string]any{"id": "g1", "cve_ids": []string{big}, "advisory_ids": []string{big}, "member_count": 1}
	out, dropped, skip := GuardIndexKeyWrites(advisoryUpdateShape, params)
	if skip || len(dropped) != 0 || len(out) != len(params) {
		t.Fatalf("oversized non-key SET: skip=%v dropped=%+v, want nothing dropped or skipped", skip, dropped)
	}
	keyed := "MATCH (g:AdvisoryGroup {id: $id})\nSET g.id = $new_id"
	if _, dropped, skip := GuardIndexKeyWrites(keyed, map[string]any{"id": "g1", "new_id": big}); !skip || len(dropped) != 1 {
		t.Fatalf("oversized key SET: skip=%v dropped=%+v, want skip=true with one record", skip, dropped)
	}
}

// TestSweepClassifiesBoundNonKeySeeds is the RED/GREEN seed pair for the
// sweep's bound-non-key verdict: the advisory shape passes, and each variant
// that could write an unmeasured index key is still found.
func TestSweepClassifiesBoundNonKeySeeds(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, "internal", "storage", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package storage\n\nconst q = "+strconv.Quote(body)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("bound_nonkey.go", advisoryUpdateShape)
	write("bound_nonkey_unwind.go", "UNWIND $rows AS row\nMATCH (n:Function {uid: row.uid})\nSET n.touched = row.at")
	write("nested_key.go", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (n:Function {uid: row.uid})\nSET n.uid = item.id")
	write("alias_map.go", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (n:Function {uid: row.uid})\nSET n += item.props")
	// The sweep's own candidate detector reads only var.prop SET targets, so the
	// opaque item rides beside a readable one to make the literal a candidate.
	write("opaque_set.go", "MATCH (g:AdvisoryGroup {id: $id}) SET g.member_count = $n, g['name'] = $v")
	write("mixed.go", "UNWIND $rows AS row\nUNWIND row.items AS item\nMATCH (g:AdvisoryGroup {id: $id})\nSET g.member_count = $n\nMERGE (x:Parameter {name: item.name, path: row.path, function_line_number: 1})")
	write("unkeyed_match.go", "MATCH (n:Function {signature: $sig}) SET n.touched = $t")

	findings, candidates := sweepIndexedWrites(t, root, []string{"internal/storage"})
	if candidates != 7 {
		t.Fatalf("sweep examined %d seed literals, want 7", candidates)
	}
	got := map[string]bool{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = true
	}
	for _, name := range []string{"bound_nonkey.go", "bound_nonkey_unwind.go"} {
		if got[name] {
			t.Errorf("%s flagged, want clean; findings = %v", name, findings)
		}
	}
	for _, name := range []string{"nested_key.go", "alias_map.go", "opaque_set.go", "mixed.go", "unkeyed_match.go"} {
		if !got[name] {
			t.Errorf("seeded violation %s not found; findings = %v", name, findings)
		}
	}
}
