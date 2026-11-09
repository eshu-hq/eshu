// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fenceWrite builds a writer input for the generation-fence proof: a strictly
// increasing fencing token per write, like the production sequence.
func fenceWrite(intentID, scope, generation string, findings ...supplychaincore.SupplyChainImpactFinding) supplychaincore.SupplyChainImpactWrite {
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: replaceSetNextToken(),
		IntentID:     intentID,
		ScopeID:      scope,
		GenerationID: generation,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #10422 fence proof",
		Findings:     findings,
	}
}

// advanceFenceScope supersedes the scope's active generation and activates a
// newer one with a later ingested_at, mimicking the projector's activation:
// the older generation is stale from here on, whatever token a late pass
// draws.
func advanceFenceScope(t *testing.T, ctx context.Context, db *sql.DB, scope, older, newer string, at time.Time) {
	t.Helper()
	// Supersede first: one generation per scope may hold active status
	// (scope_generations_active_scope_idx).
	if _, err := db.ExecContext(ctx, `UPDATE scope_generations
SET status = 'superseded', superseded_at = $3
WHERE generation_id = $1 AND scope_id = $2`, older, scope, at); err != nil {
		t.Fatalf("supersede %s: %v", older, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, newer, scope, at); err != nil {
		t.Fatalf("activate %s: %v", newer, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, newer); err != nil {
		t.Fatalf("activate %s on scope: %v", newer, err)
	}
}

// fencePassState reads the scope's durable pass-state row: partial flag,
// causes text, and fencing token. The causes read as text to stay off
// driver array decoding. No row reports recorded=false.
func fencePassState(t *testing.T, ctx context.Context, db *sql.DB, scope string) (recorded, partial bool, causes string, token int64) {
	t.Helper()
	err := db.QueryRowContext(ctx, `SELECT partial_evidence, partial_causes::text, fencing_token
FROM supply_chain_impact_scope_pass_state WHERE scope_id = $1`, scope).Scan(&partial, &causes, &token)
	if err == sql.ErrNoRows {
		return false, false, "", 0
	}
	if err != nil {
		t.Fatalf("read pass state: %v", err)
	}
	return true, partial, causes, token
}

// TestStaleGenerationPassPreservesScopePassStateLive proves #10422 against
// real SQL: a stale generation's late pass draws a higher fencing token (tokens
// are per-pass monotonic, not per-generation), is admitted under its own
// generation, and must still leave the scope's pass-state row untouched once a
// lifecycle-newer generation is reader-visible. The lifecycle order is
// (ingested_at, generation_id), the order the supersede sweep, the freshness
// guard, and the projector use.
func TestStaleGenerationPassPreservesScopePassStateLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	scope := "fence-scope"
	fanOutSeedScope(t, ctx, db, scope, "gen-1")
	gen1 := fanOutGen(scope, "gen-1")
	gen2 := fanOutGen(scope, "gen-2")

	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	keep := fallbackFinding("CVE-2026-9101", "pkg:npm/a@1")

	// The current generation marks the scope partial: no newer generation
	// exists yet, so the record lands.
	stale := fenceWrite("intent:fence:gen1-partial", scope, gen1, keep)
	stale.PartialEvidence = true
	stale.PartialCauses = []string{"evidence_budget"}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, stale); err != nil {
		t.Fatalf("gen-1 partial write: %v", err)
	}
	recorded, partial, _, _ := fencePassState(t, ctx, db, scope)
	if !recorded || !partial {
		t.Fatalf("gen-1 partial recorded=%v partial=%v, want recorded partial (no newer generation yet)", recorded, partial)
	}

	// The lifecycle advances: gen-2 ingests after the seed's clock
	// (fanOutSeedScope stamps 2026-10-31), so it is lifecycle-newer and
	// gen-1 is stale.
	advanceFenceScope(t, ctx, db, scope, gen1, gen2, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	// The newer generation records and clears: the fence must let the
	// lifecycle-newest generation through, and the result must not report
	// a skip.
	gen2Res, err := writer.WriteSupplyChainImpactFindings(ctx, fenceWrite("intent:fence:gen2-complete", scope, gen2, keep))
	if err != nil {
		t.Fatalf("gen-2 complete write: %v", err)
	}
	if gen2Res.PassStateSkipped {
		t.Fatalf("gen-2 complete PassStateSkipped=true, want false (lifecycle-newest records)")
	}
	if recorded, partial, _, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("gen-2 complete recorded=%v partial=%v, want recorded clear", recorded, partial)
	}

	// The stale generation replays late with a HIGHER fencing token. The
	// token guard passes (monotonic per pass), admission passes (per
	// scope+generation), but the row must stay clear and the result must
	// report the fence skip.
	replay := fenceWrite("intent:fence:gen1-replay", scope, gen1, keep)
	replay.PartialEvidence = true
	replay.PartialCauses = []string{"evidence_budget"}
	replayRes, err := writer.WriteSupplyChainImpactFindings(ctx, replay)
	if err != nil {
		t.Fatalf("stale gen-1 replay write: %v", err)
	}
	if !replayRes.PassStateSkipped {
		t.Fatalf("stale gen-1 replay PassStateSkipped=false, want true (fence held)")
	}
	if recorded, partial, causes, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("stale replay recorded=%v partial=%v causes=%v, want the gen-2 clear row untouched", recorded, partial, causes)
	}

	// Absent lineage cannot touch the row: for a generation with no
	// scope_generations row the bulk findings insert fails its
	// scope_generations foreign key first and the transaction aborts before
	// the record step (which runs last) ever executes. The gen-2 clear row
	// survives, no skip is reported, and the write returns the error.
	unknown := fenceWrite("intent:fence:genx-partial", scope, fanOutGen(scope, "gen-x"), keep)
	unknown.PartialEvidence = true
	unknown.PartialCauses = []string{"evidence_budget"}
	unknownRes, err := writer.WriteSupplyChainImpactFindings(ctx, unknown)
	if err == nil {
		t.Fatalf("unknown-generation write succeeded, want the findings foreign key to reject it")
	}
	if unknownRes.PassStateSkipped {
		t.Fatalf("unknown-generation PassStateSkipped=true, want false (the record step never ran)")
	}
	if recorded, partial, causes, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("unknown-generation recorded=%v partial=%v causes=%v, want the gen-2 clear row untouched", recorded, partial, causes)
	}
}

// fenceOverlapGate is the deterministic rendezvous for the stale-commit-last
// overlap proof (#10460). The stale pass runs through fenceOverlapBeginner,
// which closes bulkDone after the pass's first fact_records Exec (its bulk
// findings insert, still uncommitted) and parks the pass-state record
// statement on release. The test advances the lifecycle and commits the newer
// generation between those two points, so the stale record statement
// deterministically executes after the newer commit while the stale bulk is
// still in flight. A record step moved back ahead of the bulk writes runs
// before bulkDone with no newer generation visible and records instead of
// rendezvousing, so the proof goes red on that mutation.
type fenceOverlapGate struct {
	bulkDone    chan struct{}
	release     chan struct{}
	bulkOnce    sync.Once
	releaseOnce sync.Once
}

func newFenceOverlapGate() *fenceOverlapGate {
	return &fenceOverlapGate{bulkDone: make(chan struct{}), release: make(chan struct{})}
}

func (g *fenceOverlapGate) markBulkDone() { g.bulkOnce.Do(func() { close(g.bulkDone) }) }

func (g *fenceOverlapGate) open() { g.releaseOnce.Do(func() { close(g.release) }) }

// fenceOverlapBeginner routes one pass's transaction through fenceOverlapTx so
// the test can park that pass mid-flight. Only the stale pass's writer uses
// it; the newer generation commits through the plain beginner.
type fenceOverlapBeginner struct {
	inner supplychaincore.SupplyChainImpactBeginner
	gate  *fenceOverlapGate
}

func (b fenceOverlapBeginner) BeginSupplyChainImpactTx(ctx context.Context) (supplychaincore.SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return &fenceOverlapTx{tx: tx, gate: b.gate}, nil
}

// fenceOverlapTx forwards the whole transaction surface and only changes two
// statement kinds: the first fact_records Exec marks the bulk landed, and the
// post-bulk pass-state record waits for the test's release. Forwarding (not
// embedding) keeps the optional winners surface answering: the assertions
// below would otherwise run a pass that captures nothing, unlike production.
type fenceOverlapTx struct {
	tx       supplychaincore.SupplyChainImpactTx
	gate     *fenceOverlapGate
	bulkSeen atomic.Bool
}

func (t *fenceOverlapTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := t.tx.ExecContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "fact_records") {
		t.bulkSeen.Store(true)
		t.gate.markBulkDone()
	}
	return res, err
}

func (t *fenceOverlapTx) QueryContext(ctx context.Context, query string, args ...any) (supplychaincore.SupplyChainImpactRows, error) {
	if strings.Contains(query, "supply_chain_impact_scope_pass_state") && t.bulkSeen.Load() {
		select {
		case <-t.gate.release:
		case <-ctx.Done():
		}
	}
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *fenceOverlapTx) Commit() error { return t.tx.Commit() }

func (t *fenceOverlapTx) Rollback() error { return t.tx.Rollback() }

// CaptureWinnersDirtyFacts forwards the optional winners surface so the
// parked pass captures exactly what the plain transaction would.
func (t *fenceOverlapTx) CaptureWinnersDirtyFacts(ctx context.Context, identifiers []supplychaincore.WinnersDirtyIdentifier) (int64, error) {
	if capturer, ok := t.tx.(supplychaincore.SupplyChainImpactWinnersCapturer); ok {
		return capturer.CaptureWinnersDirtyFacts(ctx, identifiers)
	}
	return 0, nil
}

// RecordWinnersCapture forwards the winners accounting the same way.
func (t *fenceOverlapTx) RecordWinnersCapture(ctx context.Context, source string, rows int64) {
	if capturer, ok := t.tx.(supplychaincore.SupplyChainImpactWinnersCapturer); ok {
		capturer.RecordWinnersCapture(ctx, source, rows)
	}
}

// TestStaleGenerationPassOverlapsNewerCommitLive proves the stale-commit-last
// interleave for the generation fence (#10460) against real Postgres: a stale
// generation's pass has its bulk findings insert in flight (uncommitted) while
// the lifecycle advances and the newer generation's pass records and commits;
// the stale pass then records last, sees the newer generation, skips its
// per-scope pass-state write, and still commits its own per-generation
// findings. Same-scope, different-generation passes take different
// conflict-domain locks and never wait on each other, so without the
// record-last order the newer commit would land mid-stale-pass unseen.
func TestStaleGenerationPassOverlapsNewerCommitLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	scope := "fence-overlap-scope"
	fanOutSeedScope(t, ctx, db, scope, "gen-1")
	gen1 := fanOutGen(scope, "gen-1")
	gen2 := fanOutGen(scope, "gen-2")
	keep := fallbackFinding("CVE-2026-9101", "pkg:npm/a@1")

	newWriter := func(beginner supplychaincore.SupplyChainImpactBeginner) supplychaincore.PostgresSupplyChainImpactWriter {
		return supplychaincore.PostgresSupplyChainImpactWriter{
			DB:  beginner,
			Now: func() time.Time { return replaceSetLiveNow },
		}
	}
	plain := newWriter(postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}})
	gate := newFenceOverlapGate()
	staleWriter := newWriter(fenceOverlapBeginner{
		inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		gate:  gate,
	})

	// The current generation marks the scope partial: no newer generation
	// exists yet, so the record lands.
	first := fenceWrite("intent:fence-overlap:gen1-partial", scope, gen1, keep)
	first.PartialEvidence = true
	first.PartialCauses = []string{"evidence_budget"}
	if _, err := plain.WriteSupplyChainImpactFindings(ctx, first); err != nil {
		t.Fatalf("gen-1 partial write: %v", err)
	}
	if recorded, partial, _, _ := fencePassState(t, ctx, db, scope); !recorded || !partial {
		t.Fatalf("gen-1 partial recorded=%v partial=%v, want recorded partial (no newer generation yet)", recorded, partial)
	}

	// The stale replay runs concurrently: its bulk findings insert lands
	// (uncommitted) and its pass-state record parks on the gate. The result
	// rides a channel because Failf/Goexit stays on the test goroutine.
	type staleOutcome struct {
		res supplychaincore.SupplyChainImpactWriteResult
		err error
	}
	staleDone := make(chan staleOutcome, 1)
	go func() {
		replay := fenceWrite("intent:fence-overlap:gen1-replay", scope, gen1, keep)
		replay.PartialEvidence = true
		replay.PartialCauses = []string{"evidence_budget"}
		res, err := staleWriter.WriteSupplyChainImpactFindings(ctx, replay)
		staleDone <- staleOutcome{res: res, err: err}
	}()

	// Wait for the stale bulk to land, then advance the lifecycle and commit
	// the newer generation while it is still in flight. The release is the
	// newer commit for the stale record statement: it cannot execute first.
	select {
	case <-gate.bulkDone:
	case <-time.After(2 * time.Minute):
		t.Fatalf("stale gen-1 bulk write did not land: the overlap never formed")
	}
	defer gate.open()
	advanceFenceScope(t, ctx, db, scope, gen1, gen2, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if _, err := plain.WriteSupplyChainImpactFindings(ctx, fenceWrite("intent:fence-overlap:gen2-complete", scope, gen2, keep)); err != nil {
		t.Fatalf("gen-2 complete write: %v", err)
	}
	gate.open()

	var stale staleOutcome
	select {
	case stale = <-staleDone:
	case <-time.After(2 * time.Minute):
		t.Fatalf("stale gen-1 pass did not commit after the newer commit")
	}
	if stale.err != nil {
		t.Fatalf("stale gen-1 pass: %v", stale.err)
	}
	if !stale.res.PassStateSkipped {
		t.Fatalf("stale gen-1 PassStateSkipped=false, want true (its record ran after the newer commit)")
	}
	if recorded, partial, causes, _ := fencePassState(t, ctx, db, scope); !recorded || partial {
		t.Fatalf("overlap recorded=%v partial=%v causes=%v, want the gen-2 clear row untouched", recorded, partial, causes)
	}
	var staleRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE`, scope, gen1).Scan(&staleRows); err != nil {
		t.Fatalf("count stale findings: %v", err)
	}
	if staleRows == 0 {
		t.Fatalf("stale gen-1 findings missing: the overlapped pass must still commit its own generation")
	}
}
