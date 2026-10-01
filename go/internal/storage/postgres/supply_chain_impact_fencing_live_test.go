// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Issue #7142: a stale impact pass (a worker that read its evidence early and
// commits after a fresher pass) must not retract or overwrite the fresher pass's
// finding set, and must not add rows the fresher pass did not derive. These
// proofs run the real handler, the real writer and the real Postgres sequence.

// fencingLoader serves one fixed evidence set and can hold a pass inside its
// evidence load, so a test can make an older-token pass finish last.
type fencingLoader struct {
	envelopes []facts.Envelope
	gate      chan struct{}
	entered   chan struct{}
	once      sync.Once
}

func (l *fencingLoader) ListFacts(ctx context.Context, _, _ string) ([]facts.Envelope, error) {
	if l.entered != nil {
		l.once.Do(func() { close(l.entered) })
	}
	if l.gate != nil {
		select {
		case <-l.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return append([]facts.Envelope(nil), l.envelopes...), nil
}

// fencingEvidence derives exactly one finding, anchored to repositoryID: an npm
// package a repository consumes at an affected version.
func fencingEvidence(repositoryID string) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID: "cve:" + repositoryID, FactKind: facts.VulnerabilityCVEFactKind,
			Payload: map[string]any{"cve_id": replaceSetLiveCVE, "advisory_id": replaceSetLiveCVE, "cvss_score": 8.1, "aliases": []any{replaceSetLiveCVE}},
		},
		{
			FactID: "affected:" + repositoryID, FactKind: facts.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id": replaceSetLiveCVE, "advisory_id": replaceSetLiveCVE, "package_id": "pkg:npm/example", "ecosystem": "npm",
				"package_name": "example", "affected_versions": []any{"1.2.3"}, "fixed_versions": []any{"1.3.0"},
			},
		},
		{
			FactID: "consume:" + repositoryID, FactKind: "reducer_package_consumption_correlation",
			Payload: map[string]any{
				"package_id": "pkg:npm/example", "relationship_kind": "consumption", "repository_id": repositoryID,
				"dependency_range": "1.2.3", "canonical_writes": 1, "evidence_fact_ids": []any{"lock-1"},
			},
		},
	}
}

func fencingHandler(db *sql.DB, loader *fencingLoader, inst *telemetry.Instruments, logs *bytes.Buffer) reducer.SupplyChainImpactHandler {
	handler := reducer.SupplyChainImpactHandler{
		FactLoader:         loader,
		Writer:             replaceSetWriter(db),
		FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
		Instruments:        inst,
		Now:                func() time.Time { return replaceSetLiveNow },
	}
	if logs != nil {
		handler.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	return handler
}

func fencingIntent(id string) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: id, ScopeID: replaceSetLiveScope, GenerationID: replaceSetLiveGeneration,
		SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact, Cause: "synthetic #7142 fencing proof",
	}
}

func fencingAdmissionToken(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()
	var token int64
	if err := db.QueryRowContext(ctx,
		`SELECT fencing_token FROM supply_chain_impact_write_admission WHERE scope_id = $1 AND generation_id = $2`,
		replaceSetLiveScope, replaceSetLiveGeneration).Scan(&token); err != nil {
		t.Fatalf("read admission watermark: %v", err)
	}
	return token
}

func isSuperseded(err error) bool {
	var classified interface{ FailureClass() string }
	return errors.As(err, &classified) && classified.FailureClass() == reducer.SupplyChainImpactWriteSupersededFailureClass
}

func supersededCounter(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_write_superseded_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// TestSupplyChainImpactStalePassResumingAfterReclaimCannotRetractFresherSetLive
// is the #7142 goal proof. Pass A reads its evidence first (and holds the older
// token), then stalls; its item is reclaimed and pass B, with fresher evidence
// and a fresher token, runs to completion and commits. A then resumes and
// finishes LAST. A must be rejected whole: no retraction of B's rows and no
// insertion of A's own rows.
func TestSupplyChainImpactStalePassResumingAfterReclaimCannotRetractFresherSetLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	const repoA, repoB = "repository:r_7142_a", "repository:r_7142_b"

	gate, entered := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	loaderA := &fencingLoader{envelopes: fencingEvidence(repoA), gate: gate, entered: entered}
	logsA := &bytes.Buffer{}
	handlerA := fencingHandler(db, loaderA, inst, logsA)

	aDone := make(chan error, 1)
	go func() {
		_, err := handlerA.Handle(ctx, fencingIntent("intent:7142:stale-a"))
		aDone <- err
	}()
	// A has issued its token (before the load) and is now parked inside it.
	<-entered

	// B: a fresher token, fresher evidence, runs to completion.
	if _, err := fencingHandler(db, &fencingLoader{envelopes: fencingEvidence(repoB)}, nil, nil).
		Handle(ctx, fencingIntent("intent:7142:fresh-b")); err != nil {
		t.Fatalf("fresher pass B: %v", err)
	}
	if active := replaceSetActiveRepositories(t, ctx, db); !slices.Equal(active, []string{repoB}) {
		t.Fatalf("active set after B = %q, want only B's finding", active)
	}
	tokenB := fencingAdmissionToken(t, ctx, db)

	release()
	errA := <-aDone
	if !isSuperseded(errA) {
		t.Fatalf("stale pass A returned %v, want the retryable superseded rejection", errA)
	}
	if active := replaceSetActiveRepositories(t, ctx, db); !slices.Equal(active, []string{repoB}) {
		t.Fatalf("active set after the stale pass finished last = %q, want B's finding untouched", active)
	}
	var aRows int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records WHERE fact_kind = $1 AND payload->>'repository_id' = $2`,
		facts.ReducerSupplyChainImpactFindingFactKind, repoA).Scan(&aRows); err != nil {
		t.Fatalf("count A rows: %v", err)
	}
	if aRows != 0 {
		t.Fatalf("stale pass A left %d rows behind; a rejected pass must write nothing", aRows)
	}
	if got := fencingAdmissionToken(t, ctx, db); got != tokenB {
		t.Fatalf("admission watermark = %d, want B's token %d (a rejected pass must not lower or move it)", got, tokenB)
	}
	if got := supersededCounter(t, reader); got != 1 {
		t.Fatalf("eshu_dp_supply_chain_impact_write_superseded_total = %d, want 1", got)
	}
	if !bytes.Contains(logsA.Bytes(), []byte("supply chain impact write superseded")) {
		t.Fatalf("no superseded WARN line: %s", logsA.String())
	}
}

// TestSupplyChainImpactEmptyFresherPassStillFencesOlderPassLive separates the
// admission table from a MAX(fencing_token) over fact_records: a fresher pass
// that derives an EMPTY finding set writes no row to take a maximum over, so a
// MAX-based fence would admit the stale pass and publish findings the fresher
// evidence says do not exist. The watermark row is written by every admitted
// pass.
func TestSupplyChainImpactEmptyFresherPassStillFencesOlderPassLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	gate, entered := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	handlerA := fencingHandler(db, &fencingLoader{envelopes: fencingEvidence("repository:r_7142_a"), gate: gate, entered: entered}, nil, nil)
	aDone := make(chan error, 1)
	go func() {
		_, err := handlerA.Handle(ctx, fencingIntent("intent:7142:stale-a"))
		aDone <- err
	}()
	<-entered

	// B derives nothing: no findings, so no fact_records row.
	if _, err := fencingHandler(db, &fencingLoader{}, nil, nil).Handle(ctx, fencingIntent("intent:7142:empty-b")); err != nil {
		t.Fatalf("empty fresher pass B: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = $1`,
		facts.ReducerSupplyChainImpactFindingFactKind).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("finding rows after the empty pass = %d (err %v), want 0: the fence cannot rely on a row", rows, err)
	}

	release()
	if err := <-aDone; !isSuperseded(err) {
		t.Fatalf("stale pass A returned %v, want the superseded rejection", err)
	}
	if active := replaceSetActiveRepositories(t, ctx, db); len(active) != 0 {
		t.Fatalf("active set = %q, want empty: the stale pass published findings the fresher evidence does not have", active)
	}
}

// TestSupplyChainImpactRetractsLegacyZeroRowsAndStampsTokenLive pins the rolling
// deploy state: finding rows written before the token existed carry 0. A pass
// with a token updates a legacy row it derives (stamping its token) and retracts
// a legacy row it does not, and the tombstone records the retiring pass.
func TestSupplyChainImpactRetractsLegacyZeroRowsAndStampsTokenLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	derived := replaceSetWrite("intent:7142:legacy-derived", replaceSetFinding(replaceSetLiveRepository))
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, derived); err != nil {
		t.Fatalf("seed derived finding: %v", err)
	}
	replaceSetPlantRow(t, ctx, db, "legacy-undelivered", replaceSetLiveScope, replaceSetLiveGeneration,
		facts.ReducerSupplyChainImpactFindingFactKind, 0)
	// Both rows now look like pre-token rows.
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET fencing_token = 0 WHERE fact_kind = $1`,
		facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("reset to legacy tokens: %v", err)
	}

	pass := replaceSetWrite("intent:7142:legacy-pass", replaceSetFinding(replaceSetLiveRepository))
	result, err := writer.WriteSupplyChainImpactFindings(ctx, pass)
	if err != nil {
		t.Fatalf("token pass: %v", err)
	}
	if result.FactsRetracted != 1 {
		t.Fatalf("FactsRetracted = %d, want 1 (the legacy row the pass does not derive)", result.FactsRetracted)
	}
	var retiredToken, derivedToken int64
	if err := db.QueryRowContext(ctx, `SELECT fencing_token FROM fact_records WHERE fact_id = 'legacy-undelivered' AND is_tombstone`).Scan(&retiredToken); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if retiredToken != pass.FencingToken {
		t.Fatalf("tombstone token = %d, want the retiring pass's token %d", retiredToken, pass.FencingToken)
	}
	if err := db.QueryRowContext(ctx, `SELECT fencing_token FROM fact_records WHERE fact_kind = $1 AND NOT is_tombstone`,
		facts.ReducerSupplyChainImpactFindingFactKind).Scan(&derivedToken); err != nil {
		t.Fatalf("read derived row: %v", err)
	}
	if derivedToken != pass.FencingToken {
		t.Fatalf("derived legacy row token = %d, want %d (updated by the fresher pass)", derivedToken, pass.FencingToken)
	}
}

// TestSupplyChainImpactUpsertGuardRefusesAStaleReviveLive is the defense in
// depth behind the admission: even a write that bypasses it cannot revive a row
// a fresher pass tombstoned, because the retraction stamps its token on the
// tombstone and the upsert applies only when existing <= incoming.
func TestSupplyChainImpactUpsertGuardRefusesAStaleReviveLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	older := replaceSetWrite("intent:7142:older", replaceSetFinding(replaceSetLiveRepository))
	fresher := replaceSetWrite("intent:7142:fresher", replaceSetFinding("repository:r_7142_other"))
	// The older pass wrote the row; the fresher pass retracted it.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, older); err != nil {
		t.Fatalf("older write: %v", err)
	}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, fresher); err != nil {
		t.Fatalf("fresher write: %v", err)
	}
	var factID string
	if err := db.QueryRowContext(ctx, `SELECT fact_id FROM fact_records WHERE is_tombstone AND fact_kind = $1`,
		facts.ReducerSupplyChainImpactFindingFactKind).Scan(&factID); err != nil {
		t.Fatalf("find the retracted row: %v", err)
	}

	// Bypass the admission: replay the older pass's upsert straight into the
	// batched writer with its older token.
	row := factwrite.VersionedRow{
		FactID: factID, ScopeID: replaceSetLiveScope, GenerationID: replaceSetLiveGeneration,
		FactKind: facts.ReducerSupplyChainImpactFindingFactKind, StableFactKey: factID, SchemaVersion: facts.ReducerDerivedSchemaVersionV1,
		CollectorKind: "reducer", SourceConfidence: facts.SourceConfidenceInferred, SourceSystem: "vulnerability_intelligence",
		SourceFactKey: "intent:7142:older", ObservedAt: replaceSetLiveNow, IngestedAt: replaceSetLiveNow,
		Payload: "{}", FencingToken: older.FencingToken,
	}
	if err := factwrite.BatchInsertVersionedFacts(ctx, postgres.SQLDB{DB: db}, []factwrite.VersionedRow{row}); err != nil {
		t.Fatalf("stale replay: %v", err)
	}
	if !replaceSetIsTombstone(t, ctx, db, factID) {
		t.Fatal("a stale replay revived a row a fresher pass tombstoned")
	}
}

// TestSupplyChainImpactFencingTokenIssuerIssuesStrictlyIncreasingValuesLive
// proves the sequence-backed issuer never repeats or reorders a value under
// concurrent callers, and that running migration 154's seed statement by hand
// (the repair an operator uses after resetting the sequence; the bootstrap
// ledger runs the file once in the service runtimes, and eshu local runs it on
// every start) never regresses the sequence below an admitted
// watermark.
func TestSupplyChainImpactFencingTokenIssuerIssuesStrictlyIncreasingValuesLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	issuer := postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}}

	const callers, each = 8, 25
	var (
		mu     sync.Mutex
		tokens []int64
		wg     sync.WaitGroup
	)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last int64
			for range each {
				token, err := issuer.NextSupplyChainImpactFencingToken(ctx)
				if err != nil {
					t.Errorf("issue token: %v", err)
					return
				}
				if token <= last {
					t.Errorf("a caller saw %d after %d: values must strictly increase", token, last)
				}
				last = token
				mu.Lock()
				tokens = append(tokens, token)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	slices.Sort(tokens)
	if len(slices.Compact(slices.Clone(tokens))) != callers*each {
		t.Fatalf("issued %d distinct tokens of %d; a value was issued twice", len(slices.Compact(slices.Clone(tokens))), callers*each)
	}
	if tokens[0] < 1 {
		t.Fatalf("first token = %d; a sequence never issues 0", tokens[0])
	}

	// An admitted watermark above the sequence must survive running the seed by
	// hand: the seed advances the sequence past it and never regresses it.
	const watermark = 1_000_000
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_write_admission (scope_id, generation_id, fencing_token, updated_at)
VALUES ('scope:seed', 'gen:seed', $1, $2)`, watermark, replaceSetLiveNow); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	var migration string
	for _, definition := range postgres.BootstrapDefinitions() {
		if definition.Name == "supply_chain_impact_write_admission" {
			migration = definition.SQL
		}
	}
	if migration == "" {
		t.Fatal("bootstrap has no supply_chain_impact_write_admission definition")
	}
	for range 2 { // idempotent: an operator may run the seed more than once
		if _, err := db.ExecContext(ctx, migration); err != nil {
			t.Fatalf("re-run the seed: %v", err)
		}
	}
	next, err := issuer.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		t.Fatalf("issue after re-run: %v", err)
	}
	if next <= watermark {
		t.Fatalf("next token = %d, want above the admitted watermark %d", next, watermark)
	}
	afterNext, err := issuer.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		t.Fatalf("issue token after re-run: %v", err)
	}
	if afterNext != next+1 {
		t.Fatalf("token after re-run = %d then %d; a re-run must not regress or skip the sequence", next, afterNext)
	}
}

// TestSupplyChainImpactSeedAdvancesAnUncalledSequenceEqualToTheWatermarkLive
// pins the seed's is_called guard. After a restore the sequence can sit exactly
// on an admitted watermark with is_called false, so its next value is that same
// token, and the second pass to hold it would be admitted as an identical
// re-execution. The seed must advance past it, and must leave a called sequence
// at the same value alone (the next value is already above the watermark).
func TestSupplyChainImpactSeedAdvancesAnUncalledSequenceEqualToTheWatermarkLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	issuer := postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}}

	var migration string
	for _, definition := range postgres.BootstrapDefinitions() {
		if definition.Name == "supply_chain_impact_write_admission" {
			migration = definition.SQL
		}
	}
	if migration == "" {
		t.Fatal("bootstrap has no supply_chain_impact_write_admission definition")
	}
	const watermark = 500
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_write_admission (scope_id, generation_id, fencing_token, updated_at)
VALUES ('scope:seed-equal', 'gen:seed-equal', $1, $2)`, watermark, replaceSetLiveNow); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}

	// Equal and uncalled: the next nextval would reissue the watermark.
	if _, err := db.ExecContext(ctx, `SELECT setval('supply_chain_impact_fencing_token_seq', $1, false)`, watermark); err != nil {
		t.Fatalf("set the sequence uncalled at the watermark: %v", err)
	}
	if _, err := db.ExecContext(ctx, migration); err != nil {
		t.Fatalf("run the seed: %v", err)
	}
	next, err := issuer.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		t.Fatalf("issue after the seed: %v", err)
	}
	if next <= watermark {
		t.Fatalf("next token = %d after seeding an uncalled sequence at the watermark %d; it must be above it", next, watermark)
	}

	// Equal and called: the next value is already above the watermark, so the
	// seed must not move the sequence.
	if _, err := db.ExecContext(ctx, `SELECT setval('supply_chain_impact_fencing_token_seq', $1, true)`, watermark); err != nil {
		t.Fatalf("set the sequence called at the watermark: %v", err)
	}
	if _, err := db.ExecContext(ctx, migration); err != nil {
		t.Fatalf("run the seed again: %v", err)
	}
	next, err = issuer.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		t.Fatalf("issue after the second seed: %v", err)
	}
	if next != watermark+1 {
		t.Fatalf("next token = %d for a called sequence at the watermark %d, want %d (the seed must leave it alone)", next, watermark, watermark+1)
	}
}
