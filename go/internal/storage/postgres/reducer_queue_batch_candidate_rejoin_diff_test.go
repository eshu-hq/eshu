// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestClaimBatchCandidateReadsRepsWithoutRejoin is the #10916 shape gate:
// the batch claim's candidate CTE must read every column it needs from
// `reps` (which carries them snapshot-identically from `base`) instead of
// joining back to fact_work_items by primary key. The lock arms keep their
// fact_work_items lock_target probes untouched: those predicates are the
// EvalPlanQual recheck, not candidate reads.
func TestClaimBatchCandidateReadsRepsWithoutRejoin(t *testing.T) {
	t.Parallel()

	body := stripSQLComments(cteBody(claimReducerWorkBatchQuery, "candidate AS (", "locked_search_doc_reserved AS"))
	if body == "" {
		t.Fatal("could not slice the candidate CTE body from the shipped batch claim")
	}
	if strings.Contains(body, "fact_work_items") {
		t.Fatalf("candidate CTE still reads fact_work_items (the #10916 rejoin):\n%s", body)
	}
	for _, want := range []string{
		"reps.work_item_id",
		"CASE WHEN reps.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority",
		"CASE WHEN reps.work_item_id = source_heads.work_item_id THEN 0 ELSE 1 END AS reducer_source_fair_rank",
		"reps.updated_at AS updated_at",
		"live_fence_keys.conflict_domain = reps.conflict_domain",
		"live_fence_keys.ckey = reps.ckey",
		"FROM reps",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("candidate CTE missing reps-sourced read %q:\n%s", want, body)
		}
	}
	// The locked arms are out of scope: both keep the per-candidate
	// lock_target primary-key probe with its row-self rechecks.
	if got := strings.Count(claimReducerWorkBatchQuery, "FOR UPDATE OF lock_target SKIP LOCKED"); got != 2 {
		t.Fatalf("shipped claim has %d lock_target probes, want 2 (reserved + rest arms untouched)", got)
	}
	// The frozen OLD side must still carry the rejoin it freezes, and the
	// derived NEW side must not; both share their bytes through the
	// candidate CTE header, so the freeze cannot drift from the pre-change
	// chain upstream of candidate.
	oldQuery := frozenPreRejoinCandidate(t)
	if !strings.Contains(oldQuery, "JOIN fact_work_items ON fact_work_items.work_item_id = reps.work_item_id") {
		t.Fatal("frozen pre-rejoin candidate lost its fact_work_items rejoin; " +
			"it no longer represents the pre-#10916 query")
	}
	newQuery, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}
	if strings.Contains(newQuery, "JOIN fact_work_items ON fact_work_items.work_item_id = reps.work_item_id") {
		t.Fatal("derived candidate still re-joins fact_work_items; the #10916 rewrite is not in the shipped query")
	}
	if got := stripSQLComments(cteBody(newQuery, "candidate AS (", "SELECT work_item_id")); strings.Contains(got, "fact_work_items") {
		t.Fatalf("derived candidate CTE still reads fact_work_items:\n%s", got)
	}
	oldUpstream := oldQuery[strings.Index(oldQuery, "WITH "):strings.Index(oldQuery, "candidate AS (")]
	newUpstream := newQuery[strings.Index(newQuery, "WITH "):strings.Index(newQuery, "candidate AS (")]
	if oldUpstream != newUpstream {
		t.Fatal("frozen pre-rejoin candidate diverges from the shipped chain before candidate; " +
			"the differential would compare more than the #10916 rejoin removal")
	}
}

// TestClaimBatchCandidateRejoinRemovalMatchesPreChangeSetAndOrder is the
// #10916 bidirectional differential: the candidate SELECT derived from the
// shipped query (reps-sourced reads, no rejoin) must return the IDENTICAL
// candidate rows — every projected column, same set both directions, same
// order — as the pre-change candidate SELECT frozen in testdata (derived
// from the shipped query at the base commit, rejoin intact).
//
// The fixture covers every shape whose candidate read moved source:
// NULL conflict_key (reps.ckey scope fallback), a live-fenced
// representative, projector- and semantic-blocked representatives, the
// same vs fair_same representative divergence (including the
// expired-lease fair non-member and the search-doc sibling that loses its
// key), a batch LIMIT of 1, and the search-doc starvation ordering shape.
// The funnel census over the same fixture must count the same
// representative membership, since it composes the same chain.
//
// Skipped unless a DSN is provided, like the other claim differentials.
func TestClaimBatchCandidateRejoinRemovalMatchesPreChangeSetAndOrder(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the candidate-rejoin differential proof")
	}

	oldQuery := fullCandidateProjection(t, frozenPreRejoinCandidate(t))
	derivedNew, ok := deriveReadOnlyCandidateSelect(claimReducerWorkBatchQuery)
	if !ok {
		t.Fatal("could not derive read-only candidate SELECT from the shipped batch claim query")
	}
	newQuery := fullCandidateProjection(t, derivedNew)

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	seedCandidateRejoinFixture(t, ctx, db, now)
	clockA := now.Add(2 * time.Hour)
	clockB := now.Add(4 * time.Hour)

	type cell struct {
		name   string
		clock  time.Time
		filter []string
		drain  bool
		projN  int
		semCap int
		limit  int
	}
	var cells []cell
	for _, tc := range []struct {
		name  string
		clock time.Time
	}{{"clockA", clockA}, {"clockB", clockB}} {
		for _, drain := range []bool{false, true} {
			for _, caps := range []struct{ projN, semCap int }{{0, 0}, {2, 3}, {3, 3}, {2, 10}} {
				cells = append(cells, cell{
					name:  fmt.Sprintf("%s/drain=%v/proj=%d/sem=%d", tc.name, drain, caps.projN, caps.semCap),
					clock: tc.clock, drain: drain, projN: caps.projN, semCap: caps.semCap, limit: 1000,
				})
			}
		}
	}
	// Reserved-batch floor and a domain filter ride the same candidate
	// reads: LIMIT 1 must take the identical head row both sides.
	cells = append(cells,
		cell{name: "clockA/limit-1", clock: clockA, limit: 1},
		cell{name: "clockB/limit-1", clock: clockB, limit: 1},
		cell{name: "clockA/domain-filter", clock: clockA, filter: []string{"supply_chain_impact"}, drain: true, projN: 2, semCap: 10, limit: 1000},
	)

	// assertReclaimFirstHead proves the LIMIT 1 head property for the #10865
	// order: the head the new query takes under LIMIT 1 is the head of the
	// full new order for the same cell, and the full new order is the
	// reclaim-first partition of the full old order (0/0 set included).
	assertReclaimFirstHead := func(t *testing.T, c cell, newHead []candidateRow) {
		t.Helper()
		fullArgs := []any{c.clock, c.filter, "n/a", c.clock.Add(time.Hour), c.drain, c.projN, c.semCap, 1000}
		fullOld := queryCandidateRows(t, ctx, db, oldQuery, fullArgs)
		fullNew := queryCandidateRows(t, ctx, db, newQuery, fullArgs)
		if onlyInOld := diffStringSlices(candidateRowIDs(fullOld), candidateRowIDs(fullNew)); len(onlyInOld) != 0 {
			t.Fatalf("limit-1 cell full old order has rows the new order lacks: %v", onlyInOld)
		}
		if onlyInNew := diffStringSlices(candidateRowIDs(fullNew), candidateRowIDs(fullOld)); len(onlyInNew) != 0 {
			t.Fatalf("limit-1 cell full new order has rows the old order lacks: %v", onlyInNew)
		}
		if want := reclaimFirstExpectedOrder(candidateRowIDs(fullOld), expiredClaimedWorkItemIDs(t, ctx, db, c.clock)); fmt.Sprint(candidateRowIDs(fullNew)) != fmt.Sprint(want) {
			t.Fatalf("limit-1 cell full new order diverges from the #10865 reclaim-first order:\nOLD = %v\nNEW = %v\nwant = %v", fullOld, fullNew, want)
		}
		if len(fullNew) == 0 {
			if len(newHead) != 0 {
				t.Fatalf("limit-1 head = %v, want empty (full order is empty)", newHead)
			}
			return
		}
		if fmt.Sprint(newHead) != fmt.Sprint(fullNew[:1]) {
			t.Fatalf("limit-1 head = %v, want the full new order head %v", newHead, fullNew[:1])
		}
	}

	nonEmpty := 0
	for _, c := range cells {
		c := c
		t.Run(c.name, func(t *testing.T) {
			args := []any{c.clock, c.filter, "n/a", c.clock.Add(time.Hour), c.drain, c.projN, c.semCap, c.limit}
			oldRows := queryCandidateRows(t, ctx, db, oldQuery, args)
			newRows := queryCandidateRows(t, ctx, db, newQuery, args)
			if c.limit == 1 {
				// #10865: under LIMIT 1 the pre-change and reclaim-first
				// heads differ by design when an expired hold is present
				// (the hold floats to the head), so the 0/0 set premise
				// cannot hold here. Prove instead that the new head is
				// the head of the full new order for the same cell, and
				// that the full new order is the reclaim-first partition
				// of the full old order.
				assertReclaimFirstHead(t, c, newRows)
				if len(newRows) > 0 {
					nonEmpty++
				}
				return
			}
			onlyInOld := diffStringSlices(candidateRowIDs(oldRows), candidateRowIDs(newRows))
			onlyInNew := diffStringSlices(candidateRowIDs(newRows), candidateRowIDs(oldRows))
			if len(onlyInOld) != 0 || len(onlyInNew) != 0 {
				t.Fatalf("rejoin-removal candidate set diverges from pre-change set (want 0/0): "+
					"only in OLD = %v, only in NEW = %v\nOLD = %v\nNEW = %v",
					onlyInOld, onlyInNew, oldRows, newRows)
			}
			// #10865 intentionally reorders: expired holds float first.
			// The row multiset must stay identical (the moved reads are
			// value-identical), and the new ID order must be the old
			// order stable-partitioned by the expired set at the cell
			// clock.
			if fmt.Sprint(rowMultiset(oldRows)) != fmt.Sprint(rowMultiset(newRows)) {
				t.Fatalf("rejoin-removal candidate row multiset diverges:\nOLD = %v\nNEW = %v", oldRows, newRows)
			}
			if want := reclaimFirstExpectedOrder(candidateRowIDs(oldRows), expiredClaimedWorkItemIDs(t, ctx, db, c.clock)); fmt.Sprint(candidateRowIDs(newRows)) != fmt.Sprint(want) {
				t.Fatalf("rejoin-removal candidate rows diverge from the #10865 reclaim-first order:\nOLD = %v\nNEW = %v\nwant = %v", oldRows, newRows, want)
			}
			if len(oldRows) > 0 {
				nonEmpty++
			}
		})
	}
	if nonEmpty == 0 {
		t.Fatal("fixture produced zero candidates in every matrix cell; the differential is vacuous")
	}
	t.Logf("candidate-rejoin differential green in %d cells (%d non-empty)", len(cells), nonEmpty)

	argsFor := func(clock time.Time, drain bool, projN, semCap, limit int) []any {
		return []any{clock, nil, "n/a", clock.Add(time.Hour), drain, projN, semCap, limit}
	}
	deciding := []struct {
		name    string
		args    []any
		want    []string
		refused []string
	}{
		{
			name: "fenced-and-divergent-representatives-clockA",
			args: argsFor(clockA, false, 0, 0, 1000),
			want: []string{
				"rejoin-div-holder", "rejoin-sdkey-plain", "rejoin-proj-00",
				"rejoin-search-00", "rejoin-search-01",
			},
			refused: []string{
				"rejoin-fence-holder", "rejoin-fence-sibling",
				"rejoin-null-holder", "rejoin-null-sibling",
				"rejoin-div-pending-older", "rejoin-sdkey-search",
			},
		},
		{
			name:    "projector-blocked-representative",
			args:    argsFor(clockA, true, 2, 10, 1000),
			want:    []string{"rejoin-sem-00"},
			refused: []string{"rejoin-proj-00", "rejoin-proj-01"},
		},
		{
			name:    "semantic-blocked-representative",
			args:    argsFor(clockA, true, 3, 10, 1000),
			refused: []string{"rejoin-sem-00", "rejoin-sem-01"},
		},
		{
			name:    "semantic-rank-cap-refuses-tail",
			args:    argsFor(clockA, true, 2, 3, 1000),
			want:    []string{"rejoin-sem-00"},
			refused: []string{"rejoin-sem-03"},
		},
		{
			name: "expired-holders-reclaim-keys-clockB",
			args: argsFor(clockB, false, 0, 0, 1000),
			want: []string{"rejoin-fence-holder", "rejoin-null-holder", "rejoin-div-holder"},
			refused: []string{
				"rejoin-fence-sibling", "rejoin-null-sibling",
				"rejoin-div-pending-older",
			},
		},
	}
	for _, d := range deciding {
		t.Run(d.name, func(t *testing.T) {
			newRows := queryCandidateRows(t, ctx, db, newQuery, d.args)
			got := map[string]bool{}
			for _, row := range newRows {
				got[row.id] = true
			}
			for _, want := range d.want {
				if !got[want] {
					t.Errorf("deciding cell admits %v, want %s admitted", candidateRowIDs(newRows), want)
				}
			}
			for _, refused := range d.refused {
				if got[refused] {
					t.Errorf("deciding cell admits %s, want it refused", refused)
				}
			}
		})
	}

	// Search-doc starvation shape: the oldest rows are priority-1 search
	// documents, yet every priority-0 candidate sorts ahead of them. The
	// priority column itself moved source with #10916, so assert on its
	// value, and LIMIT 1 takes the same priority-0 head as the full order.
	full := queryCandidateRows(t, ctx, db, newQuery, argsFor(clockA, false, 0, 0, 1000))
	lastPriorityZero, firstSearch := -1, -1
	for i, row := range full {
		if row.priority == 0 {
			lastPriorityZero = i
		}
		if row.priority == 1 && firstSearch < 0 {
			firstSearch = i
		}
		if strings.HasPrefix(row.id, "rejoin-search-") && row.priority != 1 {
			t.Fatalf("search-doc %s carries priority %d, want 1", row.id, row.priority)
		}
	}
	if firstSearch < 0 || lastPriorityZero < 0 || firstSearch < lastPriorityZero {
		t.Fatalf("starvation order broken: first priority-1 at %d, last priority-0 at %d in %v",
			firstSearch, lastPriorityZero, candidateRowIDs(full))
	}
	head := queryCandidateRows(t, ctx, db, newQuery, argsFor(clockA, false, 0, 0, 1))
	if len(head) != 1 || head[0] != full[0] {
		t.Fatalf("LIMIT 1 head = %v, want the full order's head %v", head, full[0])
	}

	// Funnel membership identity: the census composes the same chain, so
	// its representative count equals the candidate row count.
	var counts [7]int64
	if err := db.QueryRowContext(ctx, reducerClaimBatchFunnelQuery,
		clockA, nil, "n/a", clockA.Add(time.Hour), false, 0, 0).Scan(
		&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6]); err != nil {
		t.Fatalf("run funnel census: %v", err)
	}
	if counts[6] != int64(len(full)) {
		t.Fatalf("funnel representatives = %d, want candidate rows %d (funnel membership must equal admission)",
			counts[6], len(full))
	}
}

// candidateRow is one full candidate-CTE row in claim order.
type candidateRow struct {
	id          string
	priority    int64
	srcInflight int64
	srcFair     int64
	domFair     int64
	domInflight int64
	updatedAt   string
}

func candidateRowIDs(rows []candidateRow) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.id
	}
	return ids
}

// rowMultiset renders rows order-insensitively so the #10865 reorder (holds
// first) can be asserted on IDs while the value-identity proof stays a
// multiset comparison.
func rowMultiset(rows []candidateRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprint(row))
	}
	sort.Strings(out)
	return out
}

// fullCandidateProjection widens a read-only candidate SELECT (derived or
// frozen, both ending in readOnlyCandidateTail) from bare ids to every
// candidate column, so the differential proves the moved reads
// (domain priority, fair ranks, updated_at) value-identical, not just
// membership- and order-identical. It fails loudly unless the tail it
// rewrites appears exactly once.
func fullCandidateProjection(t *testing.T, query string) string {
	t.Helper()
	const tailHead = "SELECT work_item_id\nFROM candidate"
	if got := strings.Count(query, tailHead); got != 1 {
		t.Fatalf("candidate SELECT tail %q appears %d times, want 1; the projection widening cannot anchor", tailHead, got)
	}
	return strings.Replace(query, tailHead,
		"SELECT work_item_id, reducer_domain_priority, reducer_source_inflight_count, "+
			"reducer_source_fair_rank, reducer_domain_fair_rank, reducer_domain_inflight_count, updated_at\nFROM candidate", 1)
}

func queryCandidateRows(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) []candidateRow {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query candidate rows: %v\nquery:\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()
	var out []candidateRow
	for rows.Next() {
		var row candidateRow
		var updated time.Time
		if err := rows.Scan(&row.id, &row.priority, &row.srcInflight, &row.srcFair,
			&row.domFair, &row.domInflight, &updated); err != nil {
			t.Fatalf("scan candidate row: %v", err)
		}
		row.updatedAt = updated.UTC().Format(time.RFC3339Nano)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate candidate rows: %v", err)
	}
	return out
}

// frozenPreRejoinCandidate reads the machine-derived pre-#10916 candidate
// SELECT frozen in testdata (see that file's header for provenance).
func frozenPreRejoinCandidate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	oldBytes, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "claim_candidate_pre_candidate_rejoin.sql"))
	if err != nil {
		t.Fatalf("read frozen pre-rejoin candidate: %v", err)
	}
	if len(oldBytes) == 0 {
		t.Fatal("frozen pre-rejoin candidate is empty")
	}
	return string(oldBytes)
}

// seedCandidateRejoinFixture seeds one disposable database with a row for
// every shape whose candidate read #10916 moves from fact_work_items to
// reps. now is the fixture epoch; the differential runs at now+2h and
// now+4h, and the live leases (to now+3h) straddle those clocks.
func seedCandidateRejoinFixture(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	seedGateScope(t, ctx, db, "scope-rejoin", "gen-rejoin", now)
	seedGateScope(t, ctx, db, "scope-rejoin-proj", "gen-rejoin-proj", now)
	seedGateScope(t, ctx, db, "scope-rejoin-null", "gen-rejoin-null", now)

	insert := func(item reducerFairnessWorkItem) {
		t.Helper()
		insertReducerFairnessWorkItem(t, ctx, db, item)
	}
	claim := func(id string, until time.Time) {
		t.Helper()
		mustExecGateArgs(t, ctx, db,
			`UPDATE fact_work_items SET status='claimed', lease_owner='rejoin', claim_until=$2 WHERE work_item_id=$1`, id, until)
	}

	for i := 0; i < 4; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("rejoin-plain-%02d", i), scopeID: "scope-rejoin", generationID: "gen-rejoin",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
			conflictKey: fmt.Sprintf("rejoin-plain-key-%02d", i), updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Live-fenced representative: the pending sibling is the in-base
	// representative at clockA (the holder's live lease keeps it out of
	// base) and the live_fence_keys anti-join must refuse it there.
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-fence-sibling", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-fence-key", updatedAt: now.Add(-time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-fence-holder", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-fence-key", updatedAt: now.Add(-30 * time.Minute),
	})
	claim("rejoin-fence-holder", now.Add(3*time.Hour))

	// NULL conflict_key: reps.ckey falls back to scope_id, so the live
	// holder fences its scope sibling through the same key comparison.
	mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, payload, created_at, updated_at
	) VALUES
	    ('rejoin-null-sibling', 'scope-rejoin-null', 'gen-rejoin-null', 'reducer', 'supply_chain_impact', 'scope',
	        NULL, 'pending', 0, jsonb_build_object('entity_key', 'rejoin-null-sibling'), $1, $1),
	    ('rejoin-null-holder', 'scope-rejoin-null', 'gen-rejoin-null', 'reducer', 'supply_chain_impact', 'scope',
	        NULL, 'claimed', 0, jsonb_build_object('entity_key', 'rejoin-null-holder'), $1, $1)`, now)
	mustExecGateArgs(t, ctx, db,
		`UPDATE fact_work_items SET lease_owner='rejoin-null', claim_until=$1 WHERE work_item_id='rejoin-null-holder'`,
		now.Add(3*time.Hour))

	// same vs fair_same divergence with an expired lease: the holder
	// (claimed_running_first) is the w_same representative while the older
	// pending sibling is the fair_same peer — the holder is a fair-rank
	// non-member yet must stay the admitted representative.
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-div-pending-older", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-div-key", updatedAt: now.Add(-time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-div-holder", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-div-key", updatedAt: now.Add(-30 * time.Minute),
	})
	claim("rejoin-div-holder", now.Add(time.Hour))

	// Search-doc sibling divergence: w_same deprioritizes the older
	// search document on the shared key, so the newer plain row is the
	// representative and the search document is refused as non-rep.
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-sdkey-search", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "eshu_search_document", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-sdkey", updatedAt: now.Add(-2 * time.Hour),
	})
	insert(reducerFairnessWorkItem{
		workItemID: "rejoin-sdkey-plain", scopeID: "scope-rejoin", generationID: "gen-rejoin",
		domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
		conflictKey: "rejoin-sdkey", updatedAt: now,
	})

	// Projector-blocked representatives: live projector work fences the
	// scope while $5 is on. The projector domain is not source_local, so
	// the semantic scalars still decide their own rows.
	mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, payload, created_at, updated_at
	) VALUES ('rejoin-projector-live', 'scope-rejoin-proj', 'gen-rejoin-proj', 'projector', 'code_call_materialization', 'scope',
	    'scope-rejoin-proj', 'pending', 0,
	    jsonb_build_object('entity_key', 'rejoin-projector-live'), $1, $1)`, now)
	for i := 0; i < 2; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("rejoin-proj-%02d", i), scopeID: "scope-rejoin-proj", generationID: "gen-rejoin-proj",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
			conflictKey: fmt.Sprintf("rejoin-proj-key-%02d", i), updatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Semantic-blocked representatives: four pending rows plus the two
	// succeeded source-local projectors the $6 done-count reads.
	for i := 0; i < 4; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("rejoin-sem-%02d", i), scopeID: "scope-rejoin", generationID: "gen-rejoin",
			domain: "semantic_entity_materialization", conflictDomain: reducerConflictDomainScope,
			conflictKey: fmt.Sprintf("rejoin-sem-key-%02d", i), updatedAt: now.Add(time.Duration(20+i) * time.Minute),
		})
	}
	for i := 0; i < 2; i++ {
		mustExecGateArgs(t, ctx, db, `INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, payload, created_at, updated_at
		) VALUES ($1, 'scope-rejoin', 'gen-rejoin', 'projector', 'source_local', 'scope',
		    'scope-rejoin', 'succeeded', 1,
		    jsonb_build_object('entity_key', $1::text), $2, $2)`, fmt.Sprintf("rejoin-projector-done-%02d", i), now)
	}

	// Search-doc starvation shape: the oldest rows overall are search
	// documents behind a deeper priority-0 backlog.
	for i := 0; i < 6; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("rejoin-busy-%02d", i), scopeID: "scope-rejoin", generationID: "gen-rejoin",
			domain: "supply_chain_impact", conflictDomain: reducerConflictDomainScope,
			conflictKey: fmt.Sprintf("rejoin-busy-key-%02d", i), updatedAt: now.Add(time.Duration(50+i) * time.Minute),
		})
	}
	for i := 0; i < 2; i++ {
		insert(reducerFairnessWorkItem{
			workItemID: fmt.Sprintf("rejoin-search-%02d", i), scopeID: "scope-rejoin", generationID: "gen-rejoin",
			domain: "eshu_search_document", conflictDomain: reducerConflictDomainScope,
			conflictKey: fmt.Sprintf("rejoin-search-key-%02d", i), updatedAt: now.Add(-3 * time.Hour).Add(time.Duration(i) * time.Minute),
		})
	}
}
