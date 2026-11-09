// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestStagedBatchDerivesFromShippedWinnerSelect is the freeze guard for lane A
// of the staged stale-winners audit scan (#10451): every candidate expression
// and eligibility predicate of the per-(scope, generation) batch must be
// byte-identical to the shipped winner select, and the operator-suppressions
// arm must share the same canonical-key expression. The batch pins its pair
// with parameters and never selects the wide fact row or raw payload.
func TestStagedBatchDerivesFromShippedWinnerSelect(t *testing.T) {
	t.Parallel()

	// The finding_id fallback rides only the source arm: the operator arm
	// selects '' on the same struct.
	sourceOnly := []string{
		supplyChainImpactSourceCandidateEligibilitySQL,
		"COALESCE(\n            NULLIF(fact.payload->>'finding_id', ''),",
	}
	for _, frag := range sourceOnly {
		for _, query := range []string{supplyChainImpactWinnerBodySQL, stagedWinnersSourceBatchSQL} {
			if !strings.Contains(query, frag) {
				t.Fatalf("source query lost fragment %q:\n%s", frag, query)
			}
		}
	}
	shared := []string{
		"CONCAT_WS('|',\n            COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),",
		"COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) AS priority_score",
		"CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END AS has_payload_finding_id",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') AS suppression_state",
	}
	for _, frag := range shared {
		if !strings.Contains(supplyChainImpactWinnerBodySQL, frag) {
			t.Fatalf("shipped winner body drifted away from shared staged-scan fragment %q", frag)
		}
		for _, query := range []string{stagedWinnersSourceBatchSQL, stagedWinnersOperatorBatchSQL} {
			if !strings.Contains(query, frag) {
				t.Fatalf("batch does not carry shared fragment %q:\n%s", frag, query)
			}
		}
	}
	if !strings.Contains(supplyChainImpactWinnerBodySQL, supplyChainImpactOperatorSuppressionPredicate) {
		t.Fatalf("shipped winner body drifted away from the operator suppression predicate")
	}
	if !strings.Contains(stagedWinnersOperatorBatchSQL, supplyChainImpactOperatorSuppressionPredicate) {
		t.Fatalf("operator batch lost the suppression predicate:\n%s", stagedWinnersOperatorBatchSQL)
	}
	for _, query := range []string{stagedWinnersSourceBatchSQL, stagedWinnersOperatorBatchSQL} {
		// The pinned active_facts CTE projects only the columns the
		// candidate expressions read; the wide row itself never crosses.
		if strings.Contains(query, "fact.*") {
			t.Fatalf("batch must never select the wide fact row:\n%s", query)
		}
		// The output shape is exactly the six narrow columns: the raw
		// payload never leaves the database.
		if !strings.Contains(query, "SELECT "+stagedWinnersNarrowColumns) {
			t.Fatalf("batch output is not exactly the narrow columns:\n%s", query)
		}
		if !strings.HasSuffix(query, "ORDER BY fact_id ASC") {
			t.Fatalf("batch output is not deterministically ordered:\n%s", query)
		}
		for _, param := range []string{"fact.scope_id = $1", "fact.generation_id = $2"} {
			if !strings.Contains(query, param) {
				t.Fatalf("batch must pin its pair with parameters, missing %q:\n%s", param, query)
			}
		}
		for _, want := range []string{
			"fact_kind = 'reducer_supply_chain_impact_finding'",
			"fact.is_tombstone = FALSE",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("batch lost the active-scan index predicate %q:\n%s", want, query)
			}
		}
	}
}

// stagedFakeRows is a hermetic db.Rows over literal cells. Scan assigns
// string and integer cells the way database/sql does so the production scan
// path runs unchanged.
type stagedFakeRows struct {
	cols [][]any
	pos  int
}

func (r *stagedFakeRows) Next() bool { r.pos++; return r.pos <= len(r.cols) }

func (r *stagedFakeRows) Err() error { return nil }

func (r *stagedFakeRows) Close() error { return nil }

func (r *stagedFakeRows) Scan(dest ...any) error {
	row := r.cols[r.pos-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d columns into %d destinations", len(row), len(dest))
	}
	for i, cell := range row {
		switch d := dest[i].(type) {
		case *string:
			s, ok := cell.(string)
			if !ok {
				return fmt.Errorf("column %d: want string, got %T", i, cell)
			}
			*d = s
		case *int:
			dv := reflect.ValueOf(dest[i]).Elem()
			sv := reflect.ValueOf(cell)
			switch sv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				dv.SetInt(sv.Int())
			default:
				return fmt.Errorf("column %d: want int, got %T", i, cell)
			}
		default:
			return fmt.Errorf("column %d: unsupported destination %T", i, dest[i])
		}
	}
	return nil
}

// stagedFakeDB records the query and arguments of every read and replays
// canned rows.
type stagedFakeDB struct {
	queries []string
	args    [][]any
	rows    [][]any
	err     error
}

func (d *stagedFakeDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.queries = append(d.queries, query)
	d.args = append(d.args, args)
	if d.err != nil {
		return nil, d.err
	}
	return &stagedFakeRows{cols: d.rows}, nil
}

// TestCensusPairsMapsActivePairs proves the enumerator returns every active
// pair the reader reports, in the index order it reports them, and that the
// census statement itself only admits active generations of the scope's pinned
// generation.
func TestCensusPairsMapsActivePairs(t *testing.T) {
	t.Parallel()

	reader := &stagedFakeDB{rows: [][]any{
		{"img://a", "gen-2"},
		{"img://b", "gen-9"},
		{"operator:vulnerability_suppressions", "gen-op"},
	}}
	pairs, err := CensusPairs(context.Background(), reader)
	if err != nil {
		t.Fatalf("CensusPairs() error = %v", err)
	}
	want := []ScopeGeneration{
		{ScopeID: "img://a", GenerationID: "gen-2"},
		{ScopeID: "img://b", GenerationID: "gen-9"},
		{ScopeID: "operator:vulnerability_suppressions", GenerationID: "gen-op"},
	}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("CensusPairs() = %+v, want %+v", pairs, want)
	}
	if len(reader.queries) != 1 || reader.queries[0] != stagedWinnersCensusSQL {
		t.Fatalf("census sent %q, want the census statement", reader.queries)
	}
	if len(reader.args[0]) != 0 {
		t.Fatalf("census takes no parameters, got %v", reader.args[0])
	}
	for _, marker := range []string{
		"generation.generation_id = scope.active_generation_id",
		"generation.status = 'active'",
		"ORDER BY scope.scope_id, scope.active_generation_id",
	} {
		if !strings.Contains(stagedWinnersCensusSQL, marker) {
			t.Fatalf("census SQL missing active-pair marker %q:\n%s", marker, stagedWinnersCensusSQL)
		}
	}
}

func TestCensusPairsRequiresReader(t *testing.T) {
	t.Parallel()

	if _, err := CensusPairs(context.Background(), nil); err == nil {
		t.Fatal("CensusPairs(nil) error = nil, want missing-reader error")
	}
}

// TestStageBatchSourceEligibility proves one batch pins its pair as parameters
// and carries the shipped eligibility predicates verbatim: operator-scope
// exclusion, derived-image exclusion, and the classification filter.
func TestStageBatchSourceEligibility(t *testing.T) {
	t.Parallel()

	reader := &stagedFakeDB{}
	if _, err := StageBatch(context.Background(), reader, "img://a", "gen-2"); err != nil {
		t.Fatalf("StageBatch() error = %v", err)
	}
	if len(reader.queries) != 1 || reader.queries[0] != stagedWinnersSourceBatchSQL {
		t.Fatalf("source batch sent %q, want the source batch statement", reader.queries)
	}
	if want := []any{"img://a", "gen-2"}; !reflect.DeepEqual(reader.args[0], want) {
		t.Fatalf("source batch args = %v, want %v", reader.args[0], want)
	}
	for _, marker := range []string{
		"fact.scope_id <> 'operator:vulnerability_suppressions'",
		"fact.scope_id NOT LIKE 'image://derived/%'",
		"finding_association.finding_fact_id IS NULL",
		"(finding_class.source_class = 'static' AND finding_class.lifecycle = 'active')",
	} {
		if !strings.Contains(stagedWinnersSourceBatchSQL, marker) {
			t.Fatalf("source batch lost eligibility marker %q:\n%s", marker, stagedWinnersSourceBatchSQL)
		}
	}
}

func TestStageBatchRequiresPair(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		scope      string
		generation string
	}{
		{"nil reader", "img://a", "gen-2"},
		{"empty scope", "", "gen-2"},
		{"empty generation", "img://a", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var reader db.Queryer
			if tc.name != "nil reader" {
				reader = &stagedFakeDB{}
			}
			if _, err := StageBatch(context.Background(), reader, tc.scope, tc.generation); err == nil {
				t.Fatal("StageBatch() error = nil, want contract error")
			}
		})
	}
}

// TestStagedRowFindingFallbackAndFlag proves the row semantics lane B compacts
// on: the server-side finding_id fallback stays populated on source rows while
// has_payload_finding_id records whether the payload carried one, and the
// operator arm leaves finding_id empty on the same struct.
func TestStagedRowFindingFallbackAndFlag(t *testing.T) {
	t.Parallel()

	reader := &stagedFakeDB{rows: [][]any{
		{"fact-with-id", "canon|A", "payload-id-1", 30, 1, "active"},
		{"fact-fallback", "canon|B", "canon|B", 10, 0, "suppressed"},
	}}
	rows, err := StageBatch(context.Background(), reader, "img://a", "gen-2")
	if err != nil {
		t.Fatalf("StageBatch() error = %v", err)
	}
	want := []StagedRow{
		{FactID: "fact-with-id", CanonicalKey: "canon|A", FindingID: "payload-id-1", PriorityScore: 30, HasPayloadFindingID: true, SuppressionState: "active"},
		{FactID: "fact-fallback", CanonicalKey: "canon|B", FindingID: "canon|B", PriorityScore: 10, HasPayloadFindingID: false, SuppressionState: "suppressed"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("StageBatch() = %+v, want %+v", rows, want)
	}
}

// TestStageBatchOperatorArm proves the operator-suppressions arm rides the
// same struct with finding_id empty and the suppression predicate applied.
func TestStageBatchOperatorArm(t *testing.T) {
	t.Parallel()

	reader := &stagedFakeDB{rows: [][]any{
		{"fact-op", "canon|O", "", 5, 0, "suppressed"},
	}}
	rows, err := StageBatch(context.Background(), reader, "operator:vulnerability_suppressions", "gen-op")
	if err != nil {
		t.Fatalf("StageBatch() error = %v", err)
	}
	if len(reader.queries) != 1 || reader.queries[0] != stagedWinnersOperatorBatchSQL {
		t.Fatalf("operator batch sent %q, want the operator batch statement", reader.queries)
	}
	if want := []any{"operator:vulnerability_suppressions", "gen-op"}; !reflect.DeepEqual(reader.args[0], want) {
		t.Fatalf("operator batch args = %v, want %v", reader.args[0], want)
	}
	want := []StagedRow{
		{FactID: "fact-op", CanonicalKey: "canon|O", FindingID: "", PriorityScore: 5, HasPayloadFindingID: false, SuppressionState: "suppressed"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("StageBatch() = %+v, want %+v", rows, want)
	}
}
