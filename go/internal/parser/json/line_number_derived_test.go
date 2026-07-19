// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestJSONLineNumberDeterminism proves parsing the same package.json bytes
// twice yields byte-identical line_number values on every dependency and
// script row. content.CanonicalEntityID hashes line_number into the
// Variable/Function node uid (go/internal/content/writer.go:196), so a
// non-deterministic line_number would churn node identity on every
// re-ingest of an unchanged file. This is the churn-safety gate for the
// one-time identity migration issue #5329's line_number fix causes.
func TestJSONLineNumberDeterminism(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "package.json", `{
  "dependencies": {
    "zod": "^3.0.0",
    "@types/node": "^20.0.0"
  },
  "scripts": {
    "test": "vitest"
  }
}`)

	first, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() [first] error = %v, want nil", err)
	}
	second, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() [second] error = %v, want nil", err)
	}

	firstLines := lineNumbersByName(t, first, "variables")
	secondLines := lineNumbersByName(t, second, "variables")
	if !reflect.DeepEqual(firstLines, secondLines) {
		t.Fatalf("variables line_number map changed across identical re-parses: first = %#v, second = %#v", firstLines, secondLines)
	}

	firstFunctionLines := lineNumbersByName(t, first, "functions")
	secondFunctionLines := lineNumbersByName(t, second, "functions")
	if !reflect.DeepEqual(firstFunctionLines, secondFunctionLines) {
		t.Fatalf("functions line_number map changed across identical re-parses: first = %#v, second = %#v", firstFunctionLines, secondFunctionLines)
	}
}

func lineNumbersByName(t *testing.T, payload map[string]any, bucket string) map[string]int {
	t.Helper()

	rows, ok := payload[bucket].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", bucket, payload[bucket])
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		line, ok := row["line_number"].(int)
		if !ok {
			t.Fatalf("row[%q][line_number] = %T, want int", name, row["line_number"])
		}
		out[name] = line
	}
	return out
}

// TestDataIntelligenceReplayRowsRetainPositionalLineNumber pins issue #5358's
// derived-row decision: warehouse_replay.json asset/column/query rows
// summarize an external system's state, not one JSON source token, so no
// real source line exists for them. #5329 tried omitting line_number
// entirely instead of fabricating "line_number": 1, on the theory that
// materialize/query would then treat these rows as having no real source
// line -- but that theory was wrong: entityBucketsFromParsed's
// snapshotPayloadInt defaults an absent line_number to 0, and
// shape.indexedEntity.lineNumber() coerces any LineNumber < 1 back to 1
// before it is hashed into the entity's identity and persisted. So the
// materialized entity gets the exact same fabricated line 1 whether the
// parser omits line_number or states it explicitly; the omission changed
// nothing observable downstream, it only made the parser payload claim
// (falsely) that no line existed. Threading a genuine "no source line"
// sentinel through the shared shape/content contract (used by every language
// parser, and Postgres-schema-enforced NOT NULL) was assessed as
// disproportionate for this fix, so these rows now state the documented
// positional line_number: 1 placeholder explicitly instead of relying on an
// incidental coercion while claiming otherwise.
func TestDataIntelligenceReplayRowsRetainPositionalLineNumber(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "warehouse_replay.json", `{
  "metadata": {"workspace": "analytics"},
  "assets": [
    {
      "database": "raw",
      "schema": "public",
      "name": "orders",
      "kind": "table",
      "columns": [{"name": "id"}]
    }
  ],
  "query_history": [
    {"query_id": "q1", "name": "daily_load", "touched_assets": ["orders"]}
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	assets, ok := payload["data_assets"].([]map[string]any)
	if !ok || len(assets) == 0 {
		t.Fatalf("data_assets = %#v, want at least one row", payload["data_assets"])
	}
	for _, row := range assets {
		if got, ok := row["line_number"].(int); !ok || got != 1 {
			t.Errorf("data_assets row %#v line_number = %v, want the positional placeholder 1", row, row["line_number"])
		}
	}

	columns, ok := payload["data_columns"].([]map[string]any)
	if !ok || len(columns) == 0 {
		t.Fatalf("data_columns = %#v, want at least one row", payload["data_columns"])
	}
	for _, row := range columns {
		if got, ok := row["line_number"].(int); !ok || got != 1 {
			t.Errorf("data_columns row %#v line_number = %v, want the positional placeholder 1", row, row["line_number"])
		}
	}

	queries, ok := payload["query_executions"].([]map[string]any)
	if !ok || len(queries) == 0 {
		t.Fatalf("query_executions = %#v, want at least one row", payload["query_executions"])
	}
	for _, row := range queries {
		if got, ok := row["line_number"].(int); !ok || got != 1 {
			t.Errorf("query_executions row %#v line_number = %v, want the positional placeholder 1", row, row["line_number"])
		}
	}
}

// TestGovernanceReplayRowsRetainPositionalLineNumber pins issue #5358's
// derived-row decision for governance_replay.json owner/contract rows. See
// TestDataIntelligenceReplayRowsRetainPositionalLineNumber for the full
// rationale.
func TestGovernanceReplayRowsRetainPositionalLineNumber(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "governance_replay.json", `{
  "metadata": {"workspace": "analytics"},
  "owners": [
    {"owner_id": "o1", "name": "data-platform", "team": "platform", "owns_assets": ["orders"]}
  ],
  "contracts": [
    {"contract_id": "c1", "name": "orders-contract", "targets_assets": ["orders"]}
  ]
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	owners, ok := payload["data_owners"].([]map[string]any)
	if !ok || len(owners) == 0 {
		t.Fatalf("data_owners = %#v, want at least one row", payload["data_owners"])
	}
	for _, row := range owners {
		if got, ok := row["line_number"].(int); !ok || got != 1 {
			t.Errorf("data_owners row %#v line_number = %v, want the positional placeholder 1", row, row["line_number"])
		}
	}

	contracts, ok := payload["data_contracts"].([]map[string]any)
	if !ok || len(contracts) == 0 {
		t.Fatalf("data_contracts = %#v, want at least one row", payload["data_contracts"])
	}
	for _, row := range contracts {
		if got, ok := row["line_number"].(int); !ok || got != 1 {
			t.Errorf("data_contracts row %#v line_number = %v, want the positional placeholder 1", row, row["line_number"])
		}
	}
}

// TestNewlineIndexHandlesCRLF proves buildNewlineIndex/lineAt count only the
// '\n' byte of a CRLF pair, so a Windows-checked-out package.json still
// resolves real (not off-by-one-per-line) source lines.
func TestNewlineIndexHandlesCRLF(t *testing.T) {
	t.Parallel()

	source := []byte("{\r\n  \"a\": 1,\r\n  \"b\": 2\r\n}\r\n")
	idx := buildNewlineIndex(source)

	// Byte offset of "b" (the second key) should resolve to line 3.
	bOffset := int64(indexOfByte(source, '"', indexOfByte(source, '"', indexOfByte(source, '"', indexOfByte(source, '"', 0)+1)+1)+1))
	if got, want := idx.lineAt(bOffset), 3; got != want {
		t.Errorf("lineAt(%d) = %d, want %d", bOffset, got, want)
	}
}

func indexOfByte(data []byte, target byte, start int) int {
	for i := start; i < len(data); i++ {
		if data[i] == target {
			return i
		}
	}
	return -1
}

// TestUnmarshalOrderedJSONArrayLinesReportsElementStartLines is a focused
// unit test of the array-walk primitive tsconfig references and
// composer.lock packages/packages-dev rely on: each element's reported line
// must be where that element's value starts, not where the array itself
// starts or a running count of elements.
func TestUnmarshalOrderedJSONArrayLinesReportsElementStartLines(t *testing.T) {
	t.Parallel()

	data := []byte("[\n  1,\n  {\n    \"a\": 2\n  },\n  3\n]")
	idx := buildNewlineIndex(data)

	lines, err := unmarshalOrderedJSONArrayLines(data, 0, idx)
	if err != nil {
		t.Fatalf("unmarshalOrderedJSONArrayLines() error = %v, want nil", err)
	}
	want := []int{2, 3, 6}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("unmarshalOrderedJSONArrayLines() = %#v, want %#v", lines, want)
	}
}
