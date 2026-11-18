// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10851 conditions 1, 4 and 6: cross-backend resweep-claim mutual exclusion, the multi-batch writer-txn replay held open across a concurrent collapse-in-tick settle with zero 40P01 and exact conservation
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// winnersReplayConflictsTotal reads the #10851 write-conflict counter out of
// a manual reader: the wave-load measurement (condition 4) needs the counter
// at zero, not just no classified error on the test's own paths.
func winnersReplayConflictsTotal(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) int64 {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("collect test meter: %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_winners_write_conflicts_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("write-conflict counter has unexpected aggregation %T", m.Data)
			}
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	return total
}

// winnersReplayStore wires a keyed store to test instruments so the replay
// runs the production ResweepWinnersKeyed path — claim, collapse, settle,
// commit — with the #10851 counter hook armed.
func winnersReplayStore(t *testing.T, database *sql.DB) (SupplyChainImpactWinnersStore, *telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("eshu/winners-replay-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if instruments.SupplyChainImpactWinnersWriteConflicts == nil {
		t.Fatal("SupplyChainImpactWinnersWriteConflicts counter not registered")
	}
	store := SupplyChainImpactWinnersStore{DB: SQLDB{DB: database}}.WithInstruments(instruments)
	return store, instruments, reader
}

// requireConflictFree fails the replay on any error and separately on any
// error the #10851 classifier counts: a 40P01 or 40001 anywhere in the
// interleave is the gate's failure, even where the operation itself would
// tolerate it.
func requireConflictFree(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		return
	}
	if class := winnersWriteConflictClass(err); class != "" {
		t.Fatalf("%s hit a %s (%v): the replay needs zero 40P01 with no retry", what, class, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// TestSupplyChainImpactWinnersResweepClaimIsCrossBackendLive is the condition
// 1 proof: the resweep claim is a transaction-scoped Postgres advisory lock
// (pg_try_advisory_xact_lock), so it is mutual across backends, not just
// goroutines. While one backend holds the claim, a second backend's claim
// reports false; once the first transaction commits, the lock is free and a
// new claim succeeds. The code order in ResweepWinnersKeyed (claim, then the
// collapse, then the settle, one commit) keeps that same lock for the whole
// tick including the collapse.
func TestSupplyChainImpactWinnersResweepClaimIsCrossBackendLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	first, err := (SQLDB{DB: database}).Begin(ctx)
	requireConflictFree(t, err, "begin first claim transaction")
	defer func() { _ = first.Rollback() }()
	claimed, err := claimSupplyChainImpactWinnersResweep(ctx, first)
	requireConflictFree(t, err, "first resweep claim")
	if !claimed {
		t.Fatal("first resweep claim = false on an idle database, want the lock held")
	}

	// The second transaction runs on its own backend: the pool cannot hand
	// it the first transaction's busy connection.
	second, err := (SQLDB{DB: database}).Begin(ctx)
	requireConflictFree(t, err, "begin second claim transaction")
	defer func() { _ = second.Rollback() }()
	rival, err := claimSupplyChainImpactWinnersResweep(ctx, second)
	requireConflictFree(t, err, "rival resweep claim")
	if rival {
		t.Fatal("rival resweep claim = true while the first tick holds the lock: the claim is not cross-backend mutual")
	}
	if err := first.Commit(); err != nil {
		t.Fatalf("commit first claim transaction: %v", err)
	}
	// The xact lock died with the first commit: a fresh transaction claims.
	third, err := (SQLDB{DB: database}).Begin(ctx)
	requireConflictFree(t, err, "begin third claim transaction")
	defer func() { _ = third.Rollback() }()
	again, err := claimSupplyChainImpactWinnersResweep(ctx, third)
	requireConflictFree(t, err, "third resweep claim")
	if !again {
		t.Fatal("resweep claim = false after the holder committed: the lock did not release with the transaction")
	}
}

// TestSupplyChainImpactWinnersWriterTxnReplayLive is the condition 6 replay:
// a writer transaction staging a capture batch plus a retract batch over
// pre-existing dirty rows, held open across a concurrent collapse-in-tick
// settle, then committed. The gate needs zero 40P01 with no retry anywhere
// and exact conservation: every staged mark is either drained or still
// queued with its version, so drained version sums plus the residual log
// versions plus the staging residual equal the marks the test sent.
func TestSupplyChainImpactWinnersWriterTxnReplayLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)
	store, instruments, reader := winnersReplayStore(t, database)

	// Absorb bootstrap residue the way the landing replay does, so the
	// conservation below counts only this test's marks.
	residue, err := store.ResweepWinnersKeyed(ctx)
	requireConflictFree(t, err, "drain bootstrap residue")
	if residue.Skipped {
		t.Fatal("bootstrap-residue tick skipped: another resweep is in flight on a fresh database")
	}
	if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
		t.Fatalf("staging holds %d bootstrap rows after the residue drain", got)
	}

	// Pre-existing dirty rows: versioned log rows the writer batches land
	// on top of.
	const preExisting = 20
	var preSeed []string
	for i := 0; i < preExisting; i++ {
		preSeed = append(preSeed, fmt.Sprintf("pkg:replay-base:%03d", i))
	}
	var seedQuery strings.Builder
	seedQuery.WriteString(`INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id, dirty_version)
VALUES `)
	for i, pkg := range preSeed {
		if i > 0 {
			seedQuery.WriteString(", ")
		}
		fmt.Fprintf(&seedQuery, "('fact', 'fact_upsert', '%s', 'repo:replay', 'digest:replay', 'CVE-2026-0851', 'CVE-2026-0851', 1)", pkg)
	}
	if _, err := database.ExecContext(ctx, seedQuery.String()); err != nil {
		t.Fatalf("seed pre-existing dirty rows: %v", err)
	}
	var marks atomic.Int64
	marks.Add(preExisting)

	// The writer transaction: a capture batch of new keys plus a retract
	// batch over pre-existing keys and two of the new capture keys. The
	// retract batch overlaps committed log rows on purpose: under the
	// retired upsert shape this is the contended interleave.
	writer, err := (SQLDB{DB: database}).Begin(ctx)
	requireConflictFree(t, err, "begin writer transaction")
	wtx := &supplyChainImpactTx{tx: writer, instruments: instruments}
	const captureBatch = 30
	var captureIDs []supplychaincore.WinnersDirtyIdentifier
	for i := 0; i < captureBatch; i++ {
		pkg := fmt.Sprintf("pkg:replay-capture:%03d", i)
		captureIDs = append(captureIDs,
			supplychaincore.WinnersDirtyIdentifier{
				PackageID: pkg, RepositoryID: "repo:replay",
				SubjectDigest: "digest:replay",
				C0:            "CVE-2026-0851", AdvisoryID: "CVE-2026-0851",
			})
	}
	staged, err := wtx.CaptureWinnersDirtyFacts(ctx, captureIDs)
	requireConflictFree(t, err, "stage writer capture batch")
	marks.Add(staged)
	retractKeys := append(append([]string{}, preSeed[:8]...),
		"pkg:replay-capture:000", "pkg:replay-capture:001")
	var retractMarks int64
	for _, pkg := range retractKeys {
		res, err := writer.ExecContext(ctx, `INSERT INTO supply_chain_impact_winners_dirty_staging
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_retract', $1, 'repo:replay', 'digest:replay', 'CVE-2026-0851', 'CVE-2026-0851')`, pkg)
		requireConflictFree(t, err, "stage writer retract batch")
		n, err := res.RowsAffected()
		requireConflictFree(t, err, "count staged retract rows")
		retractMarks += n
	}
	wtx.RecordWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceFactRetract, retractMarks)
	marks.Add(retractMarks)

	// The collapse-in-tick settle runs while the writer transaction stays
	// open with uncommitted staged rows: the collapse must not see them
	// (READ COMMITTED) and must not wait on them (no unique index, no
	// arbiter), and the writer commit after it must not conflict.
	settleDone := make(chan supplychaincore.SupplyChainImpactWinnersResweep, 1)
	settleErr := make(chan error, 1)
	go func() {
		result, err := store.ResweepWinnersKeyed(ctx)
		if err != nil {
			settleErr <- err
			return
		}
		settleDone <- result
	}()
	var concurrent supplychaincore.SupplyChainImpactWinnersResweep
	select {
	case err := <-settleErr:
		_ = writer.Rollback()
		requireConflictFree(t, err, "concurrent collapse-in-tick settle")
	case concurrent = <-settleDone:
	case <-time.After(2 * time.Minute):
		_ = writer.Rollback()
		t.Fatal("concurrent collapse-in-tick settle did not finish while the writer transaction stayed open")
	}
	if concurrent.Skipped {
		_ = writer.Rollback()
		t.Fatal("concurrent settle skipped: it lost a claim nobody else should hold")
	}
	drained := concurrent.Identifiers

	requireConflictFree(t, writer.Commit(), "commit writer transaction")

	// Final drain through the production tick path, so every mark the test
	// sent is either tallied or still queued with its version.
	for i := 0; i < 20; i++ {
		result, err := store.ResweepWinnersKeyed(ctx)
		requireConflictFree(t, err, "final drain tick")
		if result.Skipped {
			t.Fatal("final drain tick skipped: another resweep is in flight on a test-owned database")
		}
		drained += result.Identifiers
		if result.Remaining == 0 {
			break
		}
		if i == 19 {
			t.Fatalf("log did not drain after 20 ticks, %d rows remain", result.Remaining)
		}
	}
	versions, depth := winnersLogVersionsOf(t, ctx, database)
	stagedLeft := winnersStagingDepthOf(t, ctx, database)
	if depth != 0 || versions != 0 || stagedLeft != 0 {
		t.Fatalf("residual holds log(depth=%d, versions=%d) plus staging=%d rows, want every table empty", depth, versions, stagedLeft)
	}
	if got, want := drained, marks.Load(); got != want {
		t.Fatalf("drained version sums = %d, want %d marks sent: marks were lost or double-counted", got, want)
	}
	if got := winnersReplayConflictsTotal(t, ctx, reader); got != 0 {
		t.Fatalf("write-conflict counter = %d, want zero 40P01/40001 across the replay", got)
	}
}
