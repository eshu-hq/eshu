// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

func TestIaCFactRowsShapesEveryColumn(t *testing.T) {
	facts := BuildIaCFacts("scope-1", "gen-1", 1)
	rows, err := iacFactRows(facts, seedRowsTestNow)
	if err != nil {
		t.Fatalf("iacFactRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if len(row) != len(iacFactColumns) {
		t.Fatalf("len(row) = %d, want %d (len(iacFactColumns))", len(row), len(iacFactColumns))
	}

	factKindIdx := indexOf(iacFactColumns, "fact_kind")
	if row[factKindIdx] != "content_entity" {
		t.Errorf("fact_kind = %v, want content_entity", row[factKindIdx])
	}

	payloadIdx := indexOf(iacFactColumns, "payload")
	raw, ok := row[payloadIdx].([]byte)
	if !ok {
		t.Fatalf("payload column type = %T, want []byte (marshaled JSON)", row[payloadIdx])
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if decoded["entity_type"] != facts[0].EntityType {
		t.Errorf("payload entity_type = %v, want %v", decoded["entity_type"], facts[0].EntityType)
	}
}
