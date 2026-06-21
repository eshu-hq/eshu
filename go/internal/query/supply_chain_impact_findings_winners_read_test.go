package query

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
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

	filter := SupplyChainImpactFindingFilter{ImpactStatus: "affected_exact", Limit: 51}

	for _, tc := range []struct {
		name        string
		fromWinners bool
		wantQuery   string
	}{
		{"legacy", false, listSupplyChainImpactFindingsQuery},
		{"winners", true, listSupplyChainImpactFindingsFromWinnersQuery},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingImpactQueryer{}
			store := NewPostgresSupplyChainImpactFindingStoreWithReadModel(rec, tc.fromWinners)
			_, _ = store.ListSupplyChainImpactFindings(context.Background(), filter)
			if rec.lastQuery != tc.wantQuery {
				t.Fatalf("%s gate issued the wrong query", tc.name)
			}
		})
	}
}

func TestSupplyChainImpactWinnersReadEnabled(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"true": true, "TRUE": true, " true ": true, "True": true,
		"": false, "false": false, "1": false, "yes": false,
	} {
		if got := SupplyChainImpactWinnersReadEnabled(value); got != want {
			t.Fatalf("SupplyChainImpactWinnersReadEnabled(%q) = %v, want %v", value, got, want)
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

	q := listSupplyChainImpactFindingsFromWinnersQuery
	for _, want := range []string{
		"FROM supply_chain_impact_canonical_winners AS w",
		"JOIN fact_records AS refetch",
		"ON refetch.fact_id = w.winner_fact_id",
		"w.severity_bucket = $12",
		"w.match_reason IN (", // the precise-detection branch parity
		"w.winner_scope_id = ANY($23::text[])",
		"ORDER BY",
		"LIMIT $19",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("winners read query missing %q", want)
		}
	}
	for _, banned := range []string{
		"ROW_NUMBER()",
		"PARTITION BY canonical_key",
		"JOIN ingestion_scopes",
		"JOIN scope_generations",
	} {
		if strings.Contains(q, banned) {
			t.Fatalf("winners read query must not contain %q (defeats O(page) / re-dedups)", banned)
		}
	}
}
