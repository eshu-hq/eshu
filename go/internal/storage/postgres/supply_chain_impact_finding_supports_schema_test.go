// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestSupplyChainImpactFindingSupportsMigrationShape pins the contract of
// migration 338 (#10880 PR-B1): the row identity is the support's fact id, not
// (canonical_key, scope_id), because the SQL canonical key collapses the CVE and
// advisory ids the writer's fact id keeps apart; the generation id cascades from
// scope_generations so retention and repository retirement remove supports with
// their facts; and each index is one a measured plan needs.
func TestSupplyChainImpactFindingSupportsMigrationShape(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("migrations/338_supply_chain_impact_finding_supports.sql")
	if err != nil {
		t.Fatalf("read migration 338: %v", err)
	}
	ddl := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports",
		"fact_id TEXT PRIMARY KEY",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"payload JSONB NOT NULL",
		"(scope_id, generation_id)",
		"supply_chain_impact_finding_supports_repository_idx ON supply_chain_impact_finding_supports (repository_id)",
		"supply_chain_impact_finding_supports_finding_idx ON supply_chain_impact_finding_supports (finding_id)",
		"(canonical_key) WHERE scope_id = 'operator:vulnerability_suppressions'",
		// The generation foreign key's cascade reads the supports by generation_id
		// once per deleted generation; PostgreSQL 18.6 reads the whole
		// (scope_id, generation_id) index for it, so this index is required.
		"supply_chain_impact_finding_supports_generation_idx ON supply_chain_impact_finding_supports (generation_id)",
		// The old-writer fence: a dirty-scope table and triggers that skip rows
		// written under the maintainer's transaction setting.
		"CREATE TABLE IF NOT EXISTS supply_chain_impact_finding_supports_dirty ( scope_id TEXT PRIMARY KEY",
		// The fence function runs as its owner with a pinned search_path and no
		// PUBLIC execute (the form of migration 307); the live proofs pin the
		// posture and the behavior per schema.
		"RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$",
		"ALTER FUNCTION supply_chain_impact_finding_supports_mark_dirty() SET search_path FROM CURRENT",
		"REVOKE ALL ON FUNCTION supply_chain_impact_finding_supports_mark_dirty() FROM PUBLIC",
		"CREATE TRIGGER supply_chain_impact_finding_supports_dirty_insert AFTER INSERT ON fact_records FOR EACH ROW",
		"CREATE TRIGGER supply_chain_impact_finding_supports_dirty_update AFTER UPDATE ON fact_records FOR EACH ROW",
		"NEW.fact_kind = 'reducer_supply_chain_impact_finding'",
		"current_setting('eshu.supply_chain_impact_supports_writer', true) IS DISTINCT FROM 'supports_v1'",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("migration 338 lacks %q", want)
		}
	}
	for _, forbidden := range []string{
		"UNIQUE (canonical_key", "PRIMARY KEY (canonical_key", "UNIQUE (scope_id, canonical_key",
	} {
		if strings.Contains(ddl, forbidden) {
			t.Errorf("migration 338 keys the table by %q: (canonical_key, scope_id) is not unique among supports", forbidden)
		}
	}
}

// holderIndexStatements are the single statements of migrations 339 to 342
// (#11197). 341 carries fastupdate = off and 342 carries no fastupdate option: the
// members GIN's pending list flipped the mass probe to a sequential scan, and the
// tokens GIN carried most of the writer WAL cost of turning the option off (the
// W-A amendment of arbiter-b2b-w-a-return).
var holderIndexStatements = map[string]string{
	"339_supply_chain_impact_finding_supports_cve_idx.sql":      "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_cve_idx ON supply_chain_impact_finding_supports (cve_id, fact_id) WHERE cve_id <> '';",
	"340_supply_chain_impact_finding_supports_advisory_idx.sql": "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_advisory_idx ON supply_chain_impact_finding_supports (advisory_id, fact_id) WHERE advisory_id <> '';",
	"341_supply_chain_impact_finding_supports_members_idx.sql":  "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_members_idx ON supply_chain_impact_finding_supports USING GIN ((payload #> '{provenance,advisory_group_members}')) WITH (fastupdate = off);",
	"342_supply_chain_impact_finding_supports_tokens_idx.sql":   "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_tokens_idx ON supply_chain_impact_finding_supports USING GIN (eshu_sci_foreign_member_tokens_v4(fact_id, payload));",
}

// migrationStatement returns the code of a migration file as one normalized
// statement, comments dropped.
func migrationStatement(raw string) string {
	var code []string
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code = append(code, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(code, " ")), " ")
}

// holderIndexStatementViolation returns why stmt (a migration's normalized
// statement) is not the pinned statement of file, or "".
func holderIndexStatementViolation(file, stmt string) string {
	want, ok := holderIndexStatements[file]
	if !ok {
		return "no pinned statement for " + file
	}
	if stmt != want {
		return fmt.Sprintf("%s statement = %q, want %q", file, stmt, want)
	}
	return ""
}

// TestSupplyChainImpactHolderIndexMigrationsShape pins migrations 339 to 342
// (#11197): one concurrent build per file, the two btrees partial on a non-blank
// id, the members GIN with fastupdate = off and the tokens GIN with no
// fastupdate option. The check is exercised in both directions: a members GIN
// without the option and a tokens GIN with it are each reported.
func TestSupplyChainImpactHolderIndexMigrationsShape(t *testing.T) {
	t.Parallel()
	for file := range holderIndexStatements {
		raw, err := os.ReadFile("migrations/" + file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if v := holderIndexStatementViolation(file, migrationStatement(string(raw))); v != "" {
			t.Error(v)
		}
	}
	// Seeded violations: the pinned text with the option flipped is reported.
	members := "341_supply_chain_impact_finding_supports_members_idx.sql"
	if holderIndexStatementViolation(members, strings.Replace(holderIndexStatements[members], " WITH (fastupdate = off)", "", 1)) == "" {
		t.Error("a members GIN without fastupdate = off is accepted")
	}
	tokens := "342_supply_chain_impact_finding_supports_tokens_idx.sql"
	if holderIndexStatementViolation(tokens, strings.Replace(holderIndexStatements[tokens], ";", " WITH (fastupdate = off);", 1)) == "" {
		t.Error("a tokens GIN with fastupdate = off is accepted")
	}
}
