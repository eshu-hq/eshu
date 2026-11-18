// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// phaseFakeResult is the sql.Result of the fake transaction's Exec statements.
type phaseFakeResult struct{}

func (phaseFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (phaseFakeResult) RowsAffected() (int64, error) { return 0, nil }

// phaseFakeRows is one scripted row (or none) for a QueryContext statement.
type phaseFakeRows struct {
	row    []any
	served bool
}

func (r *phaseFakeRows) Next() bool {
	if r.row == nil || r.served {
		return false
	}
	r.served = true
	return true
}

func (r *phaseFakeRows) Scan(dest ...any) error {
	if len(dest) != len(r.row) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(r.row))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *bool:
			*p = r.row[i].(bool)
		case *string:
			*p = r.row[i].(string)
		case *int64:
			*p = r.row[i].(int64)
		case *sql.NullTime:
			*p = sql.NullTime{}
		case *time.Time:
			*p = r.row[i].(time.Time)
		default:
			return fmt.Errorf("scan: unsupported destination %T", d)
		}
	}
	return nil
}

func (*phaseFakeRows) Err() error   { return nil }
func (*phaseFakeRows) Close() error { return nil }

// phaseFakeTx scripts the keyed tick's statements by their SQL text. delay
// slows one statement and failOn fails one, so a test can prove the time of a
// failed phase is still recorded. rollbacks counts Rollback calls, so a test
// can prove the tick never leaks the transaction that holds the resweep lock.
// queries records every statement the tick ran, in order, so a test can pin
// statement ordering (#10706 F1: the tick-clock read precedes admission).
type phaseFakeTx struct {
	claimed    bool
	consumed   int64
	identifier int64
	stored     int64
	delay      map[string]time.Duration
	failOn     string
	commitErr  error
	rollbacks  int
	queries    []string
}

func (tx *phaseFakeTx) step(query string) error {
	time.Sleep(tx.delay[query])
	if tx.failOn == query {
		return errors.New("scripted statement failure")
	}
	return nil
}

func (tx *phaseFakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if err := tx.step(query); err != nil {
		return nil, err
	}
	tx.queries = append(tx.queries, query)
	return phaseFakeResult{}, nil
}

func (tx *phaseFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if err := tx.step(query); err != nil {
		return nil, err
	}
	tx.queries = append(tx.queries, query)
	switch query {
	case tryClaimSupplyChainImpactWinnersResweepSQL:
		return &phaseFakeRows{row: []any{tx.claimed}}, nil
	case countWinnersKeyedConsumedSQL:
		return &phaseFakeRows{row: []any{tx.consumed}}, nil
	case selectWinnersStagingCollapseStatsSQL:
		// The fake queue always holds two staged rows under the test default
		// budget, so every tick exercises the bound and both merge arms.
		return &phaseFakeRows{row: []any{int64(2), int64(9), nil}}, nil
	case selectWinnersStagingCollapseBoundSQL:
		return &phaseFakeRows{row: []any{int64(7)}}, nil
	case collapseWinnersStagingFactSQL:
		return &phaseFakeRows{row: []any{int64(2), int64(2)}}, nil
	case collapseWinnersStagingScopeSQL:
		return &phaseFakeRows{row: []any{int64(0), int64(0)}}, nil
	case selectWinnersStagingResidualSQL:
		return &phaseFakeRows{row: []any{int64(0), nil}}, nil
	case selectWinnersKeyedChunkIdentsSQL:
		return &phaseFakeRows{row: []any{"pkg:npm/fake", "repo-fake", "", "CVE-2026-1", ""}}, nil
	case countWinnersKeyedActiveFactsSQL:
		return &phaseFakeRows{row: []any{int64(1)}}, nil
	case deleteWinnersKeyedAppliedDirtySQL, deleteWinnersKeyedAllDirtySQL:
		return &phaseFakeRows{row: []any{telemetry.SupplyChainImpactWinnersSourceActivation, tx.consumed}}, nil
	case countWinnersKeyedRemainingSQL:
		return &phaseFakeRows{row: []any{int64(0)}}, nil
	case countWinnersKeyedIdentsSQL:
		return &phaseFakeRows{row: []any{tx.identifier}}, nil
	case winnersKeyedStoredEstimateSQL:
		return &phaseFakeRows{row: []any{tx.stored}}, nil
	case stampWinnersWatermarkSQL:
		return &phaseFakeRows{row: []any{int64(0)}}, nil
	case selectWinnersKeyedTickClockSQL:
		return &phaseFakeRows{row: []any{phaseFakeNow}}, nil
	case selectWinnersKeyedRemainingMinSQL:
		return &phaseFakeRows{row: []any{nil}}, nil
	case selectWinnersWatermarkSQL:
		return &phaseFakeRows{row: []any{phaseFakeCommitted}}, nil
	case applyWinnersKeyedSQL:
		return &phaseFakeRows{row: []any{int64(1), int64(0), int64(0), int64(0), int64(1), int64(0)}}, nil
	case rebuildSupplyChainImpactWinnersSQL:
		return &phaseFakeRows{row: []any{int64(1), int64(0), int64(0), int64(0), int64(0)}}, nil
	}
	return nil, fmt.Errorf("unscripted query %q", query)
}

func (tx *phaseFakeTx) Commit() error { return tx.commitErr }

func (tx *phaseFakeTx) Rollback() error {
	tx.rollbacks++
	return nil
}

// phaseFakeNow is the scripted DB clock every fake keyed tick reads, and
// phaseFakeCommitted is the committed watermark the re-read observes. They
// deliberately differ, the way GREATEST keeps a previous watermark ahead of
// the stamp input, so a test can prove the result reports the committed
// value rather than echoing the stamp (#10706 F4). The phase tests pin
// statement shape and phase samples, not stamp values.
var phaseFakeNow = time.Date(2026, 11, 2, 9, 1, 0, 0, time.UTC)

// phaseFakeCommitted is the scripted committed watermark: ahead of the tick
// clock, the way a GREATEST-kept previous watermark survives a stamp.
var phaseFakeCommitted = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

// phaseFakeDB hands the one scripted transaction to ResweepWinnersKeyed, or
// fails Begin with beginErr.
type phaseFakeDB struct {
	tx       *phaseFakeTx
	beginErr error
}

func (phaseFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("keyed tick must read through its transaction")
}

func (d phaseFakeDB) Begin(context.Context) (db.Transaction, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

// tickPhaseSamples returns the tick-phase histogram's sample count and summed
// seconds by phase label.
func tickPhaseSamples(t *testing.T, reader *sdkmetric.ManualReader) (counts map[string]uint64, seconds map[string]float64) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts, seconds = map[string]uint64{}, map[string]float64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != "eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds" {
				continue
			}
			for _, dp := range hist.DataPoints {
				phase, _ := dp.Attributes.Value("phase")
				counts[phase.AsString()] += dp.Count
				seconds[phase.AsString()] += dp.Sum
				if got := dp.Attributes.Len(); got != 1 {
					t.Errorf("tick phase sample has %d attributes, want only phase", got)
				}
			}
		}
	}
	return counts, seconds
}

func newPhaseStore(t *testing.T, tx *phaseFakeTx) (SupplyChainImpactWinnersStore, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx}).WithInstruments(inst), reader
}

// TestResweepWinnersKeyedRecordsOneSamplePerPhase pins #9975: a keyed tick
// records one duration sample for each phase it ran, under a closed phase label,
// so an operator can tell a slow read from a slow write. The phases a tick did
// not reach record nothing.
func TestResweepWinnersKeyedRecordsOneSamplePerPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tx   phaseFakeTx
		want []string
	}{
		{
			name: "keyed apply",
			tx:   phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100},
			want: []string{"claim", "collapse", "consume", "build_idents", "build_active_facts", "apply", "commit"},
		},
		{
			name: "idle tick stamps the watermark",
			tx:   phaseFakeTx{claimed: true},
			want: []string{"claim", "collapse", "consume", "stamp", "commit"},
		},
		{
			name: "over the cap falls back to the full rebuild",
			tx:   phaseFakeTx{claimed: true, consumed: 3, identifier: defaultWinnersKeyedMaxIdentifiers + 1, stored: 1000000},
			want: []string{"claim", "collapse", "consume", "build_idents", "full_rebuild", "commit"},
		},
		{
			name: "lock held by another resweep",
			tx:   phaseFakeTx{claimed: false},
			want: []string{"claim"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := tt.tx
			store, reader := newPhaseStore(t, &tx)
			if _, err := store.ResweepWinnersKeyed(context.Background()); err != nil {
				t.Fatalf("ResweepWinnersKeyed() error = %v", err)
			}
			counts, _ := tickPhaseSamples(t, reader)
			want := map[string]uint64{}
			for _, phase := range tt.want {
				want[phase] = 1
			}
			if !reflect.DeepEqual(counts, want) {
				t.Errorf("tick phase samples = %v, want %v", counts, want)
			}
			// A tick that began a transaction rolls it back exactly once: after
			// a commit it is a no-op, and on a held lock it releases the claim.
			if tx.rollbacks != 1 {
				t.Errorf("Rollback calls = %d, want 1", tx.rollbacks)
			}
		})
	}
}

// TestResweepWinnersKeyedRecordsTheTimeOfAFailedPhase pins that a statement
// that fails still leaves its phase sample, with the time it spent, and that
// no later phase records anything. A tick that dies in the apply is the slow
// one an operator most needs to see.
func TestResweepWinnersKeyedRecordsTheTimeOfAFailedPhase(t *testing.T) {
	t.Parallel()

	const spent = 30 * time.Millisecond
	tests := []struct {
		name   string
		failOn string
		phase  string
		want   []string
		// idle scripts a tick that consumed nothing, so it stamps the
		// watermark; overCap scripts one whose identifier set is over the cap,
		// so it falls back to the full rebuild.
		idle, overCap bool
	}{
		{"claim", tryClaimSupplyChainImpactWinnersResweepSQL, "claim", []string{"claim"}, false, false},
		{"collapse", selectWinnersStagingCollapseStatsSQL, "collapse", []string{"claim", "collapse"}, false, false},
		{"consume", admitWinnersKeyedWindowSQL, "consume", []string{"claim", "collapse", "consume"}, false, false},
		{"build_idents", buildWinnersKeyedIdentsSQL, "build_idents", []string{"claim", "collapse", "consume", "build_idents"}, false, false},
		{"build_active_facts", addWinnersKeyedOperatorFactsSQL, "build_active_facts", []string{"claim", "collapse", "consume", "build_idents", "build_active_facts"}, false, false},
		{"apply", applyWinnersKeyedSQL, "apply", []string{"claim", "collapse", "consume", "build_idents", "build_active_facts", "apply"}, false, false},
		{"stamp", stampWinnersWatermarkSQL, "stamp", []string{"claim", "collapse", "consume", "stamp"}, true, false},
		{"full_rebuild", rebuildSupplyChainImpactWinnersSQL, "full_rebuild", []string{"claim", "collapse", "consume", "build_idents", "full_rebuild"}, false, true},
		{"commit", "", "commit", []string{"claim", "collapse", "consume", "build_idents", "build_active_facts", "apply", "commit"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &phaseFakeTx{claimed: true, consumed: 2, identifier: 2, stored: 100, failOn: tt.failOn}
			if tt.idle {
				tx.consumed = 0
			}
			if tt.overCap {
				tx.identifier, tx.stored = defaultWinnersKeyedMaxIdentifiers+1, 1000000
			}
			if tt.name == "commit" {
				tx.commitErr = errors.New("commit failed")
			}
			if tt.failOn != "" {
				tx.delay = map[string]time.Duration{tt.failOn: spent}
			}
			store, reader := newPhaseStore(t, tx)
			if _, err := store.ResweepWinnersKeyed(context.Background()); err == nil {
				t.Fatal("ResweepWinnersKeyed() error = nil, want the scripted failure")
			}
			counts, seconds := tickPhaseSamples(t, reader)
			want := map[string]uint64{}
			for _, phase := range tt.want {
				want[phase] = 1
			}
			if !reflect.DeepEqual(counts, want) {
				t.Errorf("tick phase samples = %v, want %v", counts, want)
			}
			if tt.failOn != "" && seconds[tt.phase] < spent.Seconds() {
				t.Errorf("failed phase %s recorded %.4f s, want at least %.4f s", tt.phase, seconds[tt.phase], spent.Seconds())
			}
			// A failure in any phase must still release the transaction, or it
			// would hold the resweep try-lock and a pooled connection.
			if tx.rollbacks != 1 {
				t.Errorf("Rollback calls = %d, want 1", tx.rollbacks)
			}
		})
	}
}

// TestResweepWinnersKeyedBeginFailureRollsBackNothing pins that a tick whose
// Begin fails owns no transaction: it records the claim time, returns the
// begin error, and never calls Rollback.
func TestResweepWinnersKeyedBeginFailureRollsBackNothing(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true}
	beginErr := errors.New("begin refused")
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	store := NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx, beginErr: beginErr}).WithInstruments(inst)
	if _, err := store.ResweepWinnersKeyed(context.Background()); !errors.Is(err, beginErr) {
		t.Fatalf("ResweepWinnersKeyed() error = %v, want %v", err, beginErr)
	}
	if tx.rollbacks != 0 {
		t.Errorf("Rollback calls = %d, want 0 when Begin failed", tx.rollbacks)
	}
	counts, _ := tickPhaseSamples(t, reader)
	if want := map[string]uint64{"claim": 1}; !reflect.DeepEqual(counts, want) {
		t.Errorf("tick phase samples = %v, want %v", counts, want)
	}
}

// TestResweepWinnersKeyedWithoutInstrumentsRunsUnchanged pins that a store
// with no instruments (every existing caller and test) behaves as before.
func TestResweepWinnersKeyedWithoutInstrumentsRunsUnchanged(t *testing.T) {
	t.Parallel()

	tx := &phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100}
	got, err := NewSupplyChainImpactWinnersStore(phaseFakeDB{tx: tx}).ResweepWinnersKeyed(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	if got.Path != telemetry.SupplyChainImpactWinnersPathKeyed || got.Identifiers != 3 || got.Inserted != 1 || got.Keys != 1 {
		t.Errorf("result = %+v, want a keyed apply of 3 identifiers", got)
	}
}

// TestResweepWinnersKeyedReadsTickClockBeforeAdmission pins the #10706 stamp
// ordering: one tick reads the DB-clock tick stamp exactly once, before it
// admits the dirty window, on the idle, drained-keyed and full-fallback paths
// alike. A row that commits mid-tick carries a later captured_at than any
// stamp the tick can write, so the stamp never covers a capture the tick did
// not see.
func TestResweepWinnersKeyedReadsTickClockBeforeAdmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tx   phaseFakeTx
	}{
		{"idle tick stamps S", phaseFakeTx{claimed: true}},
		{"drained keyed tick stamps S", phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100}},
		{"over-cap fallback stamps S", phaseFakeTx{claimed: true, consumed: 3, identifier: defaultWinnersKeyedMaxIdentifiers + 1, stored: 1000000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := tt.tx
			store, _ := newPhaseStore(t, &tx)
			if _, err := store.ResweepWinnersKeyed(context.Background()); err != nil {
				t.Fatalf("ResweepWinnersKeyed() error = %v", err)
			}
			var clockIdx, admitIdx, clockCount int
			clockIdx, admitIdx = -1, -1
			for i, q := range tx.queries {
				switch q {
				case selectWinnersKeyedTickClockSQL:
					clockCount++
					if clockIdx < 0 {
						clockIdx = i
					}
				case admitWinnersKeyedWindowSQL:
					if admitIdx < 0 {
						admitIdx = i
					}
				}
			}
			if clockCount != 1 {
				t.Errorf("tick clock reads = %d, want exactly one per tick", clockCount)
			}
			if clockIdx < 0 || admitIdx < 0 || clockIdx > admitIdx {
				t.Errorf("tick clock index = %d, admission index = %d, want the single stamp read before admission", clockIdx, admitIdx)
			}
		})
	}
}

// TestResweepWinnersKeyedReportsCommittedWatermark pins the #10706 F4
// contract: every branch reports the re-read committed watermark, never the
// stamp input. The fake scripts the committed watermark ahead of the tick
// clock, the way GREATEST keeps a previous watermark past a stamp, so a
// branch that echoes its stamp input disagrees with the database.
func TestResweepWinnersKeyedReportsCommittedWatermark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tx   phaseFakeTx
	}{
		{"idle tick", phaseFakeTx{claimed: true}},
		{"drained keyed tick", phaseFakeTx{claimed: true, consumed: 3, identifier: 3, stored: 100}},
		{"over-cap fallback", phaseFakeTx{claimed: true, consumed: 3, identifier: defaultWinnersKeyedMaxIdentifiers + 1, stored: 1000000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := tt.tx
			store, _ := newPhaseStore(t, &tx)
			got, err := store.ResweepWinnersKeyed(context.Background())
			if err != nil {
				t.Fatalf("ResweepWinnersKeyed() error = %v", err)
			}
			if !got.Watermark.Equal(phaseFakeCommitted) {
				t.Errorf("result watermark = %v, want the re-read committed watermark %v", got.Watermark, phaseFakeCommitted)
			}
		})
	}
}
