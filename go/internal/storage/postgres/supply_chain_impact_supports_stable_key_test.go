// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The supports table gains a nullable stable_fact_key (#10880 PR-C1 a): the key
// that links a finding across generations, copied from the fact so the stored
// read can leave fact_records. These tests pin that the upsert fills it and that
// it stays out of the write-on-change compare, so a replay writes no row version
// only to fill a key (arbiter-prc-design C1 a).

// upsertCompareTuples returns the two row tuples of the write-on-change guard:
// the stored columns and the incoming ones.
func upsertCompareTuples(t *testing.T) (existing, incoming string) {
	t.Helper()
	_, afterWhere, ok := strings.Cut(upsertSupplyChainImpactSupportsSQL, "WHERE (")
	if !ok {
		t.Fatal("the upsert has no write-on-change guard")
	}
	existing, rest, ok := strings.Cut(afterWhere, ")\n      IS DISTINCT FROM (")
	if !ok {
		t.Fatal("the guard has no IS DISTINCT FROM tuple")
	}
	incoming, _, ok = strings.Cut(rest, ")\nRETURNING")
	if !ok {
		t.Fatal("the guard's incoming tuple is not followed by RETURNING")
	}
	return existing, incoming
}

// TestSupportsUpsertFillsStableFactKeyOutsideTheCompare pins both halves of C1 a:
// the key is selected from the fact, inserted, and set on a row the upsert
// rewrites anyway, and it appears in neither side of the write-on-change guard.
func TestSupportsUpsertFillsStableFactKeyOutsideTheCompare(t *testing.T) {
	t.Parallel()

	sql := upsertSupplyChainImpactSupportsSQL
	for name, want := range map[string]string{
		"selected from the fact": "fact.stable_fact_key AS stable_fact_key",
		"set on a rewritten row": "stable_fact_key = EXCLUDED.stable_fact_key",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("the supports upsert is not %s: missing %q", name, want)
		}
	}
	insertColumns, _, _ := strings.Cut(sql, ")\n")
	if !strings.Contains(insertColumns, "stable_fact_key") {
		t.Errorf("the supports upsert does not insert stable_fact_key: column list %q", insertColumns)
	}
	existing, incoming := upsertCompareTuples(t)
	if strings.Contains(existing, "stable_fact_key") || strings.Contains(incoming, "stable_fact_key") {
		t.Fatalf("stable_fact_key is inside the write-on-change guard (existing %q, incoming %q): a replay would rewrite every row only to fill the key", existing, incoming)
	}
	// Non-vacuity: the guard still compares every derived column.
	for _, column := range supplyChainImpactSupportColumns {
		if !strings.Contains(existing, "supports."+column.name) || !strings.Contains(incoming, "EXCLUDED."+column.name) {
			t.Errorf("the guard no longer compares %s", column.name)
		}
	}
}

// TestSupportsBackfillWritesThroughTheKeyFillingUpsert proves the backfill, which
// lists fact ids and then writes through the maintainer's upsert, fills the key
// for the rows it writes: it calls UpsertImpactSupports, the method that runs the
// statement pinned above, and no statement of its own writes supports.
func TestSupportsBackfillWritesThroughTheKeyFillingUpsert(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("supply_chain_impact_supports_backfill.go")
	if err != nil {
		t.Fatalf("read the backfill source: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, "UpsertImpactSupports(") {
		t.Fatal("the backfill no longer writes through UpsertImpactSupports: backfilled supports would not carry the key")
	}
	if regexp.MustCompile(`INSERT INTO supply_chain_impact_finding_supports(\s|\()`).MatchString(source) {
		t.Fatal("the backfill carries its own supports INSERT: it would bypass the key-filling upsert")
	}
}

// TestSupportsStableFactKeyMigrationIsAdditiveAndNullable pins the migration
// shape: one nullable column, no default, added idempotently, so a binary of the
// previous release that names its columns reads and writes the table unchanged.
func TestSupportsStableFactKeyMigrationIsAdditiveAndNullable(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob(filepath.Join("migrations", "*_supply_chain_impact_finding_supports_stable_fact_key.sql"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("stable_fact_key migration files = %v (err %v), want exactly one", matches, err)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read %s: %v", matches[0], err)
	}
	var statements []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			statements = append(statements, line)
		}
	}
	body := strings.Join(statements, "\n")
	add := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+supply_chain_impact_finding_supports\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+stable_fact_key\s+TEXT\s*;`)
	if !add.MatchString(body) {
		t.Fatalf("migration body is not one idempotent nullable ADD COLUMN stable_fact_key TEXT:\n%s", body)
	}
	for _, forbidden := range []string{"NOT NULL", "DEFAULT", "UPDATE", "INDEX"} {
		if strings.Contains(strings.ToUpper(body), forbidden) {
			t.Errorf("migration body contains %q: the column must be nullable, unfilled and unindexed here", forbidden)
		}
	}
}

// supportsContractBaseStatementCount is how many statements the version-1 contract
// covered before #10880 PR-C2 h appended the writer's in-memory upsert.
const supportsContractBaseStatementCount = 6

// supportsContractVersion1BasePin is the version-1 contract hash before C1 a
// added the fill-only column (the pin at origin/main f1f45ddad3).
const supportsContractVersion1BasePin = "3f720ba20fc6f6ef850cd00b72d36992bd7357291b1f4c6b77cfa442865a6184"

// TestStableFactKeyDoesNotChangeTheVersion1Statements is the differential that
// lets the version-1 pin be re-pinned instead of bumped. A bump re-runs the
// supports backfill from the start on every install and answers 503 on every
// supports read until it finishes; the column added here changes no existing
// support row, so the pin moves only if the statements differ from the base ones
// by the fill-only column alone. Rendering the shipped statements with no
// fill-only column must hash to the base pin; the shipped statements must not.
func TestStableFactKeyDoesNotChangeTheVersion1Statements(t *testing.T) {
	t.Parallel()

	statements := supplyChainImpactSupportsContractStatements()
	baseUpsert := supplyChainImpactUpsertSupportsStatementGuarded(
		supplyChainImpactSupportsSelectSQLWith("fact.fact_id = ANY($1::text[])", nil), nil, nil)
	if statements[0] == baseUpsert {
		t.Fatal("the shipped upsert equals the no-fill render: the fill-only column is not in it")
	}
	// The statements the version-1 pin covered before PR-C2 h appended the in-memory upsert.
	base := append([]string{baseUpsert}, statements[1:supportsContractBaseStatementCount]...)
	if got := supplyChainImpactSupportsContractHashOf(base); got != supportsContractVersion1BasePin {
		t.Fatalf("the no-fill render hashes to %s, want the base version-1 pin %s: the statements differ from the base by more than the fill-only column", got, supportsContractVersion1BasePin)
	}
	if got := supplyChainImpactSupportsContractHashOf(statements); got == supportsContractVersion1BasePin {
		t.Fatal("the shipped statements still hash to the base pin: the pin was not re-derived")
	}
	if len(statements) <= supportsContractBaseStatementCount {
		t.Fatalf("the contract covers %d statements, want more than the %d the base pin hashed", len(statements), supportsContractBaseStatementCount)
	}
}

// supportsContractBeforeRowsUpsertPin is the version-1 pin after PR-C1 a and before
// PR-C2 h appended the writer's in-memory upsert.
const supportsContractBeforeRowsUpsertPin = "691ac3c4f55c30599005a87ae4db68165228f7f9a3da3ba9e49f87534dde8eca"

// TestRowsUpsertOnlyAppendsToTheVersion1Contract proves the PR-C2 h re-pin is an
// append: the statements the contract covered before still hash to the previous pin,
// so no existing statement changed and the version stays 1 (a bump would re-run the
// supports backfill and answer 503 on every supports read until it finished).
func TestRowsUpsertOnlyAppendsToTheVersion1Contract(t *testing.T) {
	t.Parallel()

	statements := supplyChainImpactSupportsContractStatements()
	if got := supplyChainImpactSupportsContractHashOf(statements[:supportsContractBaseStatementCount]); got != supportsContractBeforeRowsUpsertPin {
		t.Fatalf("the first %d statements hash to %s, want the previous pin %s: an existing statement changed", supportsContractBaseStatementCount, got, supportsContractBeforeRowsUpsertPin)
	}
	if statements[len(statements)-1] != upsertSupplyChainImpactSupportRowsSQL {
		t.Fatal("the appended statement is not the writer's in-memory upsert")
	}
}
