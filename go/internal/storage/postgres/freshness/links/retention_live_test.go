// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// seedP1 links scope-a over g1..g5 with g1 and g2 old enough to prune and g3,
// g4 inside the window, plus a control scope-b whose generations are all
// retained.
func (l *ledgerDB) seedP1(t *testing.T) {
	t.Helper()
	w := linksfreshnessstore.NewLinkWriter(l.store)
	old, recent := fixtureEpoch.Add(-48*time.Hour), time.Now().UTC()
	l.seedChainAt(t, "scope-a", []string{"g1", "g2", "g3", "g4", "g5"},
		[]time.Time{old, old.Add(time.Minute), recent, recent.Add(time.Minute), {}})
	for range 5 {
		mustLink(t, w, l, "scope-a")
	}
	l.linkChain(t, w, "scope-b", []string{"b1", "b2"}, recent)
}

// expectedAfterPrune applies the ruling-2.8 rule to a snapshot.
func expectedAfterPrune(before, pruned []string) (want []string, deleted map[string]int64) {
	for _, row := range before {
		if !namesGeneration(row, pruned) {
			want = append(want, row)
		}
	}
	deleted = countByTable(before)
	for table, n := range countByTable(want) {
		deleted[table] -= n
	}
	return want, deleted
}

func onlyTables(rows []string, tables ...string) []string {
	var out []string
	for _, row := range rows {
		if slices.Contains(tables, strings.SplitN(row, "|", 2)[0]) {
			out = append(out, row)
		}
	}
	return out
}

// TestRetentionRuleP1 is P1 of arbiter ruling arb-7127-3d. A scope linked
// g1..g5; retention prunes g1 and g2. Every link, delta and bucket row naming
// g1 or g2 on either side is gone, and so are their two activation rows; the
// remaining rows of all six tables equal the expected set (SHA-256 over
// ordered rows), and the state rows and the cursor row are byte-identical.
// RED: a planted rule that deletes on the generation side only leaves
// (g2 -> g3).
func TestRetentionRuleP1(t *testing.T) {
	l := openLedgerDB(t)
	l.seedP1(t)
	before := l.ledgerSnapshot(t)
	pruned := []string{"g1", "g2"}
	want, wantDeleted := expectedAfterPrune(before, pruned)
	for _, table := range retentionLedgerTables {
		if wantDeleted[table] == 0 {
			t.Fatalf("fixture has no %s row naming %v; the test would prove nothing", table, pruned)
		}
	}

	// RED first: the generation side only, inside a rolled-back transaction.
	planted := strings.Replace(linksfreshnessstore.RetentionPruneQueryForTest,
		` OR link.prior_generation_id = ANY ($2::text[])`, ``, 1)
	if planted == linksfreshnessstore.RetentionPruneQueryForTest {
		t.Fatal("planted generation-side rule: anchor not found")
	}
	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin planted: %v", err)
	}
	if _, err := tx.ExecContext(l.ctx, planted, []string{"scope-a", "scope-a"}, pruned); err != nil {
		t.Fatalf("planted delete: %v", err)
	}
	plantedAfter := ledgerSnapshotOn(t, l.ctx, tx)
	_ = tx.Rollback()
	if hashLines(plantedAfter) == hashLines(want) {
		t.Fatal("planted generation-side rule matched the ruling-2.8 set; the test cannot see the prior side")
	}
	if !slices.ContainsFunc(plantedAfter, func(row string) bool {
		return strings.HasPrefix(row, "changed_since_links|scope-a|g3|g2|")
	}) {
		t.Fatal("planted generation-side rule: (g2 -> g3) did not survive as expected")
	}

	result, err := postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(l.ctx, prunePolicy(1_000_000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations: %v", err)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (g1, g2)", result.GenerationsPruned)
	}
	after := l.ledgerSnapshot(t)
	if hashLines(after) != hashLines(want) {
		t.Fatalf("retained ledger differs from the ruling-2.8 rule:\n got %d rows %v\nwant %d rows %v", len(after), after, len(want), want)
	}
	stateAndCursor := []string{"changed_since_key_state", "changed_since_scope_cursor"}
	if hashLines(onlyTables(after, stateAndCursor...)) != hashLines(onlyTables(before, stateAndCursor...)) {
		t.Fatal("state or cursor rows changed")
	}
	for _, table := range retentionLedgerTables {
		if got := result.RowsPruned[table]; got != wantDeleted[table] {
			t.Errorf("RowsPruned[%s] = %d, want %d", table, got, wantDeleted[table])
		}
	}

	again, err := postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(l.ctx, prunePolicy(1_000_000))
	if err != nil || again.GenerationsPruned != 0 {
		t.Fatalf("second pass = %d generations, %v; want 0", again.GenerationsPruned, err)
	}
	if hashLines(l.ledgerSnapshot(t)) != hashLines(want) {
		t.Fatal("second pass changed the ledger (not idempotent)")
	}
}

// eventCounts reads the row_counts of the retention event of each generation.
func (l *ledgerDB) eventCounts(t *testing.T, gens ...string) map[string]map[string]int64 {
	t.Helper()
	out := map[string]map[string]int64{}
	for _, gen := range gens {
		var raw string
		if err := l.raw.QueryRowContext(l.ctx, `SELECT row_counts::text FROM generation_retention_events WHERE generation_id_hash = $1`,
			retentionGenerationHash(gen)).Scan(&raw); err != nil {
			t.Fatalf("event of %s: %v", gen, err)
		}
		counts := map[string]int64{}
		if err := json.Unmarshal([]byte(raw), &counts); err != nil {
			t.Fatalf("decode event of %s: %v", gen, err)
		}
		out[gen] = counts
	}
	return out
}

// eventSumsMatch reports whether, per ledger table, the events' counts sum to
// the rows the statement deleted.
func eventSumsMatch(events map[string]map[string]int64, deleted map[string]int64) bool {
	for _, table := range retentionLedgerTables {
		var sum int64
		for _, counts := range events {
			sum += counts[table]
		}
		if sum != deleted[table] {
			return false
		}
	}
	return true
}

// TestRetentionCountsP5 is P5 of arbiter ruling arb-7127-3d. Per ledger table
// the events' counts sum to the rows the statement deleted; a link naming two
// candidates is counted once, on the newer; the pre-count (SUM(delta_rows)
// for the deltas) equals what the statement deleted. RED: a planted count
// that charges a link to both candidates it names breaks the sums.
func TestRetentionCountsP5(t *testing.T) {
	l := openLedgerDB(t)
	l.seedP1(t)

	// RED: charge a link to every candidate it names.
	planted := strings.ReplaceAll(linksfreshnessstore.RetentionRowCountsQueryForTest,
		`FROM doomed WHERE doomed.rank = pruned.rank`,
		`FROM doomed WHERE pruned.generation_id IN (doomed.generation_id, doomed.prior_generation_id)`)
	if planted == linksfreshnessstore.RetentionRowCountsQueryForTest {
		t.Fatal("planted double charge: anchor not found")
	}
	rows, err := l.raw.QueryContext(l.ctx, planted, []string{"scope-a", "scope-a"}, []string{"g1", "g2"})
	if err != nil {
		t.Fatalf("planted count: %v", err)
	}
	plantedEvents := map[string]map[string]int64{}
	for rows.Next() {
		var gen, table string
		var n int64
		if err := rows.Scan(&gen, &table, &n); err != nil {
			t.Fatalf("scan planted: %v", err)
		}
		if plantedEvents[gen] == nil {
			plantedEvents[gen] = map[string]int64{}
		}
		plantedEvents[gen][table] = n
	}
	_ = rows.Close()

	result, err := postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(l.ctx, prunePolicy(1_000_000))
	if err != nil || result.GenerationsPruned != 2 {
		t.Fatalf("prune = %d generations, %v; want 2", result.GenerationsPruned, err)
	}
	if eventSumsMatch(plantedEvents, result.LedgerRowsPruned) {
		t.Fatal("planted double charge matched the deletes; the sum check cannot see it")
	}
	events := l.eventCounts(t, "g1", "g2")
	if !eventSumsMatch(events, result.LedgerRowsPruned) {
		t.Fatalf("event counts %v do not sum to the deleted rows %v", events, result.LedgerRowsPruned)
	}
	if !maps.Equal(result.LedgerRowsCounted, result.LedgerRowsPruned) {
		t.Fatalf("pre-count %v differs from the delete %v with no concurrent writer", result.LedgerRowsCounted, result.LedgerRowsPruned)
	}
	if got := events["g1"]["changed_since_links"]; got != 1 {
		t.Errorf("g1 is charged %d links, want 1 (its root)", got)
	}
	if got := events["g2"]["changed_since_links"]; got != 2 {
		t.Errorf("g2 is charged %d links, want 2 (g1 -> g2 once, on the newer, and g2 -> g3)", got)
	}
}

// TestRetentionRowLimitP5 covers P5's recount and ruling risk 3. c1 and c2
// are prunable and c2 is newer, so the first count charges the link
// (c1 -> c2) to c2. With a limit that fits c1 alone, c2 is deferred and the
// recount charges that link to c1, which still deletes it. The limit is
// re-checked on the recount: at exactly c1's recounted rows c1 is pruned
// within the limit and the rows deleted equal the recount; one row lower c1
// is over the limit only because of its ledger rows, so it is pruned in a
// batch of its own, over by one row (arbiter ruling arb-7127-3d-b).
func TestRetentionRowLimitP5(t *testing.T) {
	for _, tc := range []struct {
		name     string
		slack    int64
		wantOver int64
	}{
		{"limit equals recount", 0, 0},
		{"limit one below recount", -1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := openLedgerDB(t)
			w := linksfreshnessstore.NewLinkWriter(l.store)
			l.linkChain(t, w, "scope-c", []string{"c1", "c2", "c3"}, fixtureEpoch.Add(-48*time.Hour))
			c1Facts := l.queryInt(t, `SELECT count(*) FROM fact_records WHERE generation_id = 'c1'`)
			var c1Ledger int64 // rows the prune of c1 alone deletes
			for _, row := range l.ledgerSnapshot(t) {
				if namesGeneration(row, []string{"c1"}) {
					c1Ledger++
				}
			}
			c2Facts := l.queryInt(t, `SELECT count(*) FROM fact_records WHERE generation_id = 'c2'`)
			limit := c1Facts + c1Ledger + tc.slack
			if limit >= c1Facts+c1Ledger+c2Facts {
				t.Fatalf("fixture: limit %d would fit c2 too", limit)
			}

			result, err := postgres.NewGenerationRetentionStore(l.store).PruneSupersededGenerations(l.ctx, prunePolicy(int(limit)))
			if err != nil {
				t.Fatalf("prune: %v", err)
			}
			if result.GenerationsPruned != 1 || result.RowsOverLimit != tc.wantOver {
				t.Fatalf("pruned %d over by %d at limit %d (c1 facts %d + ledger %d), want c1 over by %d; skipped %v",
					result.GenerationsPruned, result.RowsOverLimit, limit, c1Facts, c1Ledger, tc.wantOver, result.Skipped)
			}
			var ledgerDeleted int64
			for _, n := range result.LedgerRowsPruned {
				ledgerDeleted += n
			}
			if ledgerDeleted != c1Ledger || !maps.Equal(result.LedgerRowsCounted, result.LedgerRowsPruned) {
				t.Fatalf("deleted %v (%d rows), recount %v; want %d rows and equal", result.LedgerRowsPruned, ledgerDeleted,
					result.LedgerRowsCounted, c1Ledger)
			}
			if events := l.eventCounts(t, "c1"); !eventSumsMatch(events, result.LedgerRowsPruned) {
				t.Fatalf("c1's event %v does not match the deletes %v", events, result.LedgerRowsPruned)
			}
		})
	}
}
