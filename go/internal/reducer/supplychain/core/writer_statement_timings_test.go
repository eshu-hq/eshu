// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestWriteTxStatementLabelSetIsClosed pins the arbiter's closed per-statement
// label set for eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds:
// exactly these 13 statements, these spellings, no scope, generation, CVE,
// fact-id or repository label. A rename or an addition fails here first.
func TestWriteTxStatementLabelSetIsClosed(t *testing.T) {
	t.Parallel()

	got := []string{
		telemetry.SupplyChainImpactWriteTxStatementConflictLock,
		telemetry.SupplyChainImpactWriteTxStatementSuppressionDiff,
		telemetry.SupplyChainImpactWriteTxStatementAdmission,
		telemetry.SupplyChainImpactWriteTxStatementStoredRead,
		telemetry.SupplyChainImpactWriteTxStatementFactInsert,
		telemetry.SupplyChainImpactWriteTxStatementSupportsUpsert,
		telemetry.SupplyChainImpactWriteTxStatementWinnersCapture,
		telemetry.SupplyChainImpactWriteTxStatementRetract,
		telemetry.SupplyChainImpactWriteTxStatementSupportsDelete,
		telemetry.SupplyChainImpactWriteTxStatementRecoveryRead,
		telemetry.SupplyChainImpactWriteTxStatementPassState,
		telemetry.SupplyChainImpactWriteTxStatementGoEvidenceClose,
		telemetry.SupplyChainImpactWriteTxStatementCommit,
	}
	want := []string{
		"conflict_lock", "suppression_diff", "admission", "stored_read",
		"fact_insert", "supports_upsert", "winners_capture", "retract",
		"supports_delete", "recovery_read", "pass_state", "go_evidence_close",
		"commit",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("statement label set = %q, want %q", got, want)
	}
	if got := telemetry.SupplyChainImpactWriteTxStatementLabels(); !slices.Equal(got, want) {
		t.Fatalf("SupplyChainImpactWriteTxStatementLabels() = %q, want %q", got, want)
	}
}

// statementTimingTx wraps fakeImpactTx with the optional supports-maintainer
// and winners-capturer surfaces so the timed pass exercises every statement
// label. The stub calls are in-memory only: they record that the writer
// reached the statement, never metric or log IO.
type statementTimingTx struct {
	SupplyChainImpactTx
	upserts  int
	deletes  int
	captured int64
}

func (t *statementTimingTx) UpsertImpactSupports(_ context.Context, factIDs []string) (SupplyChainImpactSupportsWrite, error) {
	t.upserts++
	return SupplyChainImpactSupportsWrite{Inserted: int64(len(factIDs))}, nil
}

// UpsertImpactSupportRows counts the writer's row-based supports upsert (the
// PR-C2 path) and delegates to the embedded fake: the legacy fact-ID form
// above no longer runs in a pass.
func (t *statementTimingTx) UpsertImpactSupportRows(ctx context.Context, rows []factwrite.VersionedRow) (SupplyChainImpactSupportsWrite, error) {
	t.upserts++
	return t.SupplyChainImpactTx.(SupplyChainImpactSupportsMaintainer).UpsertImpactSupportRows(ctx, rows)
}

// RecordRetractedImpactSupports satisfies the maintainer surface without
// counting: the fake has no instruments (mirrors fakeImpactTx).
func (statementTimingTx) RecordRetractedImpactSupports(context.Context, int64) {}

func (t *statementTimingTx) DeleteRetractedImpactSupports(_ context.Context, _, _ string) (int64, error) {
	t.deletes++
	return 1, nil
}

func (t *statementTimingTx) CaptureWinnersDirtyFacts(_ context.Context, ids []WinnersDirtyIdentifier) (int64, error) {
	t.captured += int64(len(ids))
	return int64(len(ids)), nil
}

func (t *statementTimingTx) RecordWinnersCapture(_ context.Context, _ string, rows int64) {
	t.captured += rows
}

// statementTimingBeginner adapts fakeImpactBeginner the way newFakeImpactBeginner
// does, but the transaction also maintains supports and captures winners.
type statementTimingBeginner struct {
	inner fakeImpactBeginner
	last  *statementTimingTx
}

func (b *statementTimingBeginner) BeginSupplyChainImpactTx(ctx context.Context) (SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := &statementTimingTx{SupplyChainImpactTx: tx}
	b.last = wrapped
	return wrapped, nil
}

// timedFixtureRows builds n distinct rows plus the changed-row meta the writer
// would derive, so the pass captures winners keys for every row. It also
// builds the pre-stamp payload maps runSupplyChainImpactTx remarshals
// in-transaction (#11031); they carry no stamp keys.
func timedFixtureRows(n int) ([]factwrite.VersionedRow, []changedRow, []map[string]any) {
	rows := make([]factwrite.VersionedRow, 0, n)
	meta := make([]changedRow, 0, n)
	payloads := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		cve := fmt.Sprintf("CVE-2026-%d", i)
		rows = append(rows, factwrite.VersionedRow{FactID: fmt.Sprintf("fact-%d", i)})
		meta = append(meta, changedRow{
			factID: fmt.Sprintf("fact-%d", i),
			cveID:  cve,
			ident:  WinnersDirtyIdentifier{PackageID: "pkg", C0: cve},
		})
		payloads = append(payloads, map[string]any{})
	}
	return rows, meta, payloads
}

// TestCommittedPassAccumulatesOneSamplePerStatement proves the happy path:
// every entered statement region contributes exactly one accumulated sample,
// and the statement stream on the transaction is byte-identical to the untimed
// order (lock, admit, stored read, retract, recovery reads, pass state,
// go-evidence close). PR-C2 removed the chunked fact insert, so no
// fact_insert series fires; the fake still batches the supports rows
// internally (3 Execs on the inserts fake), which is untimed.
func TestCommittedPassAccumulatesOneSamplePerStatement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	inserts := &testutil.FakeExecer{}
	beginner := &statementTimingBeginner{inner: newFakeImpactBeginner(inserts)}
	beginner.inner.state.goEvidenceCurrent = 1
	write := retractTestWrite()
	write.Recovery = true
	write.GoEvidence = []GoEvidenceGeneration{{ScopeID: "go-scope", GenerationID: "go-gen"}}

	rows, meta, payloads := timedFixtureRows(2500)
	outcome, err := runSupplyChainImpactTx(ctx, beginner, write, rows, meta, payloads, ProtectedScanImpactPublicationSet{}, false, time.Now())
	if err != nil {
		t.Fatalf("runSupplyChainImpactTx() error = %v", err)
	}
	if got, want := len(inserts.Execs), 3; got != want {
		t.Fatalf("batched insert Exec calls = %d, want %d (2500 rows in 1000-row chunks)", got, want)
	}
	if beginner.last.upserts != 1 || beginner.last.deletes != 0 {
		t.Fatalf("supports upserts=%d deletes=%d, want 1/0 (the PR-C2 retraction deletes supports itself; DeleteRetractedImpactSupports no longer runs in a pass)", beginner.last.upserts, beginner.last.deletes)
	}
	if beginner.last.captured == 0 {
		t.Fatal("winners capture ran zero rows, want the pass's dirty keys")
	}

	emitted := outcome.statements.toTelemetry()
	byStatement := make(map[string]telemetry.SupplyChainImpactWriteTxStatementTiming, len(emitted))
	for _, sample := range emitted {
		if _, dup := byStatement[sample.Statement]; dup {
			t.Fatalf("duplicate series for statement %q: one pass emits one series per statement", sample.Statement)
		}
		byStatement[sample.Statement] = sample
		if sample.Seconds < 0 {
			t.Fatalf("statement %q has negative duration %v", sample.Statement, sample.Seconds)
		}
	}
	wantPresent := []string{
		"conflict_lock", "suppression_diff", "admission", "stored_read",
		"supports_upsert", "winners_capture", "retract",
		"recovery_read", "pass_state", "go_evidence_close",
		"commit",
	}
	for _, statement := range wantPresent {
		if _, ok := byStatement[statement]; !ok {
			t.Fatalf("no sample for entered statement %q (got %v)", statement, emitted)
		}
	}
	// PR-C2 removed the fact bulk insert and folded the supports delete into
	// the retraction CTE: neither series may fire. Their absence is the
	// write-stop assertion at the timings level, read through the same
	// accessor the accumulation used.
	for _, gone := range []supplyChainImpactTxStatement{supplyChainImpactTxStmtFactInsert, supplyChainImpactTxStmtSupportsDelete} {
		if _, _, _, ok := outcome.statements.sample(gone); ok {
			t.Fatalf("sample for removed statement %v, want none after the PR-C2 write stop", gone)
		}
	}

	all := beginner.inner.state.all
	wantQueries := []string{
		lockSupplyChainImpactConflictDomainQuery,
		supplyChainImpactAdmissionQuery,
		listStoredSupplyChainImpactFindingsQuery,
		"", // index 3: the supports upsert (Query-side unnest; SQL text lives
		// unexported in postgres, asserted by content below, not by constant)
		retractSupersededSupplyChainImpactFindingsQuery,
		listSupplyChainImpactCVEIDsQuery,
		listSupplyChainImpactGroupIDsQuery,
		supplyChainImpactScopePassStateQuery,
		SupplyChainImpactGoEvidenceSharedLockQuery,
		supplyChainImpactGoEvidenceRecheckQuery,
	}
	if len(all) != len(wantQueries) {
		t.Fatalf("statements = %d, want %d (%v)", len(all), len(wantQueries), all)
	}
	if q := all[3].Query; q != fakeSupportsUpsertQuery {
		t.Fatalf("statement 3 = %q, want the fake supports-upsert marker: timing must not move, add, or drop a statement", q)
	}
	for i, want := range wantQueries {
		if want == "" {
			continue
		}
		if all[i].Query != want {
			t.Fatalf("statement %d = %q, want %q: timing must not move, add, or drop a statement", i, all[i].Query, want)
		}
	}
}

// TestRolledBackPassKeepsPartialStatementTimings proves a pass that dies at the
// retraction keeps the samples of the statements that ran (the failed
// retraction included: its duration explains the rollback), records nothing
// for the statements it never reached (supports_delete, recovery, pass_state,
// go-evidence close, commit), and maps every error — including a
// go-evidence recheck abort — to the rolled_back statement outcome.
func TestRolledBackPassKeepsPartialStatementTimings(t *testing.T) {
	t.Parallel()

	if got := supplyChainImpactWriteTxStatementOutcome(nil); got != "committed" {
		t.Fatalf("statement outcome for nil error = %q, want committed", got)
	}
	if got := supplyChainImpactWriteTxStatementOutcome(fmt.Errorf("boom")); got != "rolled_back" {
		t.Fatalf("statement outcome for a failure = %q, want rolled_back", got)
	}
	if got := supplyChainImpactWriteTxStatementOutcome(&SupplyChainImpactGoEvidenceChangedError{Cause: GoEvidenceCauseGenerationChanged}); got != "rolled_back" {
		t.Fatalf("statement outcome for a recheck abort = %q, want rolled_back (the pass did roll back)", got)
	}

	ctx := context.Background()
	inserts := &testutil.FakeExecer{}
	beginner := &statementTimingBeginner{inner: newFakeImpactBeginner(inserts)}
	beginner.inner.state.failOn = retractSupersededSupplyChainImpactFindingsQuery
	write := retractTestWrite()

	rows, meta, payloads := timedFixtureRows(10)
	outcome, err := runSupplyChainImpactTx(ctx, beginner, write, rows, meta, payloads, ProtectedScanImpactPublicationSet{}, false, time.Now())
	if err == nil {
		t.Fatal("runSupplyChainImpactTx() error = nil, want the injected retraction failure")
	}
	emitted := outcome.statements.toTelemetry()
	present := make(map[string]bool, len(emitted))
	for _, sample := range emitted {
		present[sample.Statement] = true
	}
	// The retraction ran and failed, so its sample stays: the failed
	// statement's duration is what explains the rollback. PR-C2 removed the
	// fact bulk insert, so fact_insert joins the never-reached set.
	for _, statement := range []string{"conflict_lock", "suppression_diff", "admission", "stored_read", "supports_upsert", "winners_capture", "retract"} {
		if !present[statement] {
			t.Fatalf("rolled-back pass dropped the sample for %q, want the statements that ran", statement)
		}
	}
	for _, statement := range []string{"fact_insert", "supports_delete", "recovery_read", "pass_state", "go_evidence_close", "commit"} {
		if present[statement] {
			t.Fatalf("rolled-back pass records %q, want nothing for statements never reached", statement)
		}
	}
}

// statementPoints collects the statement histogram from reader: per-statement
// counts by outcome. Every point must carry exactly the statement and outcome
// labels, or the closed set is broken.
func statementPoints(t *testing.T, reader *sdkmetric.ManualReader) map[string]map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]map[string]uint64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds" {
				continue
			}
			for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				if point.Attributes.Len() != 2 {
					t.Fatalf("statement point has %d attributes, want exactly statement and outcome", point.Attributes.Len())
				}
				statement, ok := point.Attributes.Value("statement")
				if !ok {
					t.Fatal("statement point has no statement label")
				}
				outcome, ok := point.Attributes.Value("outcome")
				if !ok {
					t.Fatal("statement point has no outcome label")
				}
				if got[statement.AsString()] == nil {
					got[statement.AsString()] = map[string]uint64{}
				}
				got[statement.AsString()][outcome.AsString()] += point.Count
			}
		}
	}
	return got
}

// statementTestInstruments wires an Instruments whose statement histogram
// records into reader, mirroring the production registration.
func statementTestInstruments(t *testing.T, reader *sdkmetric.ManualReader) *telemetry.Instruments {
	t.Helper()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	hist, err := meter.Float64Histogram("eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds")
	if err != nil {
		t.Fatalf("histogram: %v", err)
	}
	return &telemetry.Instruments{SupplyChainImpactWriteTxStatementDuration: hist}
}

// TestWriterEmitsStatementTimingsOnCommitAndRollback proves the emission the
// arbiter requires: a committed recovery pass emits one observation per entered
// statement under outcome=committed (the commit sample included, proving the
// emission ran after the commit returned), and a pass that dies at the
// pass-state fence emits the groups that ran under outcome=rolled_back with no
// commit sample.
func TestWriterEmitsStatementTimingsOnCommitAndRollback(t *testing.T) {
	t.Parallel()

	committedReader := sdkmetric.NewManualReader()
	committedInst := statementTestInstruments(t, committedReader)
	committedWriter := PostgresSupplyChainImpactWriter{
		DB:          newFakeImpactBeginner(&testutil.FakeExecer{}),
		Instruments: committedInst,
	}
	committedWrite := retractTestWrite()
	committedWrite.Recovery = true
	if _, err := committedWriter.WriteSupplyChainImpactFindings(context.Background(), committedWrite); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	committed := statementPoints(t, committedReader)
	wantCommitted := []string{
		"conflict_lock", "suppression_diff", "admission", "stored_read",
		"supports_upsert", "retract", "recovery_read", "pass_state",
		"go_evidence_close", "commit",
	}
	if len(committed) != len(wantCommitted) {
		t.Fatalf("committed statements = %v, want %v (plain fake: no supports maintainer, no winners capturer)", committed, wantCommitted)
	}
	for _, statement := range wantCommitted {
		byOutcome, ok := committed[statement]
		if !ok {
			t.Fatalf("no committed sample for %q", statement)
		}
		if len(byOutcome) != 1 || byOutcome["committed"] != 1 {
			t.Fatalf("statement %q outcomes = %v, want exactly one committed observation", statement, byOutcome)
		}
	}

	rolledReader := sdkmetric.NewManualReader()
	rolledInst := statementTestInstruments(t, rolledReader)
	rolledBeginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	rolledBeginner.state.failOn = supplyChainImpactScopePassStateQuery
	rolledWriter := PostgresSupplyChainImpactWriter{DB: rolledBeginner, Instruments: rolledInst}
	rolledWrite := retractTestWrite()
	rolledWrite.Recovery = true
	if _, err := rolledWriter.WriteSupplyChainImpactFindings(context.Background(), rolledWrite); err == nil {
		t.Fatal("WriteSupplyChainImpactFindings() error = nil, want the injected pass-state failure")
	}
	rolled := statementPoints(t, rolledReader)
	for statement, byOutcome := range rolled {
		if len(byOutcome) != 1 || byOutcome["rolled_back"] != 1 {
			t.Fatalf("statement %q outcomes = %v, want exactly one rolled_back observation", statement, byOutcome)
		}
	}
	for _, statement := range []string{"conflict_lock", "suppression_diff", "admission", "stored_read", "supports_upsert", "retract", "recovery_read", "pass_state"} {
		if _, ok := rolled[statement]; !ok {
			t.Fatalf("no rolled_back sample for entered statement %q", statement)
		}
	}
	for _, statement := range []string{"fact_insert", "winners_capture", "supports_delete", "go_evidence_close", "commit"} {
		if _, ok := rolled[statement]; ok {
			t.Fatalf("rolled_back sample for %q, which the pass never reached", statement)
		}
	}
	if rolledBeginner.state.commits != 0 || rolledBeginner.state.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d, want 0/1", rolledBeginner.state.commits, rolledBeginner.state.rollbacks)
	}
}

// TestWriterWithNilInstrumentsRecordsNothing proves the nil path the existing
// hermetic tests rely on: a committed pass with no Instruments succeeds and,
// with nothing to record into, emits nothing.
func TestWriterWithNilInstrumentsRecordsNothing(t *testing.T) {
	t.Parallel()

	writer := PostgresSupplyChainImpactWriter{DB: newFakeImpactBeginner(&testutil.FakeExecer{})}
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), retractTestWrite()); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
}

// TestWriteTxStatementInstrumentationOverhead bounds the cost the arbiter
// budgets: a full fixture pass with the instrumentation recording against a
// real SDK meter must stay under 1 percent of the measured production tx wall.
// The reference is this lane's own QA measurement, p50 5.2 s for
// write_findings (docs/internal/evidence/11430-write-findings-p95-decomposition.md),
// so the per-pass budget is 52 ms. The measured overhead is the accumulation
// (clock reads and struct stores, which run inside the tx) plus the emission
// (which runs after the commit or rollback returns, never inside the tx).
func TestWriteTxStatementInstrumentationOverhead(t *testing.T) {
	t.Parallel()

	const runs = 11
	// referenceTxWall is the measured QA p50 write_findings wall this lane
	// convicted (see the decomposition doc above): the overhead budget is 1
	// percent of a production pass, not of the microsecond hermetic fixture.
	const referenceTxWall = 5.2 * float64(time.Second)
	const budget = referenceTxWall / 100

	run := func(inst *telemetry.Instruments) (time.Duration, uint64) {
		reader := sdkmetric.NewManualReader()
		if inst != nil {
			meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
			hist, err := meter.Float64Histogram("eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds")
			if err != nil {
				t.Fatalf("histogram: %v", err)
			}
			inst.SupplyChainImpactWriteTxStatementDuration = hist
		}
		write := retractTestWrite()
		write.Recovery = true
		walls := make([]time.Duration, 0, runs)
		for i := 0; i < runs; i++ {
			writer := PostgresSupplyChainImpactWriter{DB: newFakeImpactBeginner(&testutil.FakeExecer{}), Instruments: inst}
			started := time.Now()
			if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), write); err != nil {
				t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
			}
			walls = append(walls, time.Since(started))
		}
		slices.Sort(walls)
		var points uint64
		if inst != nil {
			for _, byOutcome := range statementPoints(t, reader) {
				for _, count := range byOutcome {
					points += count
				}
			}
		}
		return walls[runs/2], points
	}

	medianOn, points := run(&telemetry.Instruments{})
	if points == 0 {
		t.Fatal("instrumented fixture emitted no statement points: the overhead comparison measured nothing")
	}
	medianOff, _ := run(nil)
	overhead := medianOn - medianOff
	if medianOff > medianOn {
		overhead = 0
	}
	t.Logf("fixture pass wall: on=%v off=%v overhead=%v (budget %v = 1%% of %.1f s production p50; %d statement points emitted)",
		medianOn, medianOff, overhead, time.Duration(budget), referenceTxWall/float64(time.Second), points)
	if float64(overhead) > budget {
		t.Fatalf("instrumentation overhead = %v, over the %v budget (1%% of the measured production tx wall)", overhead, time.Duration(budget))
	}
	if float64(overhead) > referenceTxWall/10 || medianOn > time.Minute {
		t.Fatalf("instrumentation overhead = %v (wall %v), over the promotion stop (10%% or 60 s)", overhead, medianOn)
	}
}
