// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// sqlTxQueryer adapts *sql.Tx to the db.Queryer the production helper takes.
type sqlTxQueryer struct{ tx *sql.Tx }

func (q sqlTxQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.tx.QueryContext(ctx, query, args...)
}

// pendingListCorpus seeds scopes and a base set of supports directly (no finding
// facts), with the table's autovacuum off, and ANALYZEs once. It never vacuums:
// a vacuum flushes a GIN pending list, and the guard needs the list the planner
// prices.
func pendingListCorpus(t *testing.T, ctx context.Context, database *sql.DB, rows int) {
	t.Helper()
	for _, stmt := range []string{
		// pageinspect reads the GIN metapage's pending-page count.
		`CREATE EXTENSION IF NOT EXISTS pageinspect`,
		`ALTER TABLE supply_chain_impact_finding_supports SET (autovacuum_enabled = false)`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'plg-scope-' || i, 'synthetic', 'synthetic', 'r' || i, 'synthetic', 'r' || i, now(), now(), 'active', 'plg-gen-' || i, '{}'::jsonb FROM generate_series(0, 99) i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'plg-gen-' || i, 'plg-scope-' || i, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb FROM generate_series(0, 99) i`,
	} {
		if _, err := database.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed pending-list corpus: %v", err)
		}
	}
	if _, err := database.ExecContext(ctx, pendingListInsertSQL(0, rows-1)); err != nil {
		t.Fatalf("seed supports: %v", err)
	}
	if _, err := database.ExecContext(ctx, `ANALYZE supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// pendingListInsertSQL inserts supports numbered from..to. Each row carries a
// ~540-byte narrative (production-like width, about 7 rows per heap page), a hot
// CVE on 1 row in 50 and a hot foreign group member on 1 row in 10.
func pendingListInsertSQL(from, to int) string {
	return fmt.Sprintf(`
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id, priority_score,
  source_confidence, suppression_state, impact_status, ecosystem, repository_id, cve_id, advisory_id, package_id, payload)
SELECT 'reducer_supply_chain_impact_finding:' || md5('p' || i), md5('c' || i), 'plg-scope-' || (i %% 100), 'plg-gen-' || (i %% 100), 'finding:' || i, 1, (i * 7919) %% 100,
  'observed', 'active', 'affected_exact', 'npm', 'repository:r' || (i %% 2000),
  CASE WHEN i %% 50 = 0 THEN 'CVE-HOT' ELSE 'CVE-' || (i %% 9000) END,
  CASE WHEN i %% 5 = 0 THEN '' ELSE 'GHSA-' || (i %% 6000) END,
  'pkg:npm/p' || (i %% 500),
  jsonb_build_object('cve_id', CASE WHEN i %% 50 = 0 THEN 'CVE-HOT' ELSE 'CVE-' || (i %% 9000) END,
    'impact_status', 'affected_exact', 'ecosystem', 'npm',
    'provenance', jsonb_build_object('advisory_group_members',
      jsonb_build_array('adv-' || md5((i %% 700)::text || 'a'), 'adv-' || md5((i %% 700)::text || 'b'), 'adv-' || md5((i %% 700)::text || 'c'),
        CASE WHEN i %% 10 = 3 THEN 'adv-hot-foreign' ELSE 'adv-' || md5((i %% 700)::text || 'd') END)),
    'narrative', repeat(md5(i::text), 16))
FROM generate_series(%d::bigint, %d::bigint) i`, from, to)
}

func pendingPages(t *testing.T, ctx context.Context, tx *sql.Tx, index string) int {
	t.Helper()
	var pages int
	if err := tx.QueryRowContext(ctx, `SELECT n_pending_pages FROM gin_metapage_info(get_raw_page($1, 0))`, index).Scan(&pages); err != nil {
		t.Fatalf("pending pages of %s: %v", index, err)
	}
	return pages
}

// probeMasses runs the shipped mass probe in tx and returns its masses.
func probeMasses(t *testing.T, ctx context.Context, tx *sql.Tx, applySettings bool, ids []string) map[string]int64 {
	t.Helper()
	if applySettings {
		if err := postgres.ApplyHolderMassProbeSettings(ctx, sqlTxQueryer{tx}, 10*time.Second); err != nil {
			t.Fatalf("apply the mass probe settings: %v", err)
		}
	}
	rows, err := tx.QueryContext(ctx, postgres.HolderMassProbeQuery, array.StringArray(ids))
	if err != nil {
		t.Fatalf("run the mass probe: %v", err)
	}
	defer func() { _ = rows.Close() }()
	masses := map[string]int64{}
	for rows.Next() {
		var id string
		var mass int64
		if err := rows.Scan(&id, &mass); err != nil {
			t.Fatal(err)
		}
		masses[id] = mass
	}
	return masses
}

// explainInTx plans one holder statement in tx; settings, when set, run first.
func explainInTx(t *testing.T, ctx context.Context, tx *sql.Tx, st holderPlanStatement, applyMassSettings bool) string {
	t.Helper()
	if applyMassSettings {
		if err := postgres.ApplyHolderMassProbeSettings(ctx, sqlTxQueryer{tx}, 10*time.Second); err != nil {
			t.Fatalf("apply the mass probe settings: %v", err)
		}
	}
	return explainHolderStatement(t, ctx, tx, st, false)
}

// TestSupplyChainImpactSupportsMassProbeSurvivesPendingListLive is the plan guard
// for the two decisions of arbiter-b2b-gin-covering (#11197): the group-member and
// token GINs ship with fastupdate = off (no pending list for the planner to price),
// and the mass probe's transaction turns enable_seqscan off (the probe's per-id
// LIMIT otherwise lets a sequential scan win the cost race against any GIN scan
// that is not nearly free). On about 40,000 supports with autovacuum off and no
// vacuum after the seed:
//
//	RED-1   a rolled-back transaction resets fastupdate on both GINs and inserts
//	        until each holds 400 pending pages; the shipped probe text, planned
//	        without the setting, must be a Seq Scan on supports (else the guard
//	        proves nothing: "RED not reproduced").
//	GREEN-1 the same inserts under the shipped options leave 0 pending pages on
//	        both GINs, and the probe's member arm is on the members index with no
//	        setting.
//	GREEN-2 the RED-1 state planned after ApplyHolderMassProbeSettings, the helper
//	        the reader runs: the member arm is on the members index, no Seq Scan.
//
// In the RED-1 state the 5-id holder statement and both key-range statements keep
// every arm on its index by name, with no setting.
func TestSupplyChainImpactSupportsMassProbeSurvivesPendingListLive(t *testing.T) {
	ctx, database := fanOutLiveDB(t)
	pendingListCorpus(t, ctx, database, 40000)
	statements := holderPlanStatements()
	var probe holderPlanStatement
	for _, st := range statements {
		if st.query == postgres.HolderMassProbeQuery {
			probe = st
		}
	}

	// Schema: both GINs carry fastupdate = off on the migrated database.
	for _, index := range []string{holderMembersIdx, holderTokensIdx} {
		var opts []byte
		if err := database.QueryRowContext(ctx, `SELECT coalesce(reloptions::text, '') FROM pg_class WHERE relname = $1`, index).Scan(&opts); err != nil {
			t.Fatalf("reloptions of %s: %v", index, err)
		}
		if !strings.Contains(string(opts), "fastupdate=off") {
			t.Fatalf("%s reloptions = %q, want fastupdate=off", index, opts)
		}
	}

	insertUntil := func(tx *sql.Tx, wantPending int) int {
		next := 40000
		for next < 140000 {
			if _, err := tx.ExecContext(ctx, pendingListInsertSQL(next, next+499)); err != nil {
				t.Fatalf("insert: %v", err)
			}
			next += 500
			if wantPending > 0 && pendingPages(t, ctx, tx, holderMembersIdx) >= wantPending && pendingPages(t, ctx, tx, holderTokensIdx) >= wantPending {
				return next - 40000
			}
			if wantPending == 0 && next-40000 >= 20000 {
				return next - 40000
			}
		}
		return next - 40000
	}

	// GREEN-1 runs first: rows inserted by a rolled-back transaction leave their
	// pending-list entries behind, so a RED-1 before it would poison the count.
	greenTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	insertUntil(greenTx, 0)
	for _, index := range []string{holderMembersIdx, holderTokensIdx} {
		if pages := pendingPages(t, ctx, greenTx, index); pages != 0 {
			t.Fatalf("GREEN-1: %s holds %d pending pages under fastupdate = off, want 0", index, pages)
		}
	}
	plan := explainInTx(t, ctx, greenTx, probe, false)
	if v := holderPlanViolation(plan, holderMembersIdx); v != "" {
		t.Fatalf("GREEN-1: the mass probe without the setting: %s\n%s", v, plan)
	}
	if err := greenTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// RED-1 and GREEN-2.
	redTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{holderMembersIdx, holderTokensIdx} {
		if _, err := redTx.ExecContext(ctx, `ALTER INDEX `+index+` RESET (fastupdate)`); err != nil {
			_ = redTx.Rollback()
			t.Fatalf("reset fastupdate on %s: %v", index, err)
		}
	}
	added := insertUntil(redTx, 400)
	members, tokens := pendingPages(t, ctx, redTx, holderMembersIdx), pendingPages(t, ctx, redTx, holderTokensIdx)
	if members < 400 || tokens < 400 {
		_ = redTx.Rollback()
		t.Fatalf("RED-1 fill: pending pages members=%d tokens=%d after %d rows, want >= 400", members, tokens, added)
	}
	t.Logf("RED-1: %d rows added, pending pages members=%d tokens=%d", added, members, tokens)
	redPlan := explainInTx(t, ctx, redTx, probe, false)
	if !strings.Contains(redPlan, "Seq Scan on "+holderSupportsTbl) {
		_ = redTx.Rollback()
		t.Fatalf("RED not reproduced: the shipped probe is not a Seq Scan on supports with %d/%d pending pages:\n%s", members, tokens, redPlan)
	}
	greenPlan := explainInTx(t, ctx, redTx, probe, true)
	if v := holderPlanViolation(greenPlan, holderMembersIdx); v != "" {
		_ = redTx.Rollback()
		t.Fatalf("GREEN-2: the mass probe under the reader's settings: %s\n%s", v, greenPlan)
	}
	// The other statements in the RED state, no setting.
	for _, st := range statements {
		if st.query == postgres.HolderMassProbeQuery {
			continue
		}
		plan := explainHolderStatement(t, ctx, redTx, st, false)
		if v := holderPlanViolation(plan, st.indexes...); v != "" {
			_ = redTx.Rollback()
			t.Fatalf("RED state: %s: %s\n%s", st.name, v, plan)
		}
	}
	// GREEN-2 also RUNS the probe: under the settings its masses equal the masses of
	// the same probe after the pending lists are flushed and no setting applies.
	probeIDs := []string{"CVE-HOT", "CVE-5", "GHSA-HOT", "adv-hot-foreign", "CVE-2026-none"}
	withSetting := probeMasses(t, ctx, redTx, true, probeIDs)
	for _, index := range []string{holderMembersIdx, holderTokensIdx} {
		if _, err := redTx.ExecContext(ctx, `SELECT gin_clean_pending_list($1::regclass)`, index); err != nil {
			_ = redTx.Rollback()
			t.Fatalf("flush %s: %v", index, err)
		}
	}
	if _, err := redTx.ExecContext(ctx, `RESET enable_seqscan`); err != nil {
		_ = redTx.Rollback()
		t.Fatal(err)
	}
	flushed := probeMasses(t, ctx, redTx, false, probeIDs)
	for _, id := range probeIDs {
		if withSetting[id] != flushed[id] {
			_ = redTx.Rollback()
			t.Fatalf("mass of %q under the settings = %d, flushed run = %d", id, withSetting[id], flushed[id])
		}
	}
	if withSetting["adv-hot-foreign"] == 0 {
		_ = redTx.Rollback()
		t.Fatalf("masses %v: the foreign group member must have mass, or the probe proves nothing", withSetting)
	}
	if err := redTx.Rollback(); err != nil {
		t.Fatal(err)
	}
}
