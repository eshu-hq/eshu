// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"strings"
	"testing"
)

// TestFactRecordsAnchorPayloadTrgmIndexMigrationShape pins the #7584
// activation-obligation fix: the targeted anchor-scoped relationship fact
// load tests lower(fact.payload::text) with LIKE ANY over the anchor terms
// (scoped_facts.go), and without a serving index every keyset page runs a
// full parallel sequential scan with a whole-payload detoast, dying at the
// 300s page guard. Migration 360 must carry the partial trigram GIN over
// exactly that expression, limited to the three fact kinds the query reads,
// as one concurrent statement so the migration coordinator can build it
// without taking a write-blocking lock.
func TestFactRecordsAnchorPayloadTrgmIndexMigrationShape(t *testing.T) {
	t.Parallel()

	var found *Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records_anchor_payload_trgm_idx" {
			d := def
			found = &d
		}
	}
	if found == nil {
		t.Fatal("BootstrapDefinitions() has no fact_records_anchor_payload_trgm_idx migration")
	}
	sql := found.SQL
	const createLine = "CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_anchor_payload_trgm_idx"
	if !strings.Contains(sql, createLine) {
		t.Fatalf("360 must build concurrently with %q", createLine)
	}
	const exprLine = "ON fact_records USING gin (lower((payload::text)) gin_trgm_ops)"
	if !strings.Contains(sql, exprLine) {
		t.Fatalf("360 must index exactly the anchor predicate expression, want %q", exprLine)
	}
	const partialLine = "WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')"
	if !strings.Contains(sql, partialLine) {
		t.Fatalf("360 must stay partial to the anchor load's fact kinds, want %q", partialLine)
	}
	// Sole-statement pin: a second statement in the file would put the
	// whole file in one implicit transaction and fail the concurrent
	// build. Comment prose is stripped first so header punctuation cannot
	// trip it.
	var code strings.Builder
	for line := range strings.Lines(sql) {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line + "\n")
	}
	if strings.Count(code.String(), ";") != 1 {
		t.Fatalf("360 must hold exactly one statement, got %d semicolons", strings.Count(code.String(), ";"))
	}
}
