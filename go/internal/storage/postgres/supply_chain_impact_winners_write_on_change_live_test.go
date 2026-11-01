// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	winnersLiveOperatorScope = "operator:vulnerability_suppressions"
	winnersLiveSourceScope   = "vuln-intel://9774/source"
)

// winnersLiveFinding is the payload of one impact finding fact. Two facts with
// the same cve, package, repository and impact status share a canonical key.
func winnersLiveFinding(cve, pkg string, priority int, extra map[string]any) map[string]any {
	payload := map[string]any{
		"finding_id": "finding:" + cve + ":" + pkg, "cve_id": cve, "package_id": pkg,
		"impact_status": "affected_exact", "repository_id": "repo-9774", "ecosystem": "npm",
		"priority_score": priority, "observed_version": "1.0.0",
		"service_ids": []string{"svc-a"}, "workload_ids": []string{"wl-a"}, "environments": []string{"prod"},
	}
	for k, v := range extra {
		payload[k] = v
	}
	return payload
}

func winnersLiveSeedScope(t *testing.T, ctx context.Context, db *sql.DB, scope string) string {
	t.Helper()
	gen := scope + "/active"
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload, active_generation_id)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb, NULL)`, scope, now); err != nil {
		t.Fatalf("seed scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, gen, scope, now); err != nil {
		t.Fatalf("seed generation %s: %v", gen, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen); err != nil {
		t.Fatalf("activate %s: %v", gen, err)
	}
	return gen
}

func winnersLiveSeedFact(t *testing.T, ctx context.Context, db *sql.DB, id, scope string, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false, $5::jsonb)`,
		id, scope, scope+"/active", facts.ReducerSupplyChainImpactFindingFactKind, string(body)); err != nil {
		t.Fatalf("seed fact %s: %v", id, err)
	}
}

func winnersLiveExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// winnersLiveRow is one stored winner: its row-version identity (xmin), the
// time it was last written, and its content with materialized_at removed.
type winnersLiveRow struct {
	xmin, content string
	writtenAt     time.Time
}

func winnersLiveRead(t *testing.T, ctx context.Context, db *sql.DB) map[string]winnersLiveRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT canonical_key, xmin::text, materialized_at, (to_jsonb(w) - 'materialized_at')::text
FROM supply_chain_impact_canonical_winners AS w`)
	if err != nil {
		t.Fatalf("read winners: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]winnersLiveRow{}
	for rows.Next() {
		var key string
		var row winnersLiveRow
		if err := rows.Scan(&key, &row.xmin, &row.writtenAt, &row.content); err != nil {
			t.Fatalf("scan winner: %v", err)
		}
		out[key] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read winners: %v", err)
	}
	return out
}

func winnersLiveWatermark(t *testing.T, ctx context.Context, db *sql.DB) time.Time {
	t.Helper()
	var at time.Time
	if err := db.QueryRowContext(ctx, `SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1`).Scan(&at); err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	return at.UTC()
}

// TestSupplyChainImpactWinnersResweepWritesOnlyChangedRowsLive proves the #9774
// write-on-change resweep on PostgreSQL: a tick over unchanged facts writes no
// winner row version and still advances the watermark, a mixed tick writes
// exactly the inserted, updated and deleted rows, the counts say so, and the
// resulting table equals a fresh recompute.
func TestSupplyChainImpactWinnersResweepWritesOnlyChangedRowsLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners write-on-change proof")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	winnersLiveSeedScope(t, ctx, db, winnersLiveOperatorScope)
	seed := func(id string, payload map[string]any) {
		winnersLiveSeedFact(t, ctx, db, id, winnersLiveSourceScope, payload)
	}
	seed("f-update", winnersLiveFinding("CVE-2026-9001", "pkg:npm/update", 10, nil))
	seed("f-move-low", winnersLiveFinding("CVE-2026-9002", "pkg:npm/move", 5, nil))
	seed("f-move-mid", winnersLiveFinding("CVE-2026-9002", "pkg:npm/move", 7, nil))
	seed("f-delete", winnersLiveFinding("CVE-2026-9003", "pkg:npm/delete", 4, nil))
	seed("f-jsonb", winnersLiveFinding("CVE-2026-9004", "pkg:npm/jsonb", 3, nil))
	seed("f-expiry", winnersLiveFinding("CVE-2026-9005", "pkg:npm/expiry", 6, nil))
	for i, name := range []string{"quiet-a", "quiet-b", "quiet-c"} {
		seed("f-"+name, winnersLiveFinding("CVE-2026-91"+string(rune('0'+i)), "pkg:npm/"+name, 2, nil))
	}
	// An operator decision with no expiry: suppression_expires_at is NULL on
	// the stored row, so an unchanged tick must compare NULL equal to NULL.
	winnersLiveSeedFact(t, ctx, db, "op-expiry", winnersLiveOperatorScope, winnersLiveFinding(
		"CVE-2026-9005", "pkg:npm/expiry", 0, map[string]any{"suppression_state": "ignored"}))

	t1 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	first, err := store.ResweepWinners(ctx, t1)
	if err != nil {
		t.Fatalf("first resweep: %v", err)
	}
	if want := (supplychaincore.SupplyChainImpactWinnersResweep{Inserted: 8}); first != want {
		t.Fatalf("first resweep = %+v, want %+v (an empty table, no previous watermark)", first, want)
	}
	before := winnersLiveRead(t, ctx, db)
	if len(before) != 8 {
		t.Fatalf("winners after first resweep = %d, want 8", len(before))
	}

	// Unchanged facts: zero row versions, every winner counted unchanged, the
	// watermark advanced and the previous one reported.
	t2 := t1.Add(30 * time.Second)
	second, err := store.ResweepWinners(ctx, t2)
	if err != nil {
		t.Fatalf("second resweep: %v", err)
	}
	if want := (supplychaincore.SupplyChainImpactWinnersResweep{Unchanged: 8, PreviousWatermark: t1}); second != want {
		t.Fatalf("unchanged resweep = %+v, want %+v", second, want)
	}
	if after := winnersLiveRead(t, ctx, db); !reflect.DeepEqual(after, before) {
		t.Fatalf("an unchanged resweep rewrote winner rows:\nbefore %v\nafter  %v", before, after)
	}
	if got := winnersLiveWatermark(t, ctx, db); !got.Equal(t2) {
		t.Fatalf("watermark after the unchanged resweep = %v, want %v", got, t2)
	}

	// A mixed tick: a priority change, a winner that moves to another fact, a
	// retired key, a changed JSONB column, a NULL expiry that becomes a time,
	// and one brand-new key.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, '{priority_score}', '11') WHERE fact_id = 'f-update'`)
	seed("f-move-high", winnersLiveFinding("CVE-2026-9002", "pkg:npm/move", 9, nil))
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = 'f-delete'`)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, '{service_ids}', '["svc-a","svc-b"]') WHERE fact_id = 'f-jsonb'`)
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = payload || '{"suppression":{"expires_at":"2027-01-01T00:00:00Z"}}'::jsonb WHERE fact_id = 'op-expiry'`)
	seed("f-new", winnersLiveFinding("CVE-2026-9006", "pkg:npm/new", 8, nil))

	t3 := t2.Add(30 * time.Second)
	third, err := store.ResweepWinners(ctx, t3)
	if err != nil {
		t.Fatalf("third resweep: %v", err)
	}
	if want := (supplychaincore.SupplyChainImpactWinnersResweep{
		Inserted: 1, Updated: 4, Deleted: 1, Unchanged: 3, PreviousWatermark: t2,
	}); third != want {
		t.Fatalf("mixed resweep = %+v, want %+v", third, want)
	}
	mixed := winnersLiveRead(t, ctx, db)
	for key, row := range mixed {
		old, existed := before[key]
		switch {
		case !existed:
			if !row.writtenAt.Equal(t3) {
				t.Errorf("new winner written at %v, want %v", row.writtenAt, t3)
			}
		case row.xmin == old.xmin:
			if row.content != old.content || !row.writtenAt.Equal(t1) {
				t.Errorf("unchanged winner %s rewrote content or its written-at (%v)", key, row.writtenAt)
			}
		default:
			if row.content == old.content {
				t.Errorf("winner %s got a new row version with identical content", key)
			}
			if !row.writtenAt.Equal(t3) {
				t.Errorf("changed winner %s written at %v, want %v", key, row.writtenAt, t3)
			}
		}
	}
	rewritten := 0
	for key, row := range mixed {
		if old, existed := before[key]; existed && row.xmin != old.xmin {
			rewritten++
		}
	}
	if rewritten != 4 {
		t.Errorf("rewritten winners = %d, want exactly the 4 changed ones", rewritten)
	}
	if len(mixed) != 8 {
		t.Errorf("winners after the mixed tick = %d, want 8 (one deleted, one inserted)", len(mixed))
	}
	var movedWinner string
	var movedSources int
	if err := db.QueryRowContext(ctx, `SELECT winner_fact_id, source_count FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9002'`).Scan(&movedWinner, &movedSources); err != nil {
		t.Fatalf("read moved winner: %v", err)
	}
	if movedWinner != "f-move-high" || movedSources != 3 {
		t.Errorf("moved winner = %s from %d facts, want f-move-high from 3", movedWinner, movedSources)
	}
	var expires sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT suppression_expires_at FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9005'`).Scan(&expires); err != nil || !expires.Valid {
		t.Errorf("expiry after the change = %v, %v; want a time", expires, err)
	}

	// The reverse change: a time that becomes NULL is a change too, so an
	// operator removing an expiry cannot leave the old time on the stored row.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = payload - 'suppression' WHERE fact_id = 'op-expiry'`)
	cleared, err := store.ResweepWinners(ctx, t3.Add(10*time.Second))
	if err != nil {
		t.Fatalf("cleared-expiry resweep: %v", err)
	}
	if want := (supplychaincore.SupplyChainImpactWinnersResweep{Updated: 1, Unchanged: 7, PreviousWatermark: t3}); cleared != want {
		t.Fatalf("cleared-expiry resweep = %+v, want %+v", cleared, want)
	}
	if err := db.QueryRowContext(ctx, `SELECT suppression_expires_at FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9005'`).Scan(&expires); err != nil || expires.Valid {
		t.Errorf("expiry after clearing = %v, %v; want NULL", expires, err)
	}
	mixed = winnersLiveRead(t, ctx, db)

	// Oracle: wipe the read model and rebuild it from scratch. The incremental
	// table must equal the fresh recompute, ignoring when rows were written.
	winnersLiveExec(t, ctx, db, `DELETE FROM supply_chain_impact_canonical_winners`)
	fresh, err := store.ResweepWinners(ctx, t3.Add(30*time.Second))
	if err != nil {
		t.Fatalf("fresh resweep: %v", err)
	}
	if fresh.Inserted != 8 || fresh.Updated != 0 || fresh.Deleted != 0 || fresh.Unchanged != 0 {
		t.Fatalf("fresh resweep = %+v, want 8 inserted", fresh)
	}
	recomputed := winnersLiveRead(t, ctx, db)
	if len(recomputed) != len(mixed) {
		t.Fatalf("fresh recompute has %d winners, incremental table had %d", len(recomputed), len(mixed))
	}
	for key, row := range mixed {
		if got, ok := recomputed[key]; !ok || got.content != row.content {
			t.Errorf("winner %s differs from a fresh recompute:\nincremental %s\nfresh       %s", key, row.content, got.content)
		}
	}

	// A resweep over zero winners still stamps the watermark and removes the rows.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE`)
	t5 := t3.Add(time.Minute)
	last, err := store.ResweepWinners(ctx, t5)
	if err != nil {
		t.Fatalf("empty resweep: %v", err)
	}
	if last.Deleted != 8 || last.Inserted != 0 || last.Updated != 0 || last.Unchanged != 0 {
		t.Fatalf("empty resweep = %+v, want 8 deleted", last)
	}
	if got := winnersLiveWatermark(t, ctx, db); !got.Equal(t5) {
		t.Fatalf("watermark after reswept-to-zero = %v, want %v", got, t5)
	}
}

// TestSupplyChainImpactWinnersResweepSeesEveryColumnChangeLive proves the
// comparison covers each stored column. It changes one payload field at a time
// on a single winner and requires the next resweep to rewrite exactly that row.
// A column missing from the comparison would leave the stored row stale while
// every counter said "unchanged".
func TestSupplyChainImpactWinnersResweepSeesEveryColumnChangeLive(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#9774 winners per-column change proof")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	winnersLiveSeedScope(t, ctx, db, winnersLiveSourceScope)
	winnersLiveSeedFact(t, ctx, db, "f-column", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9500", "pkg:npm/column", 5, nil))

	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tick := func() supplychaincore.SupplyChainImpactWinnersResweep {
		t.Helper()
		at = at.Add(30 * time.Second)
		result, err := store.ResweepWinners(ctx, at)
		if err != nil {
			t.Fatalf("resweep: %v", err)
		}
		return result
	}
	if got := tick(); got.Inserted != 1 {
		t.Fatalf("first resweep = %+v, want 1 inserted", got)
	}

	for _, step := range []struct{ field, value string }{
		{"finding_id", `"finding:renamed"`},
		{"priority_score", `77`},
		{"ecosystem", `"pypi"`},
		{"image_ref", `"registry.example.com/app:1"`},
		{"priority_bucket", `"p0"`},
		{"detection_profile", `"osv"`},
		{"match_reason", `"exact_version"`},
		{"cvss_score", `9.8`},
		{"service_ids", `["svc-z"]`},
		{"workload_ids", `["wl-z"]`},
		{"environments", `["staging"]`},
	} {
		winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, ARRAY[$1::text], $2::jsonb) WHERE fact_id = 'f-column'`, step.field, step.value)
		if got := tick(); got.Updated != 1 || got.Inserted != 0 || got.Deleted != 0 || got.Unchanged != 0 {
			t.Errorf("changing %s: resweep = %+v, want exactly 1 updated", step.field, got)
		}
	}

	// A second, lower-priority fact for the same key changes only source_count.
	winnersLiveSeedFact(t, ctx, db, "f-column-dup", winnersLiveSourceScope, winnersLiveFinding("CVE-2026-9500", "pkg:npm/column", 1, nil))
	if got := tick(); got.Updated != 1 || got.Inserted != 0 || got.Deleted != 0 {
		t.Errorf("adding a duplicate fact: resweep = %+v, want 1 updated (source_count)", got)
	}
	// A canonical-key field makes a different key: the old row goes, a new one arrives.
	winnersLiveExec(t, ctx, db, `UPDATE fact_records SET payload = jsonb_set(payload, '{observed_version}', '"2.0.0"') WHERE fact_id IN ('f-column', 'f-column-dup')`)
	if got := tick(); got.Inserted != 1 || got.Deleted != 1 || got.Updated != 0 {
		t.Errorf("changing a key field: resweep = %+v, want 1 inserted and 1 deleted", got)
	}
	// Nothing changed: a final tick writes nothing.
	if got := tick(); got.Unchanged != 1 || got.Inserted+got.Updated+got.Deleted != 0 {
		t.Errorf("settled resweep = %+v, want 1 unchanged and no writes", got)
	}
}
