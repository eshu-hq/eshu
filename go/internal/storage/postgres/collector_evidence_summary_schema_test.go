// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestBootstrapDefinitionsIncludeCollectorEvidenceSummary pins the #3466
// collector-readiness evidence summary table into the bootstrap schema. The
// readiness read joins this materialized table instead of scanning fact_records,
// so it must exist in fresh and bootstrapped data planes. If a later edit drops
// the grain primary key or the NOT NULL aggregate columns, the exact-count wire
// contract and the resweep upsert regress.
func TestBootstrapDefinitionsIncludeCollectorEvidenceSummary(t *testing.T) {
	t.Parallel()

	var def Definition
	for _, d := range BootstrapDefinitions() {
		if d.Name == "collector_evidence_summary" {
			def = d
			break
		}
	}
	if def.Name == "" {
		t.Fatal("collector_evidence_summary definition missing")
	}
	if def.Path != "go/internal/storage/postgres/migrations/036_collector_evidence_summary.sql" {
		t.Fatalf("unexpected Path %q", def.Path)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS collector_evidence_summary",
		"source_system     TEXT NOT NULL DEFAULT ''",
		"observation_count BIGINT NOT NULL",
		"materialized_at   TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, evidence_source, source_system)",
		"CREATE INDEX IF NOT EXISTS collector_evidence_summary_scope_gen_idx",
	} {
		if !strings.Contains(def.SQL, want) {
			t.Fatalf("collector evidence summary schema SQL missing %q:\n%s", want, def.SQL)
		}
	}
}
