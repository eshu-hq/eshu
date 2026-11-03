// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSupplyChainImpactWinnerSelectMirrorsReadDedup pins the read/write parity
// the materialization depends on (#3389): the recompute winner selection must
// use the same canonical_key, public finding_id fallback, source-owned
// tiebreak, severity bucket, and independently ranked operator suppression
// overlay that the read-time dedup uses. If these drift, the materialized
// winner stops matching what the read would have picked.
func TestSupplyChainImpactWinnerSelectMirrorsReadDedup(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// fact_kind + active-generation scope, same as the read.
		"fact.fact_kind = 'reducer_supply_chain_impact_finding'",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		// canonical_key first component is the cve/advisory COALESCE.
		"COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '')",
		// public finding_id fallback to canonical_key.
		"COALESCE(\n            NULLIF(fact.payload->>'finding_id', ''),",
		// Source truth and operator decisions rank independently. The source
		// owns row identity and filter dimensions; an operator-only key cannot
		// fabricate a finding.
		"source_candidates AS (",
		"fact.scope_id <> 'operator:vulnerability_suppressions'",
		"source_ranked AS (",
		"COUNT(*) OVER (PARTITION BY canonical_key) AS source_count",
		"source_winners AS (",
		"SELECT DISTINCT ON (canonical_key) *",
		"operator_candidates AS (",
		"fact.scope_id = 'operator:vulnerability_suppressions'",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') <> 'active'",
		"operator_overrides AS (",
		"FROM source_winners AS source",
		"LEFT JOIN operator_overrides AS override",
		"ON override.canonical_key = source.canonical_key",
		// Exact source and operator dedup tiebreak.
		"ORDER BY canonical_key,",
		"priority_score DESC,",
		"has_payload_finding_id DESC,",
		"fact_id ASC",
		// severity bucket + suppression default, same thresholds/strings as read.
		"THEN 'critical'",
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active')",
		// Only operator decisions carry a parsed read-time expiry. Invalid
		// non-empty values fail closed as already expired.
		"WHEN override.fact_id IS NULL",
		"pg_input_is_valid",
		"ELSE '-infinity'::timestamptz",
		"END AS suppression_expires_at",
	} {
		if !strings.Contains(supplyChainImpactWinnerSelectSQL, want) {
			t.Fatalf("winner select SQL missing read-parity marker %q:\n%s", want, supplyChainImpactWinnerSelectSQL)
		}
	}
}

// TestRebuildSupplyChainImpactWinnersSQLIsAtomicReconcile pins that the rebuild
// upserts current winners, deletes winners no longer in the active set, and
// stamps the maintainer watermark in one statement (no torn rebuild visible to
// readers). The winners upsert and delete are data-modifying CTEs; the final,
// unconditional watermark upsert runs even on a zero-winner resweep so the read
// can tell "never populated" from "reswept to zero findings".
func TestRebuildSupplyChainImpactWinnersSQLIsAtomicReconcile(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WITH winners_now AS (",
		"INSERT INTO supply_chain_impact_canonical_winners",
		"suppression_state, suppression_expires_at",
		"ON CONFLICT (canonical_key) DO UPDATE SET",
		"suppression_expires_at = EXCLUDED.suppression_expires_at",
		"deleted AS (\n    DELETE FROM supply_chain_impact_canonical_winners w\n    WHERE NOT EXISTS (SELECT 1 FROM winners_now n WHERE n.canonical_key = w.canonical_key)\n    RETURNING 1\n)",
		// Unconditional watermark upsert is the final statement so it stamps even
		// a zero-winner resweep.
		"INSERT INTO supply_chain_impact_winners_materialization (singleton, materialized_at)",
		"ON CONFLICT (singleton) DO UPDATE SET materialized_at = EXCLUDED.materialized_at",
	} {
		if !strings.Contains(rebuildSupplyChainImpactWinnersSQL, want) {
			t.Fatalf("rebuild SQL missing %q:\n%s", want, rebuildSupplyChainImpactWinnersSQL)
		}
	}
}

func TestRebuildAllWinnersRequiresDB(t *testing.T) {
	t.Parallel()

	store := SupplyChainImpactWinnersStore{}
	if err := store.RebuildAllWinners(context.Background(), nil); err == nil {
		t.Fatal("RebuildAllWinners() error = nil, want missing-db error")
	}
}

// captureProbeTx is a transaction whose statements each report two affected
// rows and whose Commit result the test chooses.
type captureProbeTx struct {
	commitErr error
	commits   int
	rollbacks int
}

func (*captureProbeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResultWithRowsAffected{rowsAffected: 2}, nil
}

func (*captureProbeTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("captureProbeTx runs no reads")
}

func (t *captureProbeTx) Commit() error   { t.commits++; return t.commitErr }
func (t *captureProbeTx) Rollback() error { t.rollbacks++; return nil }

type captureProbeBeginner struct{ tx *captureProbeTx }

func (b captureProbeBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

// captureRowsBySource reads the winners capture counter by its source label.
func captureRowsBySource(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_supply_chain_impact_winners_capture_rows_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				source, _ := dp.Attributes.Value("source")
				got[source.AsString()] += dp.Value
			}
		}
	}
	return got
}

// TestSupplyChainImpactTxCountsCaptureRowsOnlyAfterCommit pins #9957: the
// writer reports its fact_upsert and fact_retract capture rows while its
// statements run, but the counter moves only when the transaction commits. A
// rolled-back attempt, and a commit that fails, add nothing, so the counter
// stays equal to the dirty-log rows that exist.
func TestSupplyChainImpactTxCountsCaptureRowsOnlyAfterCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		end  func(supplychaincore.SupplyChainImpactTx) error
		want map[string]int64
	}{
		{
			name: "committed attempt counts both sources",
			end:  func(tx supplychaincore.SupplyChainImpactTx) error { return tx.Commit() },
			want: map[string]int64{"fact_upsert": 2, "fact_retract": 5},
		},
		{
			name: "rolled-back attempt counts nothing",
			end:  func(tx supplychaincore.SupplyChainImpactTx) error { return tx.Rollback() },
			want: map[string]int64{},
		},
		{
			name: "failed commit counts nothing",
			end: func(tx supplychaincore.SupplyChainImpactTx) error {
				if err := tx.Commit(); err == nil {
					return errors.New("Commit() error = nil, want the commit failure")
				}
				return nil
			},
			want: map[string]int64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			probe := &captureProbeTx{}
			if tt.name == "failed commit counts nothing" {
				probe.commitErr = errors.New("commit failed")
			}
			tx, err := SupplyChainImpactBeginner{Beginner: captureProbeBeginner{tx: probe}, Instruments: inst}.
				BeginSupplyChainImpactTx(context.Background())
			if err != nil {
				t.Fatalf("BeginSupplyChainImpactTx() error = %v", err)
			}
			capturer, ok := tx.(supplychaincore.SupplyChainImpactWinnersCapturer)
			if !ok {
				t.Fatal("transaction does not implement SupplyChainImpactWinnersCapturer")
			}
			ids := []supplychaincore.WinnersDirtyIdentifier{{PackageID: "p1"}, {PackageID: "p2"}}
			if _, err := capturer.CaptureWinnersDirtyFacts(context.Background(), ids); err != nil {
				t.Fatalf("CaptureWinnersDirtyFacts() error = %v", err)
			}
			capturer.RecordWinnersCapture(context.Background(), telemetry.SupplyChainImpactWinnersSourceFactRetract, 5)

			if got := captureRowsBySource(t, reader); len(got) != 0 {
				t.Fatalf("capture rows before the transaction ended = %v, want none", got)
			}
			if err := tt.end(tx); err != nil {
				t.Fatal(err)
			}
			if got := captureRowsBySource(t, reader); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("capture rows by source = %v, want %v", got, tt.want)
			}
		})
	}
}
