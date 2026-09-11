// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// recordingImpactQueryer captures the SQL the store issues so the read-gate
// selection can be asserted without a live database. It returns a sentinel error
// after recording, which is enough to verify which query was chosen.
type recordingImpactQueryer struct {
	lastQuery string
}

func (q *recordingImpactQueryer) QueryContext(_ context.Context, query string, _ ...any) (*sql.Rows, error) {
	q.lastQuery = query
	return nil, errors.New("recorded")
}

func TestSupplyChainImpactReadGateSelectsQuery(t *testing.T) {
	t.Parallel()

	filter := impact.SupplyChainImpactFindingFilter{ImpactStatus: "affected_exact", Limit: 51}

	for _, tc := range []struct {
		name        string
		fromWinners bool
		wantQuery   string
	}{
		{"legacy", false, impact.ListSupplyChainImpactFindingsQuery},
		{"winners", true, impact.ListSupplyChainImpactFindingsFromWinnersQuery},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingImpactQueryer{}
			store := impact.NewPostgresSupplyChainImpactFindingStoreWithReadModel(rec, tc.fromWinners)
			_, _ = store.ListSupplyChainImpactFindings(context.Background(), filter)
			if rec.lastQuery != tc.wantQuery {
				t.Fatalf("%s gate issued the wrong query", tc.name)
			}
		})
	}
}

func TestSupplyChainImpactWinnersReadEnabled(t *testing.T) {
	t.Parallel()

	// Mirrors strconv.ParseBool (the env registry's VarBool semantics): 1/t/T/
	// TRUE/true/True all enable; 0/f/false/empty and unparseable tokens leave it
	// off. This keeps the gate consistent with `eshu config validate`.
	for value, want := range map[string]bool{
		"true": true, "TRUE": true, " true ": true, "True": true,
		"1": true, "t": true, "T": true,
		"": false, "false": false, "0": false, "f": false, "yes": false, "on": false,
	} {
		if got := impact.SupplyChainImpactWinnersReadEnabled(value); got != want {
			t.Fatalf("impact.SupplyChainImpactWinnersReadEnabled(%q) = %v, want %v", value, got, want)
		}
	}
}

// TestSupplyChainImpactWinnersReadQueryShape pins the Phase 2 read shape: it
// reads from the maintained winners table, joins fact_records only for the page
// payloads, and does NOT deduplicate at read time (no ROW_NUMBER/PARTITION BY)
// nor re-join the active-generation tables (winner currency is
// materialization-enforced).
func TestSupplyChainImpactWinnersReadQueryShape(t *testing.T) {
	t.Parallel()

	q := impact.ListSupplyChainImpactFindingsFromWinnersQuery
	for _, want := range []string{
		"FROM supply_chain_impact_canonical_winners AS w",
		"JOIN fact_records AS refetch",
		"ON refetch.fact_id = page.winner_fact_id",
		"w.severity_bucket = $12",
		"w.match_reason IN (", // the precise-detection branch parity
		"w.winner_scope_id = ANY($23::text[])",
		// The cursor priority lookup MUST read from the same filtered+grant-scoped
		// set as the page (not the whole winners table), so an out-of-grant
		// after_finding_id cannot influence pagination. Pinned to match legacy
		// canonical_facts cursor semantics.
		"filtered AS NOT MATERIALIZED (",
		"SELECT c.priority_score FROM filtered c WHERE c.finding_id = $17",
		"paged AS MATERIALIZED (",
		"ORDER BY",
		"LIMIT $19",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("winners read query missing %q", want)
		}
	}
	filteredStart := strings.Index(q, "filtered AS NOT MATERIALIZED (")
	if filteredStart < 0 {
		t.Fatal("winners read query missing filtered CTE")
	}
	winnersRead := q[filteredStart:]
	for _, banned := range []string{
		"ROW_NUMBER()",
		"PARTITION BY canonical_key",
		"JOIN ingestion_scopes",
		"JOIN scope_generations",
	} {
		if strings.Contains(winnersRead, banned) {
			t.Fatalf("winners read query must not contain %q (defeats O(page) / re-dedups)", banned)
		}
	}
}
