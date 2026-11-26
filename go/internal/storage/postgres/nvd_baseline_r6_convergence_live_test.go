// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10728 R6-style convergence proof on a disposable PostgreSQL 18 database through the real commit, reducer, and keyed-resweep path: two baseline passes over one year-chunk input converge to one fact row set with zero new captures on the resweep, and the folded facts-per-record and dirty-log rows per committed fact are measured on that path
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// r6BaselineScope stands in for one NVD baseline year chunk (2025): the
	// pass under test is the commit of that chunk's folded facts plus the
	// reducer pass and the keyed resweep, not the planner mint that chose
	// the chunk (the lanes tests pin the mint).
	r6BaselineScope = "scope:r6-baseline-2025"
	// r6BaselineRepo is the consuming repository the correlation facts point
	// at. It is never seeded as a scope: the finding still derives, the way
	// the fan-out proofs derive holder copies without seeding every holder.
	r6BaselineRepo = "repository:r6-baseline"
	// r6BaselineCVECount is the year-chunk input: five NVD-identity records.
	// Five is the smallest slice that still proves set convergence (more
	// than one row could hide a systematic per-row duplicate).
	r6BaselineCVECount = 5
)

var r6BaselineNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// r6BaselineCVE names the ith record of the year-chunk input.
func r6BaselineCVE(i int) string {
	return map[int]string{
		0: "CVE-2025-9001",
		1: "CVE-2025-9002",
		2: "CVE-2025-9003",
		3: "CVE-2025-9004",
		4: "CVE-2025-9005",
	}[i]
}

// r6BaselineFacts folds one year-chunk record the way the satellite does
// under the M5 rule (design 10728-baseline-pacing section 6, evidence 10729
// section 1): one vulnerability.cve fact, one
// vulnerability.affected_package fact, one package-consumption correlation,
// and one vulnerability.affected_product (CPE) fact. The committed row set
// therefore holds exactly four facts per record; the test pins that shape.
func r6BaselineFacts(scope, generation, cve, pkg string) []facts.Envelope {
	observed := r6BaselineNow.Add(-time.Hour)
	envelope := func(id, kind string, payload map[string]any) facts.Envelope {
		return facts.Envelope{
			FactID: id, ScopeID: scope, GenerationID: generation, FactKind: kind, StableFactKey: id,
			SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1, CollectorKind: "synthetic",
			SourceConfidence: facts.SourceConfidenceReported, ObservedAt: observed, Payload: payload,
			SourceRef: facts.Ref{SourceSystem: "synthetic", ScopeID: scope, GenerationID: generation, FactKey: id},
		}
	}
	return []facts.Envelope{
		envelope("fact:r6:"+cve+":cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": cve, "cve_id": cve, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
		}),
		envelope("fact:r6:"+cve+":affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": cve, "cve_id": cve, "package_id": pkg, "ecosystem": "npm",
			"package_name": pkg, "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}),
		envelope("fact:r6:"+cve+":consumption", factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": pkg, "relationship_kind": "consumption", "repository_id": r6BaselineRepo,
			"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:r6:" + cve + ":manifest"},
		}),
		envelope("fact:r6:"+cve+":product", chain.VulnerabilityAffectedProductFactKind, map[string]any{
			"advisory_id": cve, "cve_id": cve, "source": "nvd",
			"criteria": "cpe:2.3:a:example:r6:" + pkg + ":*:*:*:*:*:*:*",
		}),
	}
}

// r6BaselineRow is one committed fact row as the convergence comparison sees
// it: kind, stable key, and payload. The generation is fixed for both
// passes, so it is not part of the comparison.
type r6BaselineRow struct {
	Kind    string
	Key     string
	Payload string
}

// r6Snapshot reads the whole committed row set of the chunk: every
// non-tombstone fact row of the generation plus one row per derived finding
// from the supports table (since PR-C2 the writer stores findings as supports
// only), in kind/key order.
func r6Snapshot(t *testing.T, ctx context.Context, db *sql.DB, generation string) []r6BaselineRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT fact_kind, stable_fact_key, payload::text FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND NOT is_tombstone
UNION ALL
SELECT '`+facts.ReducerSupplyChainImpactFindingFactKind+` AS fact_kind, fact_id AS stable_fact_key, payload::text
FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND generation_id = $2
ORDER BY 1, 2`, r6BaselineScope, generation)
	if err != nil {
		t.Fatalf("read chunk rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []r6BaselineRow
	for rows.Next() {
		var row r6BaselineRow
		if err := rows.Scan(&row.Kind, &row.Key, &row.Payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func r6Count(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// r6PendingKindSource counts the backlog rows of one capture class across
// the versioned log and the staging queue: a tick collapses staging before
// it admits, so both tables are pending work the next tick consumes.
func r6PendingKindSource(t *testing.T, ctx context.Context, db *sql.DB, kind, source string) int {
	t.Helper()
	return r6Count(t, ctx, db, `SELECT count(*) FROM (
  SELECT kind, source FROM supply_chain_impact_winners_dirty_log
  UNION ALL SELECT kind, source FROM supply_chain_impact_winners_dirty_staging
) AS pending WHERE kind = $1 AND source = $2`, kind, source)
}

// r6Drain plays the reducer worker until the queue has nothing claimable and
// returns the number of passes handled. A handler error is recorded as a
// failed attempt, like the worker does.
func r6Drain(t *testing.T, ctx context.Context, db *sql.DB, queue postgres.ReducerQueue, handler supplychaincore.SupplyChainImpactHandler) int {
	t.Helper()
	handled := 0
	for i := 0; i < 60; i++ {
		intent, ok, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if !ok {
			return handled
		}
		handled++
		result, err := handler.Handle(ctx, intent)
		if err != nil {
			if failErr := queue.Fail(ctx, intent, err); failErr != nil {
				t.Fatalf("fail %s: %v (handle error: %v)", intent.IntentID, failErr, err)
			}
			if _, execErr := db.ExecContext(ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, intent.IntentID); execErr != nil {
				t.Fatalf("make retry visible: %v", execErr)
			}
			continue
		}
		if err := queue.Ack(ctx, intent, result); err != nil {
			t.Fatalf("ack %s: %v", intent.IntentID, err)
		}
	}
	t.Fatalf("queue still had work after 60 claims")
	return -1
}

// TestNVDBaselineTwoPassesConvergeToOneRowSetLive is the committed R6-style
// convergence test (eshu-hq/eshu#10728 AC4 gap, pacing-doc item 5, ruling
// Required proof): two baseline passes over the same year-chunk input
// converge to one row set on a disposable database through the real commit
// path (FactStore.UpsertFacts), the real reducer
// (SupplyChainImpactHandler.Handle), and the production keyed resweep
// (ResweepWinnersKeyed). It also measures the folded facts-per-record and
// the dirty-log rows per committed fact on that path (evidence 10729
// sections 2-3, now pinned instead of throwaway).
//
// Pass 1 commits the folded chunk (4 facts per record) and drains it: the
// row set holds exactly the planted facts, one finding per record derives,
// and the dirty log holds the writer captures. Pass 2 re-commits the
// identical fact set under a new intent (the same-digest rerun of the #9805
// capture rule) and drains it: the row set is identical (no duplicate keys,
// no payload drift), the findings are unchanged, and the dirty log is
// untouched (zero new captures). The keyed tick then closes the full path:
// winners equal the record count and the dirty log drains to empty, and a
// second tick admits nothing.
//
// This is a pinning test: idempotency already holds (the throwaway 10729
// proof measured dirty 22 to 22 and findings 20 to 20). It would fail if the
// upsert duplicated instead of replacing on a same-key re-commit, if the
// writer captured on a same-digest rewrite, if the resweep inserted phantom
// winners or left rows queued, or if the folded shape stopped being four
// facts per single-CPE record.
func TestNVDBaselineTwoPassesConvergeToOneRowSetLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#10728 R6 baseline convergence proof")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	db.SetMaxOpenConns(24)
	t.Cleanup(func() { _ = db.Close() })

	// A template clone is not guaranteed to open with an empty dirty log
	// (migration seeds fire capture triggers), so drain to a known state
	// first; every row counted below is then this proof's own.
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
		t.Fatalf("clear dirty log: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_winners_dirty_staging`); err != nil {
		t.Fatalf("clear dirty staging: %v", err)
	}

	const generationShort = "g1"
	generation := fanOutGen(r6BaselineScope, generationShort)
	fanOutSeedScope(t, ctx, db, r6BaselineScope, generationShort)
	fanOutSeedItem(t, ctx, db, r6BaselineScope, generationShort)

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	handler := supplychaincore.SupplyChainImpactHandler{
		FactLoader:         store,
		FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
		Writer: supplychaincore.PostgresSupplyChainImpactWriter{
			DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
			Now: func() time.Time { return r6BaselineNow },
		},
		FanOut: fanOutLiveStore(db),
		Now:    func() time.Time { return r6BaselineNow },
	}
	queue := fanOutLiveQueue(db)

	chunk := make([]facts.Envelope, 0, r6BaselineCVECount*4)
	for i := range r6BaselineCVECount {
		chunk = append(chunk, r6BaselineFacts(r6BaselineScope, generation, r6BaselineCVE(i), "pkg:npm/r6-2025-"+string(rune('a'+i))+"@1.0.0")...)
	}

	// Pass 1: commit the folded chunk and drain it.
	if err := store.UpsertFacts(ctx, chunk); err != nil {
		t.Fatalf("pass 1 upsert: %v", err)
	}
	if got := r6Drain(t, ctx, db, queue, handler); got != 1 {
		t.Fatalf("pass 1 handled %d passes, want exactly the chunk's own", got)
	}
	first := r6Snapshot(t, ctx, db, generation)
	// The snapshot holds the planted chunk plus the findings the drain
	// derived into the same generation: 4 folded facts plus 1 finding per
	// record.
	if len(first) != r6BaselineCVECount*5 {
		t.Fatalf("pass 1 row set = %d rows, want %d (4 folded facts plus 1 finding per record)", len(first), r6BaselineCVECount*5)
	}
	kinds := map[string]int{}
	for _, row := range first {
		kinds[row.Kind]++
	}
	for _, kind := range []string{
		chain.VulnerabilityCVEFactKind,
		chain.VulnerabilityAffectedPackageFactKind,
		factschema.FactKindReducerPackageConsumptionCorrelation,
		chain.VulnerabilityAffectedProductFactKind,
		facts.ReducerSupplyChainImpactFindingFactKind,
	} {
		if kinds[kind] != r6BaselineCVECount {
			t.Fatalf("pass 1 kind %s = %d rows, want %d (one per record)", kind, kinds[kind], r6BaselineCVECount)
		}
	}
	// The writer stores derived findings as supports only, so the finding
	// count reads the supports table, not the frozen fact rows.
	findings := func() int {
		return r6Count(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND generation_id = $2`, r6BaselineScope, generation)
	}
	if got := findings(); got != r6BaselineCVECount {
		t.Fatalf("pass 1 findings = %d, want %d (one per record)", got, r6BaselineCVECount)
	}
	dirtyLog := func() int {
		return r6Count(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_winners_dirty_log`)
	}
	staging := func() int {
		return r6Count(t, ctx, db, `SELECT count(*) FROM supply_chain_impact_winners_dirty_staging`)
	}
	dirtyAfterPass1, stagingAfterPass1 := dirtyLog(), staging()
	if dirtyAfterPass1 == 0 && stagingAfterPass1 == 0 {
		t.Fatal("pass 1 captured no dirty rows: the writer capture the ratio divides by never fired")
	}
	t.Logf("pass 1: %d planted facts over %d records = %.2f facts/record; dirty log %d + staging %d rows over %d committed facts",
		len(chunk), r6BaselineCVECount, float64(len(chunk))/r6BaselineCVECount, dirtyAfterPass1, stagingAfterPass1, len(chunk))
	// The 6 pending rows are 5 fact_upsert captures (one per derived
	// finding: 1.0 per record, the 10729 ratio) plus the one-time
	// scope-seeding activation pointer move, all in staging (the capture
	// lane stages before the tick admits). Pin the composition, not just
	// the total, so a new capture source cannot hide inside the count.
	if got := r6PendingKindSource(t, ctx, db, "fact", "fact_upsert"); got != r6BaselineCVECount {
		t.Fatalf("pass 1 fact_upsert captures = %d, want %d (one per derived finding)", got, r6BaselineCVECount)
	}
	if got := r6PendingKindSource(t, ctx, db, "scope", "activation"); got != 1 {
		t.Fatalf("pass 1 activation captures = %d, want 1 (the one-time scope-seeding pointer move)", got)
	}
	if got := dirtyAfterPass1 + stagingAfterPass1; got != r6BaselineCVECount+1 {
		t.Fatalf("pass 1 pending total = %d, want %d (5 finding captures plus the seeding move)", got, r6BaselineCVECount+1)
	}

	// Pass 2: the identical fact set under a new intent (the queue mints a
	// new intent id on the re-claim). Same keys, same payloads: the #9805
	// same-digest rerun, which must capture nothing.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items SET status='pending', attempt_count=0, lease_owner=NULL, claim_until=NULL,
  visible_at=now(), next_attempt_at=NULL, payload = payload - 'fanout_origin'
WHERE scope_id=$1 AND domain='supply_chain_impact' AND status='succeeded'`, r6BaselineScope); err != nil {
		t.Fatalf("requeue pass 2: %v", err)
	}
	if err := store.UpsertFacts(ctx, chunk); err != nil {
		t.Fatalf("pass 2 upsert: %v", err)
	}
	if got := r6Drain(t, ctx, db, queue, handler); got != 1 {
		t.Fatalf("pass 2 handled %d passes, want exactly the chunk's own re-run", got)
	}
	second := r6Snapshot(t, ctx, db, generation)
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("pass 2 row set differs from pass 1: %d rows vs %d (want one identical row set)", len(second), len(first))
	}
	if got := findings(); got != r6BaselineCVECount {
		t.Fatalf("pass 2 findings = %d, want the same %d as pass 1", got, r6BaselineCVECount)
	}
	if got, want := dirtyLog(), dirtyAfterPass1; got != want {
		t.Fatalf("dirty log after pass 2 = %d, want the pass 1 depth %d (zero new captures on the rerun)", got, want)
	}
	if got, want := staging(), stagingAfterPass1; got != want {
		t.Fatalf("dirty staging after pass 2 = %d, want the pass 1 depth %d", got, want)
	}

	// The keyed tick closes the full path: one canonical key per record, the
	// dirty log drained, and a second tick that admits nothing.
	winners := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	winners.MaxKeyFraction = 1000
	tick1, err := winners.ResweepWinnersKeyed(ctx)
	if err != nil {
		t.Fatalf("keyed resweep: %v", err)
	}
	t.Logf("tick 1: %+v", tick1)
	if tick1.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		t.Fatalf("tick 1 path = %q, want %q", tick1.Path, telemetry.SupplyChainImpactWinnersPathKeyed)
	}
	if tick1.Skipped || tick1.Bounded || tick1.Remaining != 0 {
		t.Fatalf("tick 1 = %+v, want an unbounded tick that drains everything", tick1)
	}
	// 6 marks consumed (5 finding captures plus the seeding move) into one
	// canonical key per record, all inserted, none updated or deleted.
	if tick1.Identifiers != r6BaselineCVECount+1 || tick1.Keys != r6BaselineCVECount {
		t.Fatalf("tick 1 marks/keys = (%d, %d), want (%d, %d)", tick1.Identifiers, tick1.Keys, r6BaselineCVECount+1, r6BaselineCVECount)
	}
	if tick1.Inserted != r6BaselineCVECount || tick1.Updated != 0 || tick1.Deleted != 0 || tick1.Unchanged != 0 {
		t.Fatalf("tick 1 inserted/updated/deleted/unchanged = (%d, %d, %d, %d), want (%d, 0, 0, 0)",
			tick1.Inserted, tick1.Updated, tick1.Deleted, tick1.Unchanged, r6BaselineCVECount)
	}
	if tick1.TriggerCaptured != (supplychaincore.WinnersTriggerCaptured{Activation: 1}) {
		t.Fatalf("tick 1 trigger-captured = %+v, want the seeding activation move only", tick1.TriggerCaptured)
	}
	if got := dirtyLog(); got != 0 {
		t.Fatalf("dirty log after the keyed tick = %d, want empty", got)
	}
	if got := staging(); got != 0 {
		t.Fatalf("dirty staging after the keyed tick = %d, want empty", got)
	}
	tick2, err := winners.ResweepWinnersKeyed(ctx)
	if err != nil {
		t.Fatalf("second keyed resweep: %v", err)
	}
	t.Logf("tick 2: %+v", tick2)
	// A drained tick stamps the horizon and admits nothing: the no-op
	// encoding on the keyed path is zeros with Skipped false, not a skip.
	if tick2.Path != telemetry.SupplyChainImpactWinnersPathKeyed || tick2.Skipped || tick2.Bounded {
		t.Fatalf("second tick = %+v, want an unskipped, unbounded keyed tick", tick2)
	}
	if tick2.Inserted != 0 || tick2.Updated != 0 || tick2.Deleted != 0 || tick2.Unchanged != 0 ||
		tick2.Identifiers != 0 || tick2.Keys != 0 || tick2.Remaining != 0 ||
		tick2.TriggerCaptured != (supplychaincore.WinnersTriggerCaptured{}) {
		t.Fatalf("second tick = %+v, want zero admissions: the first tick converged every key", tick2)
	}
	if !tick2.PreviousWatermark.Equal(tick1.Watermark) {
		t.Fatalf("second tick previous watermark = %v, want the first tick's stamp %v", tick2.PreviousWatermark, tick1.Watermark)
	}
	if tick2.Watermark.IsZero() {
		t.Fatal("second tick stamped no watermark: a drained tick still advances the horizon")
	}
}
