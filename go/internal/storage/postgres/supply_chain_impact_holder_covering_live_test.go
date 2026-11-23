// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	coverFactPrefix = "reducer_supply_chain_impact_finding:"
	coverHotCVE     = "CVE-HOT"
	coverHotAdv     = "GHSA-HOT"
)

// coveringInsertSQL inserts supports numbered from..to with a hot CVE and a hot
// advisory id on 1 row in 5 each, so each key-range statement reads hundreds of
// rows per arm at test scale.
func coveringInsertSQL(from, to int) string {
	return fmt.Sprintf(`
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score,
  source_confidence, suppression_state, impact_status, ecosystem, repository_id, cve_id, advisory_id, package_id, payload)
SELECT 'reducer_supply_chain_impact_finding:' || md5('v' || i), md5('c' || i), 'plg-scope-' || (i %% 100), 'plg-gen-' || (i %% 100), 'finding:' || i, 1, (i * 7919) %% 100,
  'observed', 'active', 'affected_exact', 'npm', 'repository:r' || (i %% 2000),
  CASE WHEN i %% 5 = 0 THEN 'CVE-HOT' ELSE 'CVE-' || (i %% 9000) END,
  CASE WHEN i %% 5 = 1 THEN 'GHSA-HOT' WHEN i %% 5 = 2 THEN '' ELSE 'GHSA-' || (i %% 6000) END,
  'pkg:npm/p' || (i %% 500),
  jsonb_build_object('cve_id', 'x', 'narrative', repeat(md5(i::text), 16))
FROM generate_series(%d::bigint, %d::bigint) i`, from, to)
}

var coverHeapFetches = regexp.MustCompile(`Heap Fetches: (\d+)`)

// coverStatements are the two key-range texts (bounded mid range and open last
// range) for both hot ids, derived from the shipped constants.
func coverStatements() []holderPlanStatement {
	ids := quotedArray(coverHotCVE, coverHotAdv)
	tokens := quotedArray(postgres.HolderMemberTokens([]string{coverHotCVE}, 0, 16)...)
	idx := []string{holderCveIdx, holderAdvisoryIdx, holderTokensIdx}
	return []holderPlanStatement{
		{
			"bounded key-range, hot cve and advisory", postgres.FindSupplyChainImpactHolderKeyRangeQuery, "force_custom_plan",
			[]string{"text[]", "text", "text", "text", "text[]"},
			[]string{ids, "'other-scope'", "'" + coverFactPrefix + "4'", "'" + coverFactPrefix + "8'", tokens},
			idx,
		},
		{
			"open key-range, hot cve and advisory", postgres.FindSupplyChainImpactHolderOpenKeyRangeQuery, "force_custom_plan",
			[]string{"text[]", "text", "text", "text[]"},
			[]string{ids, "'other-scope'", "'" + coverFactPrefix + "f'", tokens},
			idx,
		},
	}
}

// coverIndexOnly returns why plan does not read the cve and advisory arms through
// Index Only Scans on the named indexes, or "".
func coverIndexOnly(plan string) string {
	for _, index := range []string{holderCveIdx, holderAdvisoryIdx} {
		if !strings.Contains(plan, "Index Only Scan using "+index) {
			return "no Index Only Scan using " + index
		}
	}
	return ""
}

func coverHeapFetchTotal(plan string) int {
	total := 0
	for _, m := range coverHeapFetches.FindAllStringSubmatch(plan, -1) {
		n, _ := strconv.Atoi(m[1])
		total += n
	}
	return total
}

func coverExplain(t *testing.T, ctx context.Context, database *sql.DB, st holderPlanStatement) string {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	return explainHolderStatement(t, ctx, conn, st, true)
}

// TestSupplyChainImpactSupportsHolderCoveringIndexLive is the guard for the
// covering shape of migrations 339 and 340 (#11197, arbiter-b2b-covering-hot
// decisions 9 and 12): both btrees carry INCLUDE (scope_id, generation_id), so the
// key-range statements, which read only those two columns, can run index-only. On
// the migration-built indexes:
//
//   - pg_get_indexdef shows the INCLUDE on both;
//   - without a VACUUM (every row on a not-all-visible page) each cve and
//     advisory arm is still on its named index with both fact_id bounds in the
//     Index Cond (lower only on the open range);
//   - after VACUUM both arms are Index Only Scans on the shipped names with
//     Heap Fetches 0;
//   - the seeded violation, the same index built without INCLUDE in a rolled-back
//     transaction, is rejected by the same check.
//
// Covering is a capacity multiplier of about 1/f, f being the share of the id's rows
// on not-all-visible pages; it does not guarantee Heap Fetches 0 in production.
func TestSupplyChainImpactSupportsHolderCoveringIndexLive(t *testing.T) {
	ctx, database := fanOutLiveDB(t)
	pendingListCorpus(t, ctx, database, 1)
	if _, err := database.ExecContext(ctx, coveringInsertSQL(0, 39999)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := database.ExecContext(ctx, `ANALYZE supply_chain_impact_finding_supports`); err != nil {
		t.Fatal(err)
	}

	for _, index := range []string{holderCveIdx, holderAdvisoryIdx} {
		var def string
		if err := database.QueryRowContext(ctx, `SELECT pg_get_indexdef(c.oid) FROM pg_class c WHERE c.relname = $1`, index).Scan(&def); err != nil {
			t.Fatalf("indexdef %s: %v", index, err)
		}
		if !strings.Contains(def, "INCLUDE (scope_id, generation_id)") {
			t.Fatalf("%s = %q, want INCLUDE (scope_id, generation_id)", index, def)
		}
	}

	check := func(label string, vacuumed bool) {
		t.Helper()
		for _, st := range coverStatements() {
			plan := coverExplain(t, ctx, database, st)
			if v := holderPlanViolation(plan, st.indexes...); v != "" {
				t.Fatalf("%s: %s: %s\n%s", label, st.name, v, plan)
			}
			if v := holderBoundViolation(plan); v != "" {
				t.Fatalf("%s: %s: %s\n%s", label, st.name, v, plan)
			}
			if vacuumed {
				if v := coverIndexOnly(plan); v != "" {
					t.Fatalf("%s: %s: %s\n%s", label, st.name, v, plan)
				}
				if n := coverHeapFetchTotal(plan); n != 0 {
					t.Fatalf("%s: %s: %d heap fetches after VACUUM, want 0\n%s", label, st.name, n, plan)
				}
			}
		}
	}
	check("no VACUUM", false)
	if _, err := database.ExecContext(ctx, `VACUUM (FREEZE) supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	check("after VACUUM", true)

	// Seeded violation: the same indexes without INCLUDE read the heap.
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DROP INDEX ` + holderCveIdx,
		`DROP INDEX ` + holderAdvisoryIdx,
		`CREATE INDEX ` + holderCveIdx + ` ON supply_chain_impact_finding_supports (cve_id, fact_id) WHERE cve_id <> ''`,
		`CREATE INDEX ` + holderAdvisoryIdx + ` ON supply_chain_impact_finding_supports (advisory_id, fact_id) WHERE advisory_id <> ''`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			t.Fatalf("seeded violation setup: %v", err)
		}
	}
	rejected := 0
	for _, st := range coverStatements() {
		if coverIndexOnly(explainHolderStatement(t, ctx, tx, st, false)) != "" {
			rejected++
		}
	}
	if rejected != len(coverStatements()) {
		t.Fatalf("the check accepted %d of %d statements on indexes without INCLUDE", len(coverStatements())-rejected, len(coverStatements()))
	}
}
