// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// semanticFunctionShape mirrors the reducer semantic-entity legacy-rows
// Function upsert (storage/cypher semanticFunctionUpsertCypher), the path
// that bypassed the canonical guard on Neo4j (#7058 review F1).
const semanticFunctionShape = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Function {uid: row.entity_id})
SET n.id = row.entity_id,
    n.name = row.entity_name,
    n.path = row.file_path,
    n.line_number = row.start_line,
    n.semantic_kind = coalesce(row.semantic_kind, row.entity_type),
    n.evidence_source = row.evidence_source
MERGE (f)-[:CONTAINS]->(n)`

func functionRow(id, name, path string) map[string]any {
	return map[string]any{
		"entity_id": id, "entity_name": name, "file_path": path,
		"start_line": 1, "repo_id": "repo-1",
	}
}

func rowIDs(t *testing.T, params map[string]any) []string {
	t.Helper()
	var ids []string
	switch rows := params["rows"].(type) {
	case []map[string]any:
		for _, r := range rows {
			ids = append(ids, fmt.Sprint(r["entity_id"]))
		}
	case []any:
		for _, r := range rows {
			ids = append(ids, fmt.Sprint(r.(map[string]any)["entity_id"]))
		}
	default:
		t.Fatalf("rows param type %T", params["rows"])
	}
	return ids
}

func TestGuardIndexKeyWritesDropsOversizedSemanticFunctionRow(t *testing.T) {
	t.Parallel()

	params := map[string]any{"rows": []map[string]any{
		functionRow("ok", "handler", "src/a.go"),
		functionRow("big-name", strings.Repeat("n", 9000), "src/b.go"),
		// 4000 + 4001 bytes: each slot alone is fine, the composite
		// (name, path, line_number) key is not.
		functionRow("big-composite", strings.Repeat("n", 4000), strings.Repeat("p", 4001)),
		functionRow("at-limit", strings.Repeat("n", 4000), strings.Repeat("p", 4000)),
	}}

	out, dropped, skip := GuardIndexKeyWrites(semanticFunctionShape, params)
	if skip {
		t.Fatal("skip = true, want row-level drops only")
	}
	if got, want := rowIDs(t, out), []string{"ok", "at-limit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kept rows = %v, want %v", got, want)
	}
	if len(dropped) != 2 {
		t.Fatalf("dropped = %d records, want 2: %+v", len(dropped), dropped)
	}
	if d := dropped[0]; d.Label != "Function" || d.Property != "name" || d.KeyBytes != 9008 || d.EntityID != "big-name" || d.RepoID != "repo-1" {
		t.Fatalf("dropped[0] = %+v, want Function/name 9008 bytes for big-name", d)
	}
	if d := dropped[1]; d.Property != "path" || d.KeyBytes != 8001 || len(d.ValuePrefix) > 64 {
		t.Fatalf("dropped[1] = %+v, want path-dominated 8001-byte composite", d)
	}
	if n := len(params["rows"].([]map[string]any)); n != 4 {
		t.Fatalf("caller rows mutated: len = %d, want 4", n)
	}
}

func TestGuardIndexKeyWritesReturnsInputWhenNothingOversized(t *testing.T) {
	t.Parallel()

	params := map[string]any{"rows": []map[string]any{functionRow("a", "f", "p")}}
	out, dropped, skip := GuardIndexKeyWrites(semanticFunctionShape, params)
	if skip || len(dropped) != 0 {
		t.Fatalf("skip=%v dropped=%v, want neither", skip, dropped)
	}
	if reflect.ValueOf(out).Pointer() != reflect.ValueOf(params).Pointer() {
		t.Fatal("params map copied although nothing was dropped")
	}
}

func TestGuardIndexKeyWritesInlineMergeMapAndAnyRows(t *testing.T) {
	t.Parallel()

	cypher := `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (m:Module {name: row.module_name})
MERGE (f)-[:IMPORTS]->(m)`
	params := map[string]any{"rows": []any{
		map[string]any{"entity_id": "keep", "module_name": "fmt", "file_path": strings.Repeat("p", 9000)},
		map[string]any{"entity_id": "drop", "module_name": strings.Repeat("m", 8001), "file_path": "a.go"},
	}}
	out, dropped, _ := GuardIndexKeyWrites(cypher, params)
	// The oversized File path sits in a MATCH anchor, which is a lookup and
	// never an index write, so that row stays.
	if got, want := rowIDs(t, out), []string{"keep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kept rows = %v, want %v (dropped %+v)", got, want, dropped)
	}
	if len(dropped) != 1 || dropped[0].Label != "Module" || dropped[0].Property != "name" {
		t.Fatalf("dropped = %+v, want one Module/name", dropped)
	}
}

func TestGuardIndexKeyWritesMapMergeCoversCompositeKind(t *testing.T) {
	t.Parallel()

	// K8sResource keys on (name, kind, path, line_number); kind and name come
	// through a property map, as the canonical entity writer does.
	cypher := `UNWIND $rows AS row
MERGE (n:K8sResource {uid: row.entity_id})
SET n += row.props, n.path = row.file_path`
	params := map[string]any{"rows": []map[string]any{
		{"entity_id": "drop", "file_path": strings.Repeat("p", 7900), "props": map[string]any{"name": "svc", "kind": strings.Repeat("k", 200)}},
		{"entity_id": "keep", "file_path": strings.Repeat("p", 7900), "props": map[string]any{"name": "svc", "kind": "Service"}},
	}}
	out, dropped, _ := GuardIndexKeyWrites(cypher, params)
	if got, want := rowIDs(t, out), []string{"keep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kept rows = %v, want %v (dropped %+v)", got, want, dropped)
	}
}

func TestGuardIndexKeyWritesIgnoresWhereComparisons(t *testing.T) {
	t.Parallel()

	cypher := `UNWIND $rows AS row
MATCH (n:Function)
WHERE n.name = row.entity_name
SET n.touched = true`
	params := map[string]any{"rows": []map[string]any{functionRow("a", strings.Repeat("n", 9000), "p")}}
	if _, dropped, skip := GuardIndexKeyWrites(cypher, params); skip || len(dropped) != 0 {
		t.Fatalf("WHERE comparison treated as a write: skip=%v dropped=%+v", skip, dropped)
	}
}

func TestGuardIndexKeyWritesSkipsScalarStatement(t *testing.T) {
	t.Parallel()

	cypher := `MERGE (s:TerraformStateResource {uid: $uid}) SET s.address = $address`
	_, dropped, skip := GuardIndexKeyWrites(cypher, map[string]any{"uid": "u", "address": strings.Repeat("a", 8001)})
	if !skip || len(dropped) != 1 || dropped[0].Property != "address" {
		t.Fatalf("skip=%v dropped=%+v, want skip with one TerraformStateResource/address", skip, dropped)
	}
	_, _, skip = GuardIndexKeyWrites(cypher, map[string]any{"uid": "u", "address": strings.Repeat("a", 8000)})
	if skip {
		t.Fatal("8000-byte address skipped, want kept at the limit")
	}
}

func TestGuardIndexKeyWritesSumsConcatenation(t *testing.T) {
	t.Parallel()

	cypher := `UNWIND $rows AS row
MERGE (m:Module {name: row.prefix + row.suffix})`
	params := map[string]any{"rows": []map[string]any{{"prefix": strings.Repeat("a", 4001), "suffix": strings.Repeat("b", 4000)}}}
	if _, dropped, _ := GuardIndexKeyWrites(cypher, params); len(dropped) != 1 {
		t.Fatalf("dropped = %+v, want the 8001-byte concatenation dropped", dropped)
	}
}

// TestGuardIndexKeyWritesCoversEverySchemaKey drives one synthetic write per
// schema index key and requires the guard to drop it once the key exceeds the
// bound. It fails when a schema key exists that the guard cannot see.
func TestGuardIndexKeyWritesCoversEverySchemaKey(t *testing.T) {
	t.Parallel()

	keys, err := SchemaIndexKeys()
	if err != nil {
		t.Fatalf("SchemaIndexKeys() error = %v", err)
	}
	for _, key := range keys {
		sets := make([]string, 0, len(key.Properties))
		row := map[string]any{}
		for i, prop := range key.Properties {
			field := fmt.Sprintf("f%d", i)
			sets = append(sets, fmt.Sprintf("n.%s = row.%s", prop, field))
			row[field] = strings.Repeat("x", MaxIndexKeyBytes/len(key.Properties)+1)
		}
		cypher := fmt.Sprintf("UNWIND $rows AS row\nMERGE (n:%s {uid: row.uid})\nSET %s", key.Label, strings.Join(sets, ",\n    "))
		_, dropped, _ := GuardIndexKeyWrites(cypher, map[string]any{"rows": []map[string]any{row}})
		if len(dropped) != 1 {
			t.Errorf("index %s on %s%v: dropped = %d, want 1", key.Name, key.Label, key.Properties, len(dropped))
		}
	}
}

// BenchmarkGuardIndexKeyWrites measures the guard's per-statement cost on the
// two hot shapes: a 500-row semantic Function batch (explicit SET fields) and
// a 500-row canonical entity batch (SET n += row.props), with nothing
// oversized, which is the steady state.
func BenchmarkGuardIndexKeyWrites(b *testing.B) {
	semanticRows := make([]map[string]any, 500)
	canonicalRows := make([]map[string]any, 500)
	for i := range semanticRows {
		semanticRows[i] = functionRow(fmt.Sprintf("content-entity:e_%012d", i), fmt.Sprintf("handler%d", i), "/repo/src/service/handlers.go")
		canonicalRows[i] = map[string]any{
			"entity_id": fmt.Sprintf("content-entity:e_%012d", i),
			"props": map[string]any{
				"name": fmt.Sprintf("handler%d", i), "path": "/repo/src/service/handlers.go",
				"line_number": i, "repo_id": "repo-1", "lang": "go",
			},
		}
	}
	canonicalShape := "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\nMERGE (n:Function {uid: row.entity_id})\nSET n += row.props\nMERGE (f)-[rel:CONTAINS]->(n)\nSET rel.evidence_source = 'projector/canonical',\n    rel.generation_id = row.generation_id"
	for _, bc := range []struct {
		name   string
		cypher string
		rows   []map[string]any
	}{
		{"semantic_function_500", semanticFunctionShape, semanticRows},
		{"canonical_entity_props_500", canonicalShape, canonicalRows},
	} {
		b.Run(bc.name, func(b *testing.B) {
			params := map[string]any{"rows": bc.rows}
			b.ReportAllocs()
			for b.Loop() {
				if _, dropped, _ := GuardIndexKeyWrites(bc.cypher, params); dropped != nil {
					b.Fatal("unexpected drop")
				}
			}
		})
	}
}
