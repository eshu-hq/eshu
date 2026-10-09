// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// selectionRowsOutsideLedger sums a candidate's rows outside the ledger
// tables, computed here independently of the store's helper.
func selectionRowsOutsideLedger(rows map[string]int64) int64 {
	var n int64
	for table, count := range rows {
		if !strings.HasPrefix(table, "changed_since_") {
			n += count
		}
	}
	return n
}

// TestSelectCandidatesWithinRowLimitProperties is PB2 of arbiter ruling
// arb-7127-3d-b: over generated candidate lists, the selection
//  1. keeps a batch of two or more within BatchRowLimit,
//  2. keeps a batch of one within the limit outside the ledger,
//  3. selects at least one candidate whenever one has its rows outside the
//     ledger within the limit (progress: nothing is skipped for good but a
//     generation whose own rows exceed the limit),
//  4. never selects a candidate whose rows outside the ledger exceed it,
//
// and partitions the candidates into selected and skipped. RED: with the
// per-candidate term (candidateRows > limit) restored, property 3 fails.
func TestSelectCandidatesWithinRowLimitProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(7127, 3))
	const limit = 100
	for iteration := range 5000 {
		n := 1 + rng.IntN(8)
		var candidates []generationRetentionCandidate
		counts := map[string]map[string]int64{}
		for i := range n {
			id := fmt.Sprintf("g%d", i)
			candidates = append(candidates, generationRetentionCandidate{generationID: id})
			rows := map[string]int64{"fact_records": int64(rng.IntN(2 * limit))}
			if rng.IntN(3) == 0 {
				rows["content_entities"] = int64(rng.IntN(limit / 2))
			}
			if rng.IntN(2) == 0 {
				rows["changed_since_link_deltas"] = int64(rng.IntN(4 * limit))
				rows["changed_since_links"] = int64(1 + rng.IntN(2))
			}
			counts[id] = rows
		}
		selected, _, selectedRows, skipped := selectCandidatesWithinRowLimit(candidates, counts, limit)

		var total int64
		for _, c := range selected {
			total += generationRetentionRowsTotal(counts[c.generationID])
			if selectionRowsOutsideLedger(counts[c.generationID]) > limit {
				t.Fatalf("iteration %d: selected %s with %v, rows outside the ledger over %d", iteration, c.generationID, counts[c.generationID], limit)
			}
			if selectedRows[c.generationID] == nil {
				t.Fatalf("iteration %d: selected %s has no event counts", iteration, c.generationID)
			}
		}
		if len(selected) >= 2 && total > limit {
			t.Fatalf("iteration %d: a batch of %d holds %d rows, over %d", iteration, len(selected), total, limit)
		}
		progress := slices.ContainsFunc(candidates, func(c generationRetentionCandidate) bool {
			return selectionRowsOutsideLedger(counts[c.generationID]) <= limit
		})
		if progress && len(selected) == 0 {
			t.Fatalf("iteration %d: nothing selected though a candidate fits outside the ledger: %v", iteration, counts)
		}
		if len(selected)+len(skipped) != n {
			t.Fatalf("iteration %d: %d selected + %d skipped of %d", iteration, len(selected), len(skipped), n)
		}
	}
}

// TestSelectCandidatesWithinRowLimitCases pins the rule's rows, one case each.
func TestSelectCandidatesWithinRowLimitCases(t *testing.T) {
	const limit = 60
	for _, tc := range []struct {
		name     string
		rows     []map[string]int64
		selected []string
		reasons  []string
	}{
		{
			"own rows over the limit skip (ADR #2248)",
			[]map[string]int64{{"fact_records": 70}, {"fact_records": 5}},
			[]string{"g1"},
			[]string{"row_limit_own_rows"},
		},
		{"first candidate over only by ledger is admitted alone", []map[string]int64{
			{"fact_records": 5, "changed_since_link_deltas": 90}, {"fact_records": 1},
		}, []string{"g0"}, []string{"row_limit"}},
		{"later candidate over only by ledger is deferred", []map[string]int64{
			{"fact_records": 5}, {"fact_records": 5, "changed_since_link_deltas": 90},
		}, []string{"g0"}, []string{"row_limit_ledger"}},
		{"later candidate that fits alone waits for a full batch", []map[string]int64{
			{"fact_records": 40}, {"fact_records": 30},
		}, []string{"g0"}, []string{"row_limit"}},
		{"a zero-row candidate after an over-limit one is skipped", []map[string]int64{
			{"fact_records": 5, "changed_since_link_deltas": 90}, {},
		}, []string{"g0"}, []string{"row_limit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var candidates []generationRetentionCandidate
			counts := map[string]map[string]int64{}
			for i, rows := range tc.rows {
				id := fmt.Sprintf("g%d", i)
				candidates = append(candidates, generationRetentionCandidate{generationID: id})
				counts[id] = rows
			}
			selected, _, _, skipped := selectCandidatesWithinRowLimit(candidates, counts, limit)
			var ids, reasons []string
			for _, c := range selected {
				ids = append(ids, c.generationID)
			}
			for _, id := range skipped {
				reasons = append(reasons, rowLimitSkipReason(counts[id], limit))
			}
			if !slices.Equal(ids, tc.selected) || !slices.Equal(reasons, tc.reasons) {
				t.Fatalf("selected %v skipped reasons %v, want %v and %v", ids, reasons, tc.selected, tc.reasons)
			}
		})
	}
}

func recheckCandidate(scope, id string, now time.Time, ageDays int) []any {
	return []any{scope, id, "repository", now.Add(-time.Duration(ageDays) * 24 * time.Hour), now.Add(-time.Duration(ageDays+1) * 24 * time.Hour)}
}

// TestGenerationRetentionRecheckDefersTheGrownMember is PB3's first fixture.
// a, b and c: c is deferred, and the recount moves the link b shares with c
// onto b, so a + b exceeds the limit. b is deferred, a is pruned, and the rows
// of the batch stay within the limit. RED: without the re-check, a and b are
// both pruned over the limit.
func TestGenerationRetentionRecheckDefersTheGrownMember(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			recheckCandidate("scope-r", "a", now, 14), recheckCandidate("scope-r", "b", now, 13), recheckCandidate("scope-r", "c", now, 12),
		},
		countRows: [][]any{{"a", "fact_records", int64(10)}, {"b", "fact_records", int64(10)}, {"c", "fact_records", int64(40)}},
		// First count: the (b -> c) link of 45 rows is c's.
		ledgerCountRows: [][]any{{"a", "changed_since_links", int64(1)}, {"c", "changed_since_link_deltas", int64(45)}},
		// Recount without c: that link is deleted with b, so it is b's.
		ledgerRecountRows: [][]any{{"a", "changed_since_links", int64(1)}, {"b", "changed_since_link_deltas", int64(45)}},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 || result.RowsOverLimit != 0 {
		t.Fatalf("pruned %d over by %d, skipped %v; want a alone within the limit", result.GenerationsPruned, result.RowsOverLimit, result.Skipped)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			if ids, _ := exec.args[0].([]string); !slices.Equal(ids, []string{"a"}) {
				t.Fatalf("deleted generations %v, want [a]", ids)
			}
		}
	}
}

// TestGenerationRetentionRecheckIsCapped is PB3's second fixture: every
// recount grows the last member, forcing more than three re-checks. After
// three the pass keeps the first selected candidate only, recounts it once,
// and stops: one generation, and at most five planning count statements plus
// #7334 fix 1's one mandatory recount under the re-locked set, six total.
func TestGenerationRetentionRecheckIsCapped(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	grow := func(id string, n int64) [][]any {
		return [][]any{{id, "changed_since_link_deltas", n}}
	}
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			recheckCandidate("s", "a", now, 20), recheckCandidate("s", "b", now, 19), recheckCandidate("s", "c", now, 18),
			recheckCandidate("s", "d", now, 17), recheckCandidate("s", "e", now, 16), recheckCandidate("s", "f", now, 15),
		},
		countRows: [][]any{
			{"a", "fact_records", int64(5)},
			{"b", "fact_records", int64(5)},
			{"c", "fact_records", int64(5)},
			{"d", "fact_records", int64(5)},
			{"e", "fact_records", int64(5)},
			{"f", "fact_records", int64(100)},
		},
		// Count 1 skips f on its own rows; each recount then grows the last member.
		ledgerCountScripts: [][][]any{
			nil, grow("e", 40), grow("d", 45), grow("c", 50), grow("b", 55), grow("a", 0),
		},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("pruned %d, skipped %v; want one generation after the capped re-check", result.GenerationsPruned, result.Skipped)
	}
	if database.countCalls > 6 {
		t.Fatalf("%d count statements for one candidate query, want at most 6 (5 planning + 1 re-lock recount)", database.countCalls)
	}
}
