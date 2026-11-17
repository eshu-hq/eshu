// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// R2 schema shim for the #10827 settle gate. Migrations 303-306 own the
// shipped path (dirty_version column, dedup, the two partial unique
// indexes); every statement below is IF NOT EXISTS with the shipped
// definitions, so it is a no-op once those migrations have applied and the
// gate proves the same behavior before they land.
const winnersR2SettleShimColumnSQL = `
ALTER TABLE supply_chain_impact_winners_dirty_log
    ADD COLUMN IF NOT EXISTS dirty_version BIGINT NOT NULL DEFAULT 1
`

const winnersR2SettleShimFactKeySQL = `
CREATE UNIQUE INDEX IF NOT EXISTS supply_chain_impact_winners_dirty_log_fact_key_uniq
    ON supply_chain_impact_winners_dirty_log (package_id, repository_id, subject_digest, c0, advisory_id)
    WHERE kind = 'fact'
`

const winnersR2SettleShimScopeKeySQL = `
CREATE UNIQUE INDEX IF NOT EXISTS supply_chain_impact_winners_dirty_log_scope_key_uniq
    ON supply_chain_impact_winners_dirty_log (scope_id, generation_id)
    WHERE kind = 'scope'
`

// The gate writers hammer the shipped capture statement itself
// (captureWinnersDirtyFactsSQL), never a copy: a shipped-shape change must
// move this gate with it. Input arrays are passed in reverse key order on
// purpose, so the gate proves the statement's ORDER BY — not the caller's
// input order — governs lock acquisition. Writers in both gates below are
// autocommit single statements: they prove per-statement lock order and
// cannot falsify the multi-batch transaction-level claim (#10851).

// winnersDeadlockSeedSQL plants distinct fact keys for the gate.
const winnersDeadlockSeedSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert',
    'pkg:deadlock:' || lpad(g::text, 3, '0'), 'repo:deadlock', 'digest:deadlock',
    'CVE-2026-0001', 'CVE-2026-0001'
FROM generate_series(0, 59) AS g
`

// winnersDeadlockSettleBounds admits and chunks far past the gate's key
// count, so every settle applies its whole admitted window in one chunk and
// the gate measures lock order, not chunking.
var winnersDeadlockSettleBounds = winnersChunkBounds{dirtyRows: 5000, identifiers: 5000}

// winnersSettleGateDB opens the disposable proof database the way the
// supply-chain family does: ESHU_POSTGRES_TEST_DSN gates the run (fatal when
// the required-proof switch is 1, skip otherwise) and the disposable pair
// scopes the database. It mirrors supplyChainImpactDisposableAdmin without
// importing the external test package.
func winnersSettleGateDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	if os.Getenv("ESHU_POSTGRES_TEST_DSN") == "" {
		if os.Getenv("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF") == "1" {
			t.Fatal("ESHU_POSTGRES_TEST_DSN is unset with ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF=1: the #10827 settle gate would skip")
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the #10827 settle deadlock gate on a disposable database")
	}
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN"),
		os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE"),
		10*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	for _, shim := range []string{
		winnersR2SettleShimColumnSQL,
		winnersR2SettleShimFactKeySQL,
		winnersR2SettleShimScopeKeySQL,
	} {
		if _, err := database.ExecContext(ctx, shim); err != nil {
			t.Fatalf("R2 schema shim: %v\n%s", err, shim)
		}
	}
	return ctx, database
}

// winnersExplainSettleLocks pins the row-order guarantee the verdict's gate
// needs: the applied drain's locked CTE takes its FOR UPDATE locks above the
// key-order Sort, so locks are acquired in conflict-key order.
func winnersExplainSettleLocks(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()

	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, ddl := range []string{
		createWinnersKeyedConsumedSQL,
		buildWinnersKeyedScopeIdentsSQL,
		buildWinnersKeyedIdentsSQL,
		createWinnersKeyedChunkIdentsSQL,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("stage explain temp tables: %v", err)
		}
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+deleteWinnersKeyedAppliedDirtySQL)
	if err != nil {
		t.Fatalf("explain applied drain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain rows: %v", err)
	}
	text := plan.String()
	lockAt, sortAt := strings.Index(text, "LockRows"), strings.Index(text, "Sort")
	if lockAt < 0 || sortAt < 0 || lockAt > sortAt {
		t.Fatalf("applied drain does not lock above the key-order sort:\n%s", text)
	}
	// Some Sort Key line must carry the full conflict key in order; a
	// permuted or dropped column fails here even though every name still
	// appears somewhere in the plan. Any matching line counts: the outer
	// GROUP BY may add its own Sort node ahead of the locking sort.
	wantOrder := []string{"kind", "package_id", "repository_id", "subject_digest", "c0", "advisory_id", "scope_id", "generation_id", "seq"}
	matched := false
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "Sort Key:") {
			continue
		}
		var gotOrder []string
		for _, part := range strings.Split(line[strings.Index(line, "Sort Key:")+len("Sort Key:"):], ",") {
			col := strings.TrimSpace(part)
			if dot := strings.LastIndex(col, "."); dot >= 0 {
				col = col[dot+1:]
			}
			gotOrder = append(gotOrder, col)
		}
		if strings.Join(gotOrder, ",") == strings.Join(wantOrder, ",") {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("no Sort Key line carries the full conflict key in order %v:\n%s", wantOrder, text)
	}
}

// TestSupplyChainImpactWinnersDirtyConvertMixedSourceLive pins the convert statement's
// one-proposal-per-key shape: the same unapplied identifier as the remainder
// of two partially applied scope rows with different sources must convert
// to exactly one fact row. A SELECT DISTINCT over (source, key) proposes
// the key twice and Postgres refuses the whole upsert ("ON CONFLICT DO
// UPDATE command cannot affect row a second time"), failing every tick on
// the same admission window.
func TestSupplyChainImpactWinnersDirtyConvertMixedSourceLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	tx, err := (SQLDB{DB: database}).Begin(ctx)
	if err != nil {
		t.Fatalf("begin convert transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		createWinnersKeyedConsumedSQL,
		createWinnersKeyedChunkIdentsSQL,
		`CREATE TEMP TABLE winners_keyed_scope_idents (
			scope_id text, generation_id text, package_id text, repository_id text,
			subject_digest text, c0 text, advisory_id text
		) ON COMMIT DROP`,
		`INSERT INTO winners_keyed_consumed
			(seq, kind, source, scope_id, generation_id, captured_at, dirty_version)
		VALUES (1, 'scope', 'activation', 'scope-a', 'gen-1', now(), 1),
		       (2, 'scope', 'operator', 'scope-b', 'gen-1', now(), 1)`,
		// Each scope has one applied identifier (in the chunk) and shares
		// the same unapplied remainder identifier.
		`INSERT INTO winners_keyed_scope_idents VALUES
			('scope-a', 'gen-1', 'pkg:applied', 'repo://x', '', 'CVE-1', 'CVE-1'),
			('scope-a', 'gen-1', 'pkg:shared', 'repo://x', '', 'CVE-2', 'CVE-2'),
			('scope-b', 'gen-1', 'pkg:applied', 'repo://x', '', 'CVE-1', 'CVE-1'),
			('scope-b', 'gen-1', 'pkg:shared', 'repo://x', '', 'CVE-2', 'CVE-2')`,
		`INSERT INTO winners_keyed_chunk_idents VALUES ('pkg:applied', 'repo://x', '', 'CVE-1', 'CVE-1')`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("stage convert fixture: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, convertWinnersKeyedScopeRemainderSQL); err != nil {
		t.Fatalf("convert mixed-source remainder: %v", err)
	}
	n, err := scanWinnersInt64(ctx, tx, `
SELECT count(*) FROM supply_chain_impact_winners_dirty_log
WHERE kind = 'fact' AND package_id = 'pkg:shared'`, "count converted rows")
	if err != nil {
		t.Fatalf("count converted rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("converted fact rows for shared identifier = %d, want exactly 1", n)
	}
}

// winnersSettleOnce runs one fact-only settle pass: admit, build the
// identifier set and chunk, settle. It returns the drained version sums and
// the residual depth.
func winnersSettleOnce(ctx context.Context, tx db.Transaction) (winnersCaptureTally, int64, error) {
	if _, err := admitWinnersDirtyWindow(ctx, tx, winnersDeadlockSettleBounds.dirtyRows); err != nil {
		return nil, 0, fmt.Errorf("admit: %w", err)
	}
	for _, stmt := range []string{
		buildWinnersKeyedScopeIdentsSQL,
		buildWinnersKeyedIdentsSQL,
		analyzeWinnersKeyedIdentsSQL,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return nil, 0, fmt.Errorf("build identifiers: %w", err)
		}
	}
	if _, err := buildWinnersKeyedChunk(ctx, tx, winnersDeadlockSettleBounds); err != nil {
		return nil, 0, fmt.Errorf("build chunk: %w", err)
	}
	tally, remaining, _, err := settleWinnersKeyedDirtyLog(ctx, tx, false)
	if err != nil {
		return nil, 0, fmt.Errorf("settle: %w", err)
	}
	return tally, remaining, nil
}

// TestSupplyChainImpactWinnersResweepKeyedSettleDeadlockFreeLive is test (3)
// of the #10827 R2 TDD order, the deterministic deadlock interleave gate:
// two settle transactions race K-ordered writer upserts (input in reverse key
// order, so the statement's ORDER BY governs) over the same hot keys. Zero
// 40P01/deadlock errors may surface, and every mark is conserved: the drained
// version sums plus the residual versions equal the marks the writers sent,
// so no interleaving loses a queued mark. The static shape tests pin the
// lock order; the EXPLAIN assertion above pins LockRows-above-Sort on the
// shipped statement; this gate pins the runtime behavior.
func TestSupplyChainImpactWinnersResweepKeyedSettleDeadlockFreeLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	winnersExplainSettleLocks(t, ctx, database)

	// Baseline the marks bootstrap itself left queued (the bootstrap moves
	// scope pointers, so the activation trigger owns rows here before the
	// gate plants its own): the drain tallies every version it deletes, so
	// conservation counts them too.
	var baseVersions int64
	if err := database.QueryRowContext(ctx,
		`SELECT COALESCE(sum(dirty_version), 0) FROM supply_chain_impact_winners_dirty_log`).Scan(&baseVersions); err != nil {
		t.Fatalf("read baseline log versions: %v", err)
	}

	if _, err := database.ExecContext(ctx, winnersDeadlockSeedSQL); err != nil {
		t.Fatalf("seed deadlock keys: %v", err)
	}
	const keys = 60
	var totalMarks atomic.Int64
	totalMarks.Add(baseVersions + keys)

	// The writer keys in reverse K order: the upsert's ORDER BY must re-sort
	// them, so lock acquisition never follows caller order.
	reversed := make([]string, keys)
	for i := range reversed {
		reversed[i] = fmt.Sprintf("pkg:deadlock:%03d", keys-1-i)
	}
	const repos, digest, cve = "repo:deadlock", "digest:deadlock", "CVE-2026-0001"
	reposArr := make([]string, keys)
	digestArr := make([]string, keys)
	cveArr := make([]string, keys)
	for i := range reposArr {
		reposArr[i], digestArr[i], cveArr[i] = repos, digest, cve
	}

	var errs atomic.Int64
	record := func(err error) {
		if err == nil {
			return
		}
		errs.Add(1)
		t.Errorf("interleaved accessor failed: %v", err)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

	start := make(chan struct{})
	var workers sync.WaitGroup
	var drained atomic.Int64
	// Two settlers race the writers over the same admitted window. Every
	// drained version sum counts toward conservation, including the ones the
	// racing settles tally while the writers are still sending.
	for s := 0; s < 2; s++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for i := 0; i < 8; i++ {
				tx, err := (SQLDB{DB: database}).Begin(ctx)
				if err != nil {
					record(err)
					return
				}
				tally, _, err := winnersSettleOnce(ctx, tx)
				if err != nil {
					_ = tx.Rollback()
					record(err)
					return
				}
				if err := tx.Commit(); err != nil {
					record(err)
					return
				}
				drained.Add(tally.total())
			}
		}()
	}
	// Four writers hammer the same keys while the settles run.
	for w := 0; w < 4; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for i := 0; i < 10; i++ {
				if _, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL,
					reversed, reposArr, digestArr, cveArr, cveArr); err != nil {
					if isDeadlock(err) {
						t.Errorf("writer hit a lock-order cycle (40P01): %v", err)
					}
					record(err)
					return
				}
				totalMarks.Add(keys)
			}
		}()
	}
	close(start)
	workers.Wait()
	if errs.Load() != 0 {
		t.Fatalf("%d interleaved accessor(s) failed; the gate needs zero failures", errs.Load())
	}

	// Final drain: every mark is either tallied or still queued with its
	// version, so the sums must equal the marks sent and the log must empty.
	for i := 0; i < 20; i++ {
		tx, err := (SQLDB{DB: database}).Begin(ctx)
		if err != nil {
			t.Fatalf("begin final drain: %v", err)
		}
		tally, remaining, err := winnersSettleOnce(ctx, tx)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("final drain: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit final drain: %v", err)
		}
		drained.Add(tally.total())
		if remaining == 0 {
			break
		}
		if i == 19 {
			t.Fatalf("log did not drain after 20 final settles, %d rows remain", remaining)
		}
	}
	var depth, versions int64
	if err := database.QueryRowContext(ctx,
		`SELECT count(*), COALESCE(sum(dirty_version), 0) FROM supply_chain_impact_winners_dirty_log`).Scan(&depth, &versions); err != nil {
		t.Fatalf("read residual log: %v", err)
	}
	if depth != 0 || versions != 0 {
		t.Fatalf("residual log holds %d rows with %d versions, want an empty log", depth, versions)
	}
	if got, want := drained.Load(), totalMarks.Load(); got != want {
		t.Fatalf("drained version sums = %d, want %d marks sent: marks were lost or double-counted", got, want)
	}
}

// winnersDirtyTwoWriterP95Envelope and winnersDirtyFourWriterP95Baseline are
// the arbiter-ruled STRATIFIED p95 reference for the #10827 R2 capture upsert:
// one 60-key K-ordered writer statement (captureWinnersDirtyFactsSQL, the
// shipped capture shape), writers-only, warm plan and cache. The 2-writer
// stratum measured 3.41ms and the 4-writer stratum ~3.89ms on the shim host.
// The two numbers stay separate constants on purpose: a single raised cap
// (one number for both strata) would let a 2-writer regression hide under
// 4-writer headroom, and TestWinnersDirtyStratifiedEnvelopesStayStratified
// fails any edit that unifies them. They are the shim-host reference now,
// not the CI hard gate: absolute targets apply only to their measured host,
// so CI gates the same-run contention ratio below and logs these for
// reference.
const (
	winnersDirtyTwoWriterP95Envelope  = 3410 * time.Microsecond
	winnersDirtyFourWriterP95Baseline = 3890 * time.Microsecond
)

// winnersDirtyMaxContentionRatio is the backstop arm of the hybrid gate below:
// how far a contended stratum's p95 may stand above the same-run
// single-writer control p95 before the run fails. The ratios stay stratified
// (tighter on the quieter stratum): a unified cap would let a 2-writer
// contention regression hide under 4-writer headroom. Calibration, three
// local runs: control p95 ~1.39ms, ratios ~1.5 at 2 writers and ~1.6-1.7 at
// 4. The slowest CI runner observed (run 44070281709, p95 3.885/5.255ms)
// infers ~2.1 at 2 writers and ~2.9 at 4 under p50-scaled control. Full
// serialization is 2x at 2 writers and 4x at 4, so the 2-writer inference
// already exceeds it — host oversubscription noise no fixed ratio can both
// pass and use to catch serialization collapse. The caps (3.0/3.5) sit
// beyond serialization at 2 writers and inside it at 4: global
// superlinear contention growth on the gate's disjoint private key sets
// breaks the backstop arm,
// while lock order stays with the deadlock assertions above. Shared
// hot-key serialization latency and uniform statement-cost drift are both
// unwatched: no benchmark runs this statement shape, and the bench
// regression check is advisory. That backstop is tracked in #10842.
const (
	winnersDirtyTwoWriterMaxRatio  = 3.0
	winnersDirtyFourWriterMaxRatio = 3.5
)

// winnersDirtyGateFails is the hybrid gate: a stratum fails only when its
// p95 breaks the shim-host reference AND its same-run contention ratio
// breaks the backstop. The conjunction passes absolute-only excursions on
// every host, reference hardware included (ratios there sit near 1.5, far
// under the backstop), so a uniform statement-cost regression passes this
// gate; that drift is unwatched pending the #10842 bench backstop. What the
// ratio arm buys is deflaking host oversubscription noise on slower shared
// runners that the identical code cannot control.
func winnersDirtyGateFails(p95, reference time.Duration, ratio, maxRatio float64) bool {
	return p95 > reference && ratio > maxRatio
}

// winnersDirtyContentionRatio reports stratum p95 as a multiple of the
// same-run single-writer control p95, so the gate travels with runner speed
// instead of pinning one host's absolute latency.
func winnersDirtyContentionRatio(stratumP95, controlP95 time.Duration) float64 {
	if controlP95 <= 0 {
		return math.Inf(1)
	}
	return float64(stratumP95) / float64(controlP95)
}

// winnersDirtyLatencyPercentile reports the nearest-rank pth percentile of the
// sample (rank ceil(p*n) over the sorted durations), the same estimator the
// holder H3 statement gate uses: the bound holds per stratum without assuming
// a latency distribution.
func winnersDirtyLatencyPercentile(durs []time.Duration, p float64) time.Duration {
	if len(durs) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), durs...)
	slices.Sort(cp)
	i := int(math.Ceil(p*float64(len(cp)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(cp) {
		i = len(cp) - 1
	}
	return cp[i]
}

// winnersStratifiedSeedSQL plants the stratified gate's key space: 60
// private keys per writer (writers 0-3), so every statement is a
// steady-state upsert bump with no insert/update mix variance. Writers are
// fully disjoint by key: concurrent production writers mark their own
// scopes' identifiers, and sharing one hot row across every statement would
// serialize all writers through it, measuring lock-wait scheduling instead
// of statement latency (the deadlock test above already hammers the shared
// hot keys for exactly that interleave).
const winnersStratifiedSeedSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', 'pkg:stratum:' || lpad(w::text, 2, '0') || ':' || lpad(g::text, 3, '0'),
    'repo:stratified', 'digest:stratified', 'CVE-2026-0001', 'CVE-2026-0001'
FROM generate_series(0, 3) AS w, generate_series(0, 59) AS g
`

// TestSupplyChainImpactWinnersDirtyWriterStratifiedEnvelopeLive is the perf
// half of the #10827 R2 settle gate. It times the K-ordered writer upsert
// (the shipped capture shape, one 60-key statement) stratified by
// concurrent-writer count: a single-writer control, then 2 writers, then 4.
// Each writer's statement set is its own 60 private keys: concurrent
// production writers mark their own scopes' identifiers, so disjoint sets
// are the production shape. The sets reach the statement in reverse K order,
// so the statement's ORDER BY — not caller order — still governs lock
// acquisition. Each writer runs two untimed warmup passes so the sample is
// warm-cache steady state, then its timed iterations. Settlers stay out of
// this gate on purpose: the sample is writer-statement latency, and the
// deadlock test above covers both the shared-key interleave and the
// writer-vs-settle interleave. Each contended stratum fails only when its
// p95 breaks the shim-host reference AND its same-run contention ratio
// breaks the backstop (max 3.0x at 2 writers, 3.5x at 4): the conjunction
// passes absolute-only excursions on every host, so uniform statement-cost
// drift passes here too; it is unwatched pending the #10842 bench backstop.
// On slower shared runners the ratio arm deflakes host oversubscription
// noise.
func TestSupplyChainImpactWinnersDirtyWriterStratifiedEnvelopeLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	if _, err := database.ExecContext(ctx, winnersStratifiedSeedSQL); err != nil {
		t.Fatalf("seed stratified keys: %v", err)
	}
	// stratifiedKeys returns writer w's 60 private keys in reverse K order.
	stratifiedKeys := func(w int) []string {
		ks := make([]string, 0, 60)
		for g := 0; g < 60; g++ {
			ks = append(ks, fmt.Sprintf("pkg:stratum:%02d:%03d", w, g))
		}
		slices.Sort(ks)
		slices.Reverse(ks)
		return ks
	}
	const keys = 60
	const repos, digest, cve = "repo:stratified", "digest:stratified", "CVE-2026-0001"
	reposArr := make([]string, keys)
	digestArr := make([]string, keys)
	cveArr := make([]string, keys)
	for i := range reposArr {
		reposArr[i], digestArr[i], cveArr[i] = repos, digest, cve
	}

	var failed atomic.Int64
	record := func(err error) {
		if err == nil {
			return
		}
		failed.Add(1)
		t.Errorf("stratified accessor failed: %v", err)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

	// runStratum hammers the writer upsert with writers concurrent writers
	// for iters timed iterations each and returns every statement latency.
	runStratum := func(writers, iters int) []time.Duration {
		samples := make([][]time.Duration, writers)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for w := 0; w < writers; w++ {
			workers.Add(1)
			go func(w int) {
				defer workers.Done()
				<-start
				pkgs := stratifiedKeys(w)
				local := make([]time.Duration, 0, iters)
				for i := -2; i < iters; i++ {
					begin := time.Now()
					_, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL,
						pkgs, reposArr, digestArr, cveArr, cveArr)
					elapsed := time.Since(begin)
					if err != nil {
						if isDeadlock(err) {
							t.Errorf("stratified writer hit a lock-order cycle (40P01): %v", err)
						}
						record(err)
						return
					}
					if i >= 0 {
						local = append(local, elapsed)
					}
				}
				samples[w] = local
			}(w)
		}
		close(start)
		workers.Wait()
		var out []time.Duration
		for _, s := range samples {
			out = append(out, s...)
		}
		return out
	}

	// The single-writer control anchors every ratio below to this runner: it
	// runs the identical statement on its own 60 private keys with no
	// contention, so its p95 is this host's steady-state statement cost. It
	// runs first and doubles as extra cache warmup for the contended strata.
	controlDurs := runStratum(1, 25)
	if failed.Load() != 0 {
		t.Fatalf("control: %d accessor(s) failed; the sample is incomplete", failed.Load())
	}
	if want := 25; len(controlDurs) != want {
		t.Fatalf("control: %d statement samples, want %d", len(controlDurs), want)
	}
	controlP95 := winnersDirtyLatencyPercentile(controlDurs, 0.95)
	controlP50 := winnersDirtyLatencyPercentile(controlDurs, 0.5)
	t.Logf("winners_dirty_writer_p95 writers=1 n=%d p50=%s p95=%s max=%s role=control",
		len(controlDurs), controlP50, controlP95, slices.Max(controlDurs))

	check := func(writers, iters int, durs []time.Duration, reference time.Duration, maxRatio float64, stratum string) {
		if failed.Load() != 0 {
			t.Fatalf("%s: %d accessor(s) failed; the sample is incomplete", stratum, failed.Load())
		}
		if want := writers * iters; len(durs) != want {
			t.Fatalf("%s: %d statement samples, want %d (%d writers x %d iterations)", stratum, len(durs), want, writers, iters)
		}
		p50 := winnersDirtyLatencyPercentile(durs, 0.5)
		p95 := winnersDirtyLatencyPercentile(durs, 0.95)
		ratio := winnersDirtyContentionRatio(p95, controlP95)
		t.Logf("winners_dirty_writer_p95 writers=%d n=%d p50=%s p95=%s max=%s control_p95=%s ratio=%.2f max_ratio=%.1f reference=%s",
			writers, len(durs), p50, p95, slices.Max(durs), controlP95, ratio, maxRatio, reference)
		if winnersDirtyGateFails(p95, reference, ratio, maxRatio) {
			t.Errorf("stratified gate writers=%d failed over %d statements: p95 %s above the %s %s and %.2fx the same-run control above the %.1f backstop: the capture upsert regressed for this writer count",
				writers, len(durs), p95, reference, stratum, ratio, maxRatio)
		}
	}

	check(2, 25, runStratum(2, 25), winnersDirtyTwoWriterP95Envelope, winnersDirtyTwoWriterMaxRatio, "envelope")
	check(4, 15, runStratum(4, 15), winnersDirtyFourWriterP95Baseline, winnersDirtyFourWriterMaxRatio, "baseline")
}
