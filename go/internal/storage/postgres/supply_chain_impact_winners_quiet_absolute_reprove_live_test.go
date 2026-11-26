// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Quiet-host absolute re-prove for #10901 (adjudication condition 3,
// goal-ops/ruling-10837-adjudication.md). Manual landing-gate harness:
// deliberately NOT enrolled in live-postgres-readiness (no //eshu:readiness
// marker), because the absolute arm is only licensed on an owned quiet host
// and a shared CI runner cannot hold the entry gate.
//
// Shape: the committed shim
// (supply_chain_impact_winners_hotkey_staging_shim_live_test.go) with the
// adjudication's proof-plan rules applied: shared 162 hot keys, 4-writer
// stratum primary, staging plain-INSERT cell vs contended-upsert cell plus
// amortized GROUP BY collapse, every gated cell at n=200 (4 writers x 50
// iters; nearest-rank p95 of 60 is the fragile 4th order statistic).
//
// Pre-registered bounds (fixed here before measuring, never re-tuned):
//   - effective p95 (staging + amortized collapse) <= 3.41ms
//     (hotkeyShimStagingEnvelope).
//   - staging/upsert ratio <= 0.80 (hotkeyShimMaxStagingToUpsertRatio).
//   - control canary: contended-upsert 4-writer p95 <= 150ms (2x the 72.5ms
//     measured on the noisy shared shim host; a quiet host must beat the
//     noisy-host number, so a breach means the host is too noisy to
//     license absolute numbers).
//   - load1 < half the CPUs (8 on a 16-CPU host) at start, end, and
//     in-run maximum, sampled from /proc/loadavg.
//
// Fail-closed: a load or canary breach VOIDs the round (t.Errorf VOID, no
// verdict numbers published); an envelope or ratio breach KILLs the theory.
// complete=true needs a full round that passes every gate on a quiet host.

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	// quietReproveItersPerWriter times 4 writers gives n=200 per gated cell.
	quietReproveItersPerWriter = 50
	// quietReproveCollapseSamples sizes the GROUP BY collapse cell at the
	// same n as the writer cells so its p95 carries the same rank.
	quietReproveCollapseSamples = 200
	// quietReproveCanaryBound is the pre-registered control-canary ceiling
	// on the unchanged-code contended-upsert 4-writer p95. Rationale: the
	// shared-host shim measured 72.5ms for this exact cell on a host that
	// was NOT quiet; doubling it leaves headroom for a quiet host while
	// voiding any round run on a pathologically noisy one.
	quietReproveCanaryBound = 150 * time.Millisecond
	// quietReproveLoadCeil is half the CPUs of the 16-CPU proof host.
	// A round that touches it at any sample is void, not published.
	quietReproveLoadCeil = 8.0
)

// quietReproveLoad1 reads the 1-minute load average from /proc/loadavg and
// fails closed when the host cannot prove it is quiet. A platform without
// /proc/loadavg (e.g. macOS) skips instead: there is no load signal to gate
// on, and a skip is honest where a pass would be unproven. Present-but-bad
// content still fails closed below.
func quietReproveLoad1(t *testing.T) float64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("quiet re-prove SKIP: no /proc/loadavg on this platform; the quiet-host proof needs Linux: %v", err)
		}
		t.Fatalf("quiet re-prove VOID: cannot read /proc/loadavg: %v", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 1 {
		t.Fatalf("quiet re-prove VOID: unparsable /proc/loadavg: %q", raw)
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		t.Fatalf("quiet re-prove VOID: unparsable load1 %q: %v", fields[0], err)
	}
	return load
}

// quietReproveLoadWatch samples load1 every 500ms until done closes and
// reports the in-run maximum. It never fails the test itself; the caller
// voids the round when the maximum reaches the ceiling.
func quietReproveLoadWatch(done <-chan struct{}, max *atomic.Uint64) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			raw, err := os.ReadFile("/proc/loadavg")
			if err != nil {
				continue
			}
			fields := strings.Fields(string(raw))
			if len(fields) < 1 {
				continue
			}
			load, err := strconv.ParseFloat(fields[0], 64)
			if err != nil {
				continue
			}
			for {
				cur := math.Float64frombits(max.Load())
				if load <= cur {
					break
				}
				if max.CompareAndSwap(math.Float64bits(cur), math.Float64bits(load)) {
					break
				}
			}
		}
	}
}

func TestSupplyChainImpactWinnersQuietAbsoluteReproveLive(t *testing.T) {
	// Entry gate (proof-plan rule 1): the host must be quiet BEFORE the
	// disposable database exists. A breach here voids before measuring.
	loadStart := quietReproveLoad1(t)
	t.Logf("quiet_reprove load_start=%.2f ceil=%.1f ncpu=%d", loadStart, quietReproveLoadCeil, 16)
	if loadStart >= quietReproveLoadCeil {
		t.Errorf("quiet re-prove VOID: load1 %.2f at start reaches ceiling %.1f: host is not quiet, no numbers published",
			loadStart, quietReproveLoadCeil)
		return
	}

	done := make(chan struct{})
	var loadMaxBits atomic.Uint64
	loadMaxBits.Store(math.Float64bits(loadStart))
	go quietReproveLoadWatch(done, &loadMaxBits)
	defer close(done)

	ctx, database := winnersSettleGateDB(t)

	if _, err := database.ExecContext(ctx, hotkeyShimSeedSQL); err != nil {
		t.Fatalf("seed shared hot keys: %v", err)
	}

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

	for w := 0; w < 4; w++ {
		pkgs := hotkeyShimKeys(w)[hotkeyShimHotKeys:]
		if _, err := database.ExecContext(ctx, hotkeyShimDirectUpsertSQL,
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
		t.Errorf("quiet re-prove accessor failed: %v", err)
	}
	isDeadlock := func(err error) bool {
		return err != nil && (strings.Contains(err.Error(), "40P01") ||
			strings.Contains(err.Error(), "deadlock detected"))
	}

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
					_, err := database.ExecContext(ctx, hotkeyShimDirectUpsertSQL,
						pkgs, reposArr, digestArr, cveArr, cveArr)
					elapsed := time.Since(begin)
					if err != nil {
						if isDeadlock(err) {
							t.Errorf("quiet re-prove upsert hit a lock-order cycle (40P01): %v", err)
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
							t.Errorf("quiet re-prove staging plain insert deadlocked (must never happen): %v", err)
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

	// collapseCell stages one full wave (162 keys x 150 marks) through the
	// same plain INSERT the staging cell times, then times repeats of the
	// GROUP BY collapse over it. wantGroups pins conservation: every staged
	// mark lands in exactly one key group.
	collapseCell := func(conn *sql.Conn, distinct bool, wantGroups int64, samples int) time.Duration {
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
		t.Logf("quiet_reprove wave staged rows=%d distinct=%t", total, distinct)
		var durs []time.Duration
		var groups, marks int64
		for i := 0; i < samples; i++ {
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
		return winnersDirtyLatencyPercentile(durs, 0.95)
	}

	upsertDurs := runUpsertStratum(4, quietReproveItersPerWriter)
	stagingDurs := runStagingStratum(4, quietReproveItersPerWriter)
	if failed.Load() != 0 {
		t.Fatalf("quiet re-prove VOID: %d accessor(s) failed; the sample is incomplete, no numbers published", failed.Load())
	}
	if len(upsertDurs) != 4*quietReproveItersPerWriter || len(stagingDurs) != 4*quietReproveItersPerWriter {
		t.Fatalf("quiet re-prove VOID: short sample upsert=%d staging=%d want=%d; no numbers published",
			len(upsertDurs), len(stagingDurs), 4*quietReproveItersPerWriter)
	}

	collapseConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin collapse session: %v", err)
	}
	defer func() { _ = collapseConn.Close() }()
	collapseP95 := collapseCell(collapseConn, false, hotkeyShimHotKeys, quietReproveCollapseSamples)

	worstConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin worst-partition session: %v", err)
	}
	defer func() { _ = worstConn.Close() }()
	worstP95 := collapseCell(worstConn, true, int64(hotkeyShimHotKeys*hotkeyShimWaveMarks), quietReproveCollapseSamples)

	loadEnd := quietReproveLoad1(t)
	loadMax := math.Float64frombits(loadMaxBits.Load())
	t.Logf("quiet_reprove load_start=%.2f load_end=%.2f load_max=%.2f ceil=%.1f",
		loadStart, loadEnd, loadMax, quietReproveLoadCeil)

	// Gate order: load, then canary, then theory. VOID rounds publish no
	// verdict numbers; only the load and canary evidence above travels.
	if loadEnd >= quietReproveLoadCeil || loadMax >= quietReproveLoadCeil {
		t.Errorf("quiet re-prove VOID: load1 reached %.2f (end %.2f, max %.2f, ceil %.1f): host went noisy, no numbers published",
			max3(loadStart, loadEnd, loadMax), loadEnd, loadMax, quietReproveLoadCeil)
		return
	}

	upsert4 := winnersDirtyLatencyPercentile(upsertDurs, 0.95)
	if upsert4 > quietReproveCanaryBound {
		t.Errorf("quiet re-prove VOID: control canary upsert4 p95 %s breaks pre-registered bound %s: host went noisy, no numbers published",
			upsert4, quietReproveCanaryBound)
		return
	}
	t.Logf("quiet_reprove canary upsert4 n=%d p95=%s (%.6fs) bound=%s PASS",
		len(upsertDurs), upsert4, upsert4.Seconds(), quietReproveCanaryBound)

	staging4 := winnersDirtyLatencyPercentile(stagingDurs, 0.95)
	amortized := collapseP95 / hotkeyShimCollapseStatementsPerWave
	effective := staging4 + amortized
	ratio := float64(staging4) / float64(upsert4)
	t.Logf("quiet_reprove verdict upsert4_p95=%s (%.6fs) staging4_p95=%s (%.6fs) ratio=%.3f collapse_p95=%s amortized=%s effective=%s (%.6fs) envelope=%s worst_collapse_p95=%s",
		upsert4, upsert4.Seconds(), staging4, staging4.Seconds(), ratio,
		collapseP95, amortized, effective, effective.Seconds(),
		hotkeyShimStagingEnvelope, worstP95)
	if staging4 > time.Duration(hotkeyShimMaxStagingToUpsertRatio*float64(upsert4)) {
		t.Errorf("quiet re-prove KILL: staging 4-writer p95 %s is not decisively below same-run contended-upsert p95 %s (ratio %.3f above %.2f)",
			staging4, upsert4, ratio, hotkeyShimMaxStagingToUpsertRatio)
	}
	if effective > hotkeyShimStagingEnvelope {
		t.Errorf("quiet re-prove KILL: staging 4-writer p95 %s plus amortized collapse %s = %s breaks the 3.41ms envelope %s",
			staging4, amortized, effective, hotkeyShimStagingEnvelope)
	}
}

func max3(a, b, c float64) float64 {
	return max(a, max(b, c))
}
