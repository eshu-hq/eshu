// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSupplyChainImpactWinnersHotkeyStagingShimLive is the prove-theory shim
// for #10837 direction C (staging plain-insert + GROUP BY collapse), gated by
// the arbiter ruling recorded at goal-ops/ruling-10837-direction.md.
//
// The shipped stratified gate (TestSupplyChainImpactWinnersDirtyWriterStratifiedEnvelopeLive)
// uses DISJOINT private keys per writer and cannot see hot-row waiting. This
// harness is SEPARATE and throwaway: N writers (1, 2, 4) all mark the SAME
// 162 hot keys (plus a private tail), and times two cells on the same
// host in the same run:
//
//   - cell "upsert": the shipped capture statement (captureWinnersDirtyFactsSQL)
//     over the shared hot-key shape (steady-state version bumps).
//   - cell "staging": a plain multi-row INSERT into a session TEMP table
//     (no unique index, no ORDER BY, no ON CONFLICT) with the same 162+tail
//     row shape. The TEMP table is scratch: created per pinned session,
//     never part of bootstrap/migrations/production DDL.
//   - cell "collapse": GROUP BY over a full staged wave (162 keys x 150
//     marks = 24,300 rows), key-ordered, with count(*) and min(captured_at).
//   - cell "worst": GROUP BY over a mostly-distinct wave (24,300 distinct
//     keys), the no-combining partition ceiling.
//   - cell "vacuum": staging heap growth across 5 undrained waves plus
//     TRUNCATE and VACUUM cost, reported as a note.
//
// PRE-REGISTERED KILL THRESHOLD (ruling condition 1): at the 4-writer
// stratum on this shared hot-key shape, the staging writer p95 must be
// decisively below the same-run contended-upsert p95 (ratio <= 0.80) AND
// at/under the 3.41ms envelope with the amortized collapse cost included
// (collapse p95 spread over the 150 statements of one wave). A miss kills
// the theory pre-build: no production code follows. The absolute arm travels
// with the shim host (see TestSupplyChainImpactWinnersDirtyWriterStratifiedEnvelopeLive
// for why absolute references are host-bound); the relative arm travels with
// this run.
const (
	hotkeyShimHotKeys   = 162
	hotkeyShimTailKeys  = 18
	hotkeyShimStmtKeys  = hotkeyShimHotKeys + hotkeyShimTailKeys
	hotkeyShimWaveMarks = 150

	// hotkeyShimCollapseStatementsPerWave is the amortization denominator:
	// one wave (162 keys x 150 marks) is the staged output of 150
	// full-statement mark rounds, so each statement carries
	// collapseP95/150 of collapse cost.
	hotkeyShimCollapseStatementsPerWave = 150
	hotkeyShimCollapseSamples           = 11

	hotkeyShimMaxStagingToUpsertRatio = 0.80
	hotkeyShimStagingEnvelope         = 3410 * time.Microsecond
)

// hotkeyShimStagingDDL is session scratch, not production DDL: a TEMP table
// with no constraints and no indexes, so inserts take no row locks and never
// wait on each other. Created once per pinned session (database/sql Conn).
// IF NOT EXISTS plus an explicit TRUNCATE after creation: database/sql
// Conn.Close returns the session to the pool without ending it, so a reused
// pooled session may already carry this table (with rows) from an earlier
// stratum; bare CREATE would fail with 42P07 and stale rows would pollute
// the sample.
const hotkeyShimStagingDDL = `
CREATE TEMPORARY TABLE IF NOT EXISTS winners_staging_shim_hotkey (
    kind TEXT NOT NULL,
    source TEXT NOT NULL,
    package_id TEXT,
    repository_id TEXT,
    subject_digest TEXT,
    c0 TEXT,
    advisory_id TEXT,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
)
`

// hotkeyShimStagingInsertSQL mirrors the shipped capture statement's unnest
// shape (same caller arrays, same row width) minus everything that makes the
// capture wait: no ORDER BY (heap order is irrelevant without locks), no
// ON CONFLICT (no arbiter, no version bump).
const hotkeyShimStagingInsertSQL = `
INSERT INTO winners_staging_shim_hotkey
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', package_id, repository_id, subject_digest, c0, advisory_id
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
     AS ident(package_id, repository_id, subject_digest, c0, advisory_id)
`

// hotkeyShimCollapseSQL is the collapse operator the theory needs: one pass
// over the staged wave, key-ordered (the merge into the dirty log keeps the
// shipped K order), carrying the conservation inputs per key (mark count for
// dirty_version += count, earliest stamp for captured_at preservation).
const hotkeyShimCollapseSQL = `
SELECT package_id, repository_id, subject_digest, c0, advisory_id,
       count(*)::bigint AS marks, min(captured_at) AS first_mark
FROM winners_staging_shim_hotkey
GROUP BY package_id, repository_id, subject_digest, c0, advisory_id
ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
`

// hotkeyShimSeedSQL plants the shared hot shape: 162 hot keys every writer
// marks plus each writer's private tail, so the upsert cell measures
// steady-state version bumps with no insert/update mix variance.
const hotkeyShimSeedSQL = `
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_upsert', 'pkg:hotkey:' || lpad(g::text, 3, '0'),
    'repo:hotkey', 'digest:hotkey', 'CVE-2026-0001', 'CVE-2026-0001'
FROM generate_series(0, 161) AS g
`

func TestSupplyChainImpactWinnersHotkeyStagingShimLive(t *testing.T) {
	ctx, database := winnersSettleGateDB(t)

	if _, err := database.ExecContext(ctx, hotkeyShimSeedSQL); err != nil {
		t.Fatalf("seed shared hot keys: %v", err)
	}

	// hotkeyShimKeys returns writer w's statement keys: the 162 shared hot
	// keys plus w's 18 private tail keys, in reverse K order so the shipped
	// statement's ORDER BY (not caller order) still governs lock uptake.
	hotkeyShimKeys := func(w int) []string {
		ks := make([]string, 0, hotkeyShimStmtKeys)
		for g := 0; g < hotkeyShimHotKeys; g++ {
			ks = append(ks, fmt.Sprintf("pkg:hotkey:%03d", g))
		}
		for g := 0; g < hotkeyShimTailKeys; g++ {
			ks = append(ks, fmt.Sprintf("pkg:hotkey-tail:%02d:%03d", w, g))
		}
		slices.Sort(ks)
		slices.Reverse(ks)
		return ks
	}
	const repos, digest, cve = "repo:hotkey", "digest:hotkey", "CVE-2026-0001"
	sized := func(n int, v string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = v
		}
		return out
	}

	// Seed every writer's private tail so the upsert cell never mixes
	// inserts with bumps (the hot keys are already seeded above).
	for w := 0; w < 4; w++ {
		pkgs := hotkeyShimKeys(w)[hotkeyShimHotKeys:]
		if _, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL,
			pkgs, sized(len(pkgs), repos), sized(len(pkgs), digest),
			sized(len(pkgs), cve), sized(len(pkgs), cve)); err != nil {
			t.Fatalf("seed writer %d tail: %v", w, err)
		}
	}

	var failed atomic.Int64
	record := func(err error) {
		if err == nil {
			return
		}
		failed.Add(1)
		t.Errorf("hotkey shim accessor failed: %v", err)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

	// runUpsertStratum hammers the SHIPPED capture upsert with writers
	// concurrent writers over the shared hot keys and returns every
	// statement latency. Same-order K-locked updates serialize through
	// waiting here (the effect the disjoint-key gate cannot see).
	runUpsertStratum := func(writers, iters int) []time.Duration {
		samples := make([][]time.Duration, writers)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for w := 0; w < writers; w++ {
			workers.Add(1)
			go func(w int) {
				defer workers.Done()
				<-start
				pkgs := hotkeyShimKeys(w)
				reposArr := sized(len(pkgs), repos)
				digestArr := sized(len(pkgs), digest)
				cveArr := sized(len(pkgs), cve)
				local := make([]time.Duration, 0, iters)
				for i := -2; i < iters; i++ {
					begin := time.Now()
					_, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL,
						pkgs, reposArr, digestArr, cveArr, cveArr)
					elapsed := time.Since(begin)
					if err != nil {
						if isDeadlock(err) {
							t.Errorf("hotkey upsert hit a lock-order cycle (40P01): %v", err)
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

	// runStagingStratum hammers the plain staging INSERT with writers
	// concurrent writers over the same shared hot keys. Each writer pins one
	// session (TEMP tables are session-scoped) and truncates before the
	// timed sample, modelling the per-tick drain: production staging never
	// accumulates across ticks.
	runStagingStratum := func(writers, iters int) []time.Duration {
		samples := make([][]time.Duration, writers)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for w := 0; w < writers; w++ {
			workers.Add(1)
			go func(w int) {
				defer workers.Done()
				conn, err := database.Conn(ctx)
				if err != nil {
					record(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if _, err := conn.ExecContext(ctx, hotkeyShimStagingDDL); err != nil {
					record(err)
					return
				}
				if _, err := conn.ExecContext(ctx, "TRUNCATE winners_staging_shim_hotkey"); err != nil {
					record(err)
					return
				}
				<-start
				pkgs := hotkeyShimKeys(w)
				reposArr := sized(len(pkgs), repos)
				digestArr := sized(len(pkgs), digest)
				cveArr := sized(len(pkgs), cve)
				exec := func() (time.Duration, error) {
					begin := time.Now()
					_, err := conn.ExecContext(ctx, hotkeyShimStagingInsertSQL,
						pkgs, reposArr, digestArr, cveArr, cveArr)
					return time.Since(begin), err
				}
				for i := 0; i < 2; i++ {
					if _, err := exec(); err != nil {
						record(err)
						return
					}
				}
				if _, err := conn.ExecContext(ctx, "TRUNCATE winners_staging_shim_hotkey"); err != nil {
					record(err)
					return
				}
				local := make([]time.Duration, 0, iters)
				for i := 0; i < iters; i++ {
					elapsed, err := exec()
					if err != nil {
						if isDeadlock(err) {
							t.Errorf("staging plain insert deadlocked (must never happen): %v", err)
						}
						record(err)
						return
					}
					local = append(local, elapsed)
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

	logStratum := func(cell string, writers int, durs []time.Duration) time.Duration {
		if failed.Load() != 0 {
			t.Fatalf("%s writers=%d: %d accessor(s) failed; the sample is incomplete", cell, writers, failed.Load())
		}
		p50 := winnersDirtyLatencyPercentile(durs, 0.5)
		p95 := winnersDirtyLatencyPercentile(durs, 0.95)
		t.Logf("hotkey_shim cell=%s writers=%d n=%d p50=%s (%.6fs) p95=%s (%.6fs) max=%s (%.6fs)",
			cell, writers, len(durs), p50, p50.Seconds(), p95, p95.Seconds(), slices.Max(durs), slices.Max(durs).Seconds())
		return p95
	}

	upsert1 := logStratum("upsert", 1, runUpsertStratum(1, 25))
	upsert2 := logStratum("upsert", 2, runUpsertStratum(2, 25))
	upsert4 := logStratum("upsert", 4, runUpsertStratum(4, 15))
	staging1 := logStratum("staging", 1, runStagingStratum(1, 25))
	staging2 := logStratum("staging", 2, runStagingStratum(2, 25))
	staging4 := logStratum("staging", 4, runStagingStratum(4, 15))
	_ = upsert1
	_ = staging1

	// collapseWave stages one full wave (162 keys x 150 marks) through the
	// same plain INSERT the staging cell times, then times repeats of the
	// GROUP BY collapse over it. wantGroups pins the conservation shape:
	// every staged mark lands in exactly one key group.
	collapseWave := func(conn *sql.Conn, distinct bool, wantGroups int64) time.Duration {
		if _, err := conn.ExecContext(ctx, hotkeyShimStagingDDL); err != nil {
			t.Fatalf("create staging scratch: %v", err)
		}
		if _, err := conn.ExecContext(ctx, "TRUNCATE winners_staging_shim_hotkey"); err != nil {
			t.Fatalf("reset staging scratch: %v", err)
		}
		pkgs := make([]string, 0, hotkeyShimStmtKeys)
		for g := 0; g < hotkeyShimHotKeys; g++ {
			pkgs = append(pkgs, fmt.Sprintf("pkg:hotkey:%03d", g))
		}
		reposArr := sized(len(pkgs), repos)
		digestArr := sized(len(pkgs), digest)
		cveArr := sized(len(pkgs), cve)
		var total int64
		for m := 0; m < hotkeyShimWaveMarks; m++ {
			use := pkgs
			ra, da, ca := reposArr, digestArr, cveArr
			if distinct {
				use = make([]string, 0, len(pkgs))
				for g := 0; g < hotkeyShimHotKeys; g++ {
					use = append(use, fmt.Sprintf("pkg:hotkey-wave:%03d:%03d", m, g))
				}
				ra, da, ca = sized(len(use), repos), sized(len(use), digest), sized(len(use), cve)
			}
			res, err := conn.ExecContext(ctx, hotkeyShimStagingInsertSQL, use, ra, da, ca, ca)
			if err != nil {
				t.Fatalf("stage wave rows: %v", err)
			}
			n, _ := res.RowsAffected()
			total += n
		}
		t.Logf("hotkey_shim wave staged rows=%d distinct=%t", total, distinct)
		var durs []time.Duration
		var groups, marks int64
		for i := 0; i < hotkeyShimCollapseSamples; i++ {
			begin := time.Now()
			rows, err := conn.QueryContext(ctx, hotkeyShimCollapseSQL)
			if err != nil {
				t.Fatalf("collapse wave: %v", err)
			}
			groups, marks = 0, 0
			for rows.Next() {
				var pkg, repo, dgst, c0, adv, first string
				var c int64
				if err := rows.Scan(&pkg, &repo, &dgst, &c0, &adv, &c, &first); err != nil {
					_ = rows.Close()
					t.Fatalf("scan collapse row: %v", err)
				}
				groups++
				marks += c
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatalf("read collapse rows: %v", err)
			}
			durs = append(durs, time.Since(begin))
		}
		if groups != wantGroups || marks != total {
			t.Fatalf("collapse conservation broken: groups=%d want %d marks=%d staged=%d",
				groups, wantGroups, marks, total)
		}
		p95 := winnersDirtyLatencyPercentile(durs, 0.95)
		t.Logf("hotkey_shim cell=collapse distinct=%t groups=%d rows=%d n=%d p95=%s (%.6fs) max=%s (%.6fs)",
			distinct, groups, total, len(durs), p95, p95.Seconds(), slices.Max(durs), slices.Max(durs).Seconds())
		return p95
	}

	collapseConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin collapse session: %v", err)
	}
	defer func() { _ = collapseConn.Close() }()
	collapseP95 := collapseWave(collapseConn, false, hotkeyShimHotKeys)

	worstConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin worst-partition session: %v", err)
	}
	defer func() { _ = worstConn.Close() }()
	worstP95 := collapseWave(worstConn, true, int64(hotkeyShimHotKeys*hotkeyShimWaveMarks))

	// Vacuum note: grow the worst-partition staging heap to 5 undrained
	// waves, report heap size, then TRUNCATE and VACUUM costs. TEMP staging
	// is drained per tick in the theory, so this bounds the neglect case.
	var sizeBefore, sizeAfterTruncate, sizeAfterVacuum int64
	if err := worstConn.QueryRowContext(ctx, "SELECT pg_total_relation_size('winners_staging_shim_hotkey')").Scan(&sizeBefore); err != nil {
		t.Fatalf("staging size before truncate: %v", err)
	}
	extraPkgs := make([]string, 0, hotkeyShimHotKeys)
	for g := 0; g < hotkeyShimHotKeys; g++ {
		extraPkgs = append(extraPkgs, fmt.Sprintf("pkg:hotkey-neglect:%03d", g))
	}
	for m := 0; m < 4*hotkeyShimWaveMarks; m++ {
		if _, err := worstConn.ExecContext(ctx, hotkeyShimStagingInsertSQL, extraPkgs,
			sized(len(extraPkgs), repos), sized(len(extraPkgs), digest),
			sized(len(extraPkgs), cve), sized(len(extraPkgs), cve)); err != nil {
			t.Fatalf("stage neglect rows: %v", err)
		}
	}
	var sizeNeglected int64
	if err := worstConn.QueryRowContext(ctx, "SELECT pg_total_relation_size('winners_staging_shim_hotkey')").Scan(&sizeNeglected); err != nil {
		t.Fatalf("staging size neglected: %v", err)
	}
	truncBegin := time.Now()
	if _, err := worstConn.ExecContext(ctx, "TRUNCATE winners_staging_shim_hotkey"); err != nil {
		t.Fatalf("truncate staging: %v", err)
	}
	truncElapsed := time.Since(truncBegin)
	if err := worstConn.QueryRowContext(ctx, "SELECT pg_total_relation_size('winners_staging_shim_hotkey')").Scan(&sizeAfterTruncate); err != nil {
		t.Fatalf("staging size after truncate: %v", err)
	}
	vacBegin := time.Now()
	if _, err := worstConn.ExecContext(ctx, "VACUUM winners_staging_shim_hotkey"); err != nil {
		t.Fatalf("vacuum staging: %v", err)
	}
	vacElapsed := time.Since(vacBegin)
	if err := worstConn.QueryRowContext(ctx, "SELECT pg_total_relation_size('winners_staging_shim_hotkey')").Scan(&sizeAfterVacuum); err != nil {
		t.Fatalf("staging size after vacuum: %v", err)
	}
	t.Logf("hotkey_shim cell=vacuum one_wave_bytes=%d five_wave_bytes=%d truncate=%s (%.6fs) truncate_bytes=%d vacuum=%s (%.6fs) vacuum_bytes=%d",
		sizeBefore, sizeNeglected, truncElapsed, truncElapsed.Seconds(), sizeAfterTruncate, vacElapsed, vacElapsed.Seconds(), sizeAfterVacuum)

	// Pre-registered verdict (ruling condition 1). Collapse cost rides with
	// every staged mark: one wave is the output of 150 mark rounds, so each
	// statement carries collapseP95/150.
	amortized := collapseP95 / hotkeyShimCollapseStatementsPerWave
	effective := staging4 + amortized
	ratio := float64(staging4) / float64(upsert4)
	t.Logf("hotkey_shim verdict upsert4_p95=%s (%.6fs) staging4_p95=%s (%.6fs) ratio=%.3f collapse_p95=%s amortized=%s effective=%s (%.6fs) envelope=%s worst_collapse_p95=%s upsert2_p95=%s staging2_p95=%s",
		upsert4, upsert4.Seconds(), staging4, staging4.Seconds(), ratio,
		collapseP95, amortized, effective, effective.Seconds(),
		hotkeyShimStagingEnvelope, worstP95, upsert2, staging2)
	if staging4 > time.Duration(hotkeyShimMaxStagingToUpsertRatio*float64(upsert4)) {
		t.Errorf("hotkey_shim KILL: staging 4-writer p95 %s is not decisively below same-run contended-upsert p95 %s (ratio %.3f above %.2f): the hot-row wait the theory avoids is not the dominant cost",
			staging4, upsert4, ratio, hotkeyShimMaxStagingToUpsertRatio)
	}
	if effective > hotkeyShimStagingEnvelope {
		t.Errorf("hotkey_shim KILL: staging 4-writer p95 %s plus amortized collapse %s = %s breaks the 3.41ms envelope %s (collapse cost included): direction C misses its budget pre-build",
			staging4, amortized, effective, hotkeyShimStagingEnvelope)
	}
}
