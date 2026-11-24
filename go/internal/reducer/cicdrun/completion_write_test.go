// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// storedFact is one fact_records row the fake transaction returns.
type storedFact struct {
	tombstone bool
	payload   []byte
}

// verdictDB is a fake beginner whose transaction serves stored rows by fact id
// and records upserts, commit and rollback. Each err field fails that step.
type verdictDB struct {
	stored                                          map[string]storedFact
	beginErr, queryErr, rowsErr, execErr, commitErr error
	execs                                           int
	committed, rolledBack                           bool
	autocommitExecs                                 int
}

// ExecContext is the autocommit path; the verdict path must not use it.
func (d *verdictDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.autocommitExecs++
	return driverResult(1), nil
}

func (d *verdictDB) Begin(context.Context) (db.Transaction, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return verdictTx{d}, nil
}

type verdictTx struct{ d *verdictDB }

func (t verdictTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	if t.d.execErr != nil {
		return nil, t.d.execErr
	}
	t.d.execs++
	return driverResult(1), nil
}

func (t verdictTx) QueryContext(_ context.Context, _ string, args ...any) (db.Rows, error) {
	if t.d.queryErr != nil {
		return nil, t.d.queryErr
	}
	rows := &verdictRows{err: t.d.rowsErr}
	for _, id := range args[0].([]string) {
		if fact, ok := t.d.stored[id]; ok {
			rows.ids = append(rows.ids, id)
			rows.facts = append(rows.facts, fact)
		}
	}
	return rows, nil
}

func (t verdictTx) Commit() error {
	if t.d.commitErr != nil {
		return t.d.commitErr
	}
	t.d.committed = true
	return nil
}

func (t verdictTx) Rollback() error {
	if !t.d.committed {
		t.d.rolledBack = true
	}
	return nil
}

type verdictRows struct {
	ids   []string
	facts []storedFact
	next  int
	err   error
}

func (r *verdictRows) Next() bool { r.next++; return r.next <= len(r.ids) }

func (r *verdictRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.ids[r.next-1]
	*dest[1].(*bool) = r.facts[r.next-1].tombstone
	*dest[2].(*[]byte) = r.facts[r.next-1].payload
	return nil
}
func (r *verdictRows) Err() error   { return r.err }
func (r *verdictRows) Close() error { return nil }

type driverResult int64

func (r driverResult) LastInsertId() (int64, error) { return 0, nil }
func (r driverResult) RowsAffected() (int64, error) { return int64(r), nil }

// owedMarker records each recorder it builds and how often it ran.
type owedMarker struct {
	err       error
	workItems []string
	claimedAt []time.Time
	recorded  int
}

func (m *owedMarker) Recorder(workItemID string, claimedAt time.Time) CompletionOwedRecorder {
	m.workItems = append(m.workItems, workItemID)
	m.claimedAt = append(m.claimedAt, claimedAt)
	return owedRecorder{m}
}

type owedRecorder struct{ m *owedMarker }

func (r owedRecorder) RecordCompletionOwed(context.Context, factwrite.Execer) error {
	if r.m.err != nil {
		return r.m.err
	}
	r.m.recorded++
	return nil
}

var gateClaimedAt = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func gateWrite(environment string) CICDRunCorrelationWrite {
	return CICDRunCorrelationWrite{
		IntentID: "intent-1", ScopeID: "scope-1", GenerationID: "gen-1", SourceSystem: "ci_cd_run",
		ClaimedAt: gateClaimedAt,
		Decisions: []CICDRunCorrelationDecision{{
			Provider: "github_actions", RunID: "run-1", RunAttempt: "1", RepositoryID: "repository:r",
			ArtifactDigest: "sha256:a", ImageRef: "img:1", Environment: environment, Outcome: CICDRunCorrelationExact,
		}},
	}
}

// storedAs returns the stored rows a previous write of write would have left.
func storedAs(t *testing.T, write CICDRunCorrelationWrite) map[string]storedFact {
	t.Helper()
	stored := map[string]storedFact{}
	for _, decision := range write.Decisions {
		payload, err := json.Marshal(cicdRunCorrelationPayload(write, decision))
		if err != nil {
			t.Fatal(err)
		}
		stored[cicdRunCorrelationFactID(write, decision)] = storedFact{payload: payload}
	}
	return stored
}

func writeVerdict(t *testing.T, d *verdictDB, marker *owedMarker, write CICDRunCorrelationWrite) (CICDRunCorrelationWriteResult, error) {
	t.Helper()
	writer := PostgresCICDRunCorrelationWriter{DB: d, Beginner: d}
	if marker != nil {
		writer.CompletionOwed = marker
	}
	return writer.WriteCICDRunCorrelations(context.Background(), write)
}

// TestWriteWithVerdictRecordsTheMarkerOnlyForAChangedWrite drives the writer's
// transactional path: a write with no stored rows is changed, commits its rows
// and records the owed marker for its own item and claim; a rewrite of the same
// consumer truth from a later pass is unchanged and records no marker; a
// changed environment is changed again.
func TestWriteWithVerdictRecordsTheMarkerOnlyForAChangedWrite(t *testing.T) {
	t.Parallel()

	first := &verdictDB{}
	marker := &owedMarker{}
	result, err := writeVerdict(t, first, marker, gateWrite("prod"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if result.CompletionChanged == nil || !*result.CompletionChanged || !first.committed || first.execs == 0 || first.autocommitExecs != 0 {
		t.Fatalf("first write: changed=%v committed=%v execs=%d autocommit=%d, want changed rows committed in the transaction",
			result.CompletionChanged, first.committed, first.execs, first.autocommitExecs)
	}
	if marker.recorded != 1 || marker.workItems[0] != "intent-1" || !marker.claimedAt[0].Equal(gateClaimedAt) {
		t.Fatalf("marker = %+v, want one record for intent-1 at the claim", marker)
	}

	rewrite := gateWrite("prod")
	rewrite.IntentID, rewrite.Cause = "intent-2", "a later pass"
	again := &verdictDB{stored: storedAs(t, gateWrite("prod"))}
	marker = &owedMarker{}
	result, err = writeVerdict(t, again, marker, rewrite)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if *result.CompletionChanged || marker.recorded != 0 || !again.committed {
		t.Fatalf("rewrite: changed=%v marker=%d committed=%v, want unchanged, no marker, committed", *result.CompletionChanged, marker.recorded, again.committed)
	}

	moved := &verdictDB{stored: storedAs(t, gateWrite("prod"))}
	result, err = writeVerdict(t, moved, nil, gateWrite("staging"))
	if err != nil || !*result.CompletionChanged {
		t.Fatalf("environment change: changed=%v err=%v, want changed", result.CompletionChanged, err)
	}
}

// TestWriteWithVerdictRollsBackOnEveryFailure: any failed step fails the write
// and commits nothing, so no rows and no marker land without the other.
func TestWriteWithVerdictRollsBackOnEveryFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	cases := map[string]func() (*verdictDB, *owedMarker){
		"begin":       func() (*verdictDB, *owedMarker) { return &verdictDB{beginErr: boom}, nil },
		"read stored": func() (*verdictDB, *owedMarker) { return &verdictDB{queryErr: boom}, nil },
		"stored rows": func() (*verdictDB, *owedMarker) { return &verdictDB{rowsErr: boom}, nil },
		"upsert":      func() (*verdictDB, *owedMarker) { return &verdictDB{execErr: boom}, nil },
		"commit":      func() (*verdictDB, *owedMarker) { return &verdictDB{commitErr: boom}, nil },
		"lost lease":  func() (*verdictDB, *owedMarker) { return &verdictDB{}, &owedMarker{err: boom} },
		"corrupt stored": func() (*verdictDB, *owedMarker) {
			d := &verdictDB{stored: storedAs(t, gateWrite("prod"))}
			for id := range d.stored {
				d.stored[id] = storedFact{payload: []byte("{not json")}
			}
			return d, nil
		},
	}
	for name, build := range cases {
		d, marker := build()
		if _, err := writeVerdict(t, d, marker, gateWrite("prod")); err == nil {
			t.Fatalf("%s: write succeeded, want an error", name)
		}
		if d.committed {
			t.Fatalf("%s: transaction committed after a failure", name)
		}
	}
}

// TestWriteCICDRunCorrelationsRequiresADatabase: no database is an error, not
// a silent no-op.
func TestWriteCICDRunCorrelationsRequiresADatabase(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresCICDRunCorrelationWriter{}).WriteCICDRunCorrelations(context.Background(), gateWrite("prod")); err == nil {
		t.Fatal("write without a database succeeded")
	}
}

// TestCompletionSubSignalsCarryTheVerdictAndCountSuppression: a changed write
// carries 1, an unchanged one carries 0 and counts one suppressed event for
// the ci_cd domain, and a write without a verdict leaves the signals as they
// were so the ack emits.
func TestCompletionSubSignalsCarryTheVerdictAndCountSuppression(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	handler := CICDRunCorrelationHandler{Instruments: instruments}
	changed, unchanged := true, false
	ctx := context.Background()

	if got := handler.completionSubSignals(ctx, CICDRunCorrelationWriteResult{}, map[string]float64{"other": 2}); len(got) != 1 || got["other"] != 2 {
		t.Fatalf("no verdict: signals = %v, want them untouched", got)
	}
	if got := handler.completionSubSignals(ctx, CICDRunCorrelationWriteResult{CompletionChanged: &changed}, nil); got[CompletionChangedSignal] != 1 {
		t.Fatalf("changed: signals = %v, want %s=1", got, CompletionChangedSignal)
	}
	got := handler.completionSubSignals(ctx, CICDRunCorrelationWriteResult{CompletionChanged: &unchanged}, nil)
	if value, ok := got[CompletionChangedSignal]; !ok || value != 0 {
		t.Fatalf("unchanged: signals = %v, want %s=0", got, CompletionChangedSignal)
	}
	if !ShouldEmitCompletion(reducercontract.Result{SubSignals: map[string]float64{CompletionChangedSignal: 1}}) {
		t.Fatal("a changed signal must emit")
	}

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &resources); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if total := suppressedTotal(resources, string(reducercontract.DomainCICDRunCorrelation)); total != 1 {
		t.Fatalf("suppressed counter for ci_cd = %d, want 1 (only the unchanged pass)", total)
	}
	// A handler without instruments still sets the signal.
	if got := (CICDRunCorrelationHandler{}).completionSubSignals(ctx, CICDRunCorrelationWriteResult{CompletionChanged: &unchanged}, nil); got[CompletionChangedSignal] != 0 {
		t.Fatalf("no instruments: signals = %v", got)
	}
}

func suppressedTotal(resources metricdata.ResourceMetrics, domain string) int64 {
	var total int64
	for _, scope := range resources.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_cross_scope_completion_suppressed_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value("domain"); ok && value.AsString() == domain {
					total += point.Value
				}
			}
		}
	}
	return total
}

// TestClaimedAtOfReturnsTheClaimIdentity: the marker fences on the claim's
// last_attempt_at; an intent without a claim yields the zero time.
func TestClaimedAtOfReturnsTheClaimIdentity(t *testing.T) {
	t.Parallel()

	local := gateClaimedAt.In(time.FixedZone("x", 3600))
	if got := claimedAtOf(reducercontract.Intent{ClaimedAt: &local}); !got.Equal(gateClaimedAt) || got.Location() != time.UTC {
		t.Fatalf("claimedAtOf = %v, want %v in UTC", got, gateClaimedAt)
	}
	if got := claimedAtOf(reducercontract.Intent{}); !got.IsZero() {
		t.Fatalf("claimedAtOf(no claim) = %v, want zero", got)
	}
}
