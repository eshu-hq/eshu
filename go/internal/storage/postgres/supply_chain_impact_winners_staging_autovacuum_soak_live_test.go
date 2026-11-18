// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Soak harness for eshu-hq/eshu#10901 box 4 (sustained autovacuum soak,
// goal-ops/ruling-10901-autovacuum-soak.md, as amended by
// goal-ops/ruling-10901-autovacuum-soak-r2.md). Manual landing-gate harness:
// deliberately NOT enrolled in live-postgres-readiness (no //eshu:readiness
// marker), because the soak is only licensed on an owned quiet host and
// shared CI runners cannot hold the entry gate.
//
// Shape: production-shape writer transactions (conflict-domain lock +
// BatchInsertVersionedFacts + production capture + owed marker + replace-set
// retract with fact_retract staging, alternating 2/4 writers per wave) with
// the collapse-in-tick loop running concurrently and a bounded final drain
// per wave, sustained until BOTH elapsed >= 5 wall minutes AND >= 4 observed
// autovacuum_naptime intervals have passed, whichever is longer.
//
// Pre-registered bounds (ruling v2, frozen before measuring, never re-tuned):
//   - paced load: wave + 2s sleep (G = 2s hardcoded; the p50 inter-wave
//     clause is struck — drain-watch snapshots lack tick granularity);
//   - in-load containment: second-half heap max <= 128MB, series max
//     dead <= 500k;
//   - recovery gate (writers stopped, quiesce window <= 120s): depth == 0,
//     dead <= 1000, vac advance >= +1 in recovery (total delta >= 2),
//     heap <= load-end high-water mark + max(2MB, 5%) one-sided;
//   - per-wave series logged every wave with staging depth back to 0;
//   - conservation exact (marks == drained version sums + residuals);
//   - zero 40P01 anywhere, no retry;
//   - entry/exit load1 < 8.0 with 500ms in-run max, control canary
//     (unchanged-code contended-upsert 4-writer p95 <= 150ms).
// A load or canary breach VOIDs the round; a bound breach KILLs it.
// No VACUUM FULL / CLUSTER anywhere: the proof stays autovacuum-only.
//
// RED-first: ESHU_SOAK_VACUUM_OFF=1 disables autovacuum on the staging table
// after bootstrap and runs the same soak. The RED invocation passes iff an
// in-load or recovery bound is breached ("RED proven"); it fails with
// "RED void" when the bounds hold with the vacuum off (bounds are too
// loose: tighten them, never waive).
// Run the RED invocation before trusting any GREEN:
//   ESHU_SOAK_VACUUM_OFF=1 go test ./internal/storage/postgres/ -run TestSupplyChainImpactWinnersStagingAutovacuumSoakLive -count=1 -timeout 30m -v
// then the GREEN invocation without the variable. Give the run a
// -timeout well above the required duration: the proof context from
// winnersSettleGateDB caps setup queries at 10 minutes.

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	// soakWaveGap paces waves to production duty cycle (ruling v2: G = 2s
	// hardcoded; the p50 inter-wave clause is struck).
	soakWaveGap = 2 * time.Second
	// soakInLoadHeapBoundBytes caps the second-half heap maximum: it
	// catches unbounded bloat without failing on legitimate truncation.
	soakInLoadHeapBoundBytes = 128 << 20
	// soakInLoadDeadBound caps the series maximum dead tuples under load.
	soakInLoadDeadBound = 500000
	// soakRecoveryWindow caps the post-load quiesce poll loop.
	soakRecoveryWindow = 120 * time.Second
	// soakRecoveryDeadBound is one vacuum threshold (migration 309),
	// applied only after the writers stop.
	soakRecoveryDeadBound = 1000
	// soakMinVacuumCount is the repeat proof across the whole run:
	// at least one firing in-load and one in recovery.
	soakMinVacuumCount = 2
	// soakMinDuration and soakMinNaptimeIntervals are the duration floor,
	// whichever is longer once naptime is read from pg_settings at runtime.
	soakMinDuration         = 5 * time.Minute
	soakMinNaptimeIntervals = 4
	// soakMinWaves guards against degenerate sub-second waves satisfying
	// the clock without exercising the daemon.
	soakMinWaves = 4
	// soakDrainLimit bounds the per-wave final drain loop.
	soakDrainLimit = 20
	// soakPassesPerWave and the hot/tail split mirror the interleave gate.
	soakPassesPerWave = 8
	soakHotPerPass    = 32
	soakHotKeys       = 120
	soakTailPerWriter = 8
)

// soakVacuumStats is one per-wave snapshot of the daemon's view of staging.
type soakVacuumStats struct {
	depth      int64
	heapBytes  int64
	deadTup    int64
	lastVacuum sql.NullString
	vacCount   int64
	elapsed    time.Duration
}

func TestSupplyChainImpactWinnersStagingAutovacuumSoakLive(t *testing.T) {
	// Entry gate: the host must be quiet BEFORE the disposable database
	// exists. A breach here voids before measuring.
	loadStart := quietReproveLoad1(t)
	t.Logf("soak load_start=%.2f ceil=%.1f ncpu=16", loadStart, quietReproveLoadCeil)
	if loadStart >= quietReproveLoadCeil {
		t.Errorf("soak VOID: load1 %.2f at start reaches ceiling %.1f: host is not quiet, no numbers published",
			loadStart, quietReproveLoadCeil)
		return
	}

	done := make(chan struct{})
	var loadMaxBits atomic.Uint64
	loadMaxBits.Store(math.Float64bits(loadStart))
	go quietReproveLoadWatch(done, &loadMaxBits)
	defer close(done)

	redMode := os.Getenv("ESHU_SOAK_VACUUM_OFF") == "1"

	ctx, database := winnersSettleGateDB(t)
	if redMode {
		if _, err := database.ExecContext(ctx, `ALTER TABLE supply_chain_impact_winners_dirty_staging SET (autovacuum_enabled = false)`); err != nil {
			t.Fatalf("soak RED setup: disable staging autovacuum: %v", err)
		}
		t.Logf("soak RED mode: staging autovacuum disabled; the heap/dead bounds must breach")
	}

	// Bootstrap seeds can stage residue; drain it once. The conservation
	// baseline freezes later, after the canary (whose direct log upserts
	// are not soak marks).
	soakDrainWave(t, ctx, database, -1)

	// Read the daemon's cadence from the server: the required duration is
	// max(5 minutes, 4 observed naptime intervals), never a hard-coded guess.
	var naptimeSec float64
	if err := database.QueryRowContext(ctx, `SELECT setting::float8 FROM pg_settings WHERE name = 'autovacuum_naptime'`).Scan(&naptimeSec); err != nil {
		t.Fatalf("soak read autovacuum_naptime: %v", err)
	}
	required := soakMinDuration
	if byNaptime := time.Duration(float64(soakMinNaptimeIntervals)) * time.Duration(naptimeSec*float64(time.Second)); byNaptime > required {
		required = byNaptime
	}
	if required > 9*time.Minute {
		t.Fatalf("soak VOID: naptime %.0fs makes the required duration %s exceed the proof context; lower naptime or raise the context, no numbers published",
			naptimeSec, required)
	}
	t.Logf("soak plan: naptime=%.0fs required=%s red=%t", naptimeSec, required, redMode)

	canaryP95 := soakCanary(t, ctx, database)
	t.Logf("soak canary upsert4 p95=%s bound=%s", canaryP95, quietReproveCanaryBound)
	if canaryP95 > quietReproveCanaryBound {
		t.Errorf("soak VOID: control canary upsert4 p95 %s breaks pre-registered bound %s: host went noisy, no numbers published",
			canaryP95, quietReproveCanaryBound)
		return
	}

	// Freeze the conservation baseline AFTER the canary: the canary's direct
	// log upserts (162 distinct keys) are not soak marks and must not
	// contaminate the wave accounting below.
	soakDrainWave(t, ctx, database, -2)
	baseVersions, baseDepth := winnersLogVersionsOf(t, ctx, database)

	var failed atomic.Int64
	var marks atomic.Int64
	var mu sync.Mutex
	distinct := map[string]struct{}{}
	noteKeys := func(pkgs []string) {
		mu.Lock()
		for _, k := range pkgs {
			distinct[k] = struct{}{}
		}
		mu.Unlock()
	}

	start := time.Now()
	series := make([]soakVacuumStats, 0, 32)
	wave := 0
	for {
		writers := 2
		if wave%2 == 1 {
			writers = 4
		}
		scopes := writers / 2
		type domain struct {
			scopeID, genID, itemID string
			claimedAt              time.Time
			token                  atomic.Int64
		}
		domains := make([]domain, scopes)
		for s := 0; s < scopes; s++ {
			scopeID, genID, itemID, claimedAt := soakSetupScope(t, ctx, database, wave, s)
			domains[s] = domain{scopeID: scopeID, genID: genID, itemID: itemID, claimedAt: claimedAt}
		}
		if got := winnersStagingDepthOf(t, ctx, database); got != 0 {
			t.Fatalf("soak wave %d: setup staged %d rows, want none", wave, got)
		}

		tails := make([][]string, writers)
		for w := 0; w < writers; w++ {
			for i := 0; i < soakTailPerWriter; i++ {
				tails[w] = append(tails[w], fmt.Sprintf("pkg:soak-tail:%d:%d:%02d", wave, w, i))
			}
		}
		stop := make(chan struct{})
		var collapseWorker sync.WaitGroup
		collapseWorker.Add(1)
		go func() {
			defer collapseWorker.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tx, err := (SQLDB{DB: database}).Begin(ctx)
				if err != nil {
					failed.Add(1)
					return
				}
				if _, err := collapseWinnersStaging(ctx, tx, 0); err != nil {
					_ = tx.Rollback()
					if strings.Contains(err.Error(), "40P01") {
						t.Errorf("soak wave %d collapse-in-tick hit a lock-order cycle (40P01): %v", wave, err)
					}
					failed.Add(1)
					return
				}
				if err := tx.Commit(); err != nil {
					if strings.Contains(err.Error(), "40P01") {
						t.Errorf("soak wave %d collapse-in-tick commit hit a lock-order cycle (40P01): %v", wave, err)
					}
					failed.Add(1)
					return
				}
			}
		}()

		var writersDone sync.WaitGroup
		for w := 0; w < writers; w++ {
			writersDone.Add(1)
			go func(w int) {
				defer writersDone.Done()
				d := &domains[w%scopes]
				for p := 0; p < soakPassesPerWave; p++ {
					hot := make([]string, 0, soakHotPerPass)
					for i := 0; i < soakHotPerPass; i++ {
						hot = append(hot, fmt.Sprintf("pkg:soak-hot:%03d", (p*soakHotPerPass+w*7+i)%soakHotKeys))
					}
					noteKeys(hot)
					noteKeys(tails[w])
					token := d.token.Add(1)
					marks.Add(soakRunPass(t, ctx, database, &failed, w, token, d.scopeID, d.genID, d.itemID, d.claimedAt, hot, tails[w]))
					if failed.Load() != 0 {
						return
					}
				}
			}(w)
		}
		writersDone.Wait()
		close(stop)
		collapseWorker.Wait()
		soakDrainWave(t, ctx, database, wave)

		s := soakReadVacuumStats(t, ctx, database, start)
		series = append(series, s)
		t.Logf("soak wave=%d writers=%d marks=%d depth=%d heap=%d dead=%d vac_count=%d last_vacuum=%q elapsed=%s",
			wave, writers, marks.Load(), s.depth, s.heapBytes, s.deadTup, s.vacCount, s.lastVacuum.String, s.elapsed)
		if s.depth != 0 {
			t.Fatalf("soak wave %d KILL: staging holds %d rows after the drain; steady state requires 0", wave, s.depth)
		}
		wave++
		if time.Since(start) >= required && wave >= soakMinWaves {
			break
		}
		time.Sleep(soakWaveGap)
	}
	if failed.Load() != 0 {
		t.Fatalf("soak KILL: %d accessor(s) failed with no retry; see errors above", failed.Load())
	}

	loadEnd := quietReproveLoad1(t)
	loadMax := math.Float64frombits(loadMaxBits.Load())
	t.Logf("soak load_start=%.2f load_end=%.2f load_max=%.2f ceil=%.1f",
		loadStart, loadEnd, loadMax, quietReproveLoadCeil)

	versions, depth := winnersLogVersionsOf(t, ctx, database)
	t.Logf("soak totals waves=%d marks=%d log_version_growth=%d log_depth_growth=%d distinct_keys=%d elapsed=%s",
		wave, marks.Load(), versions-baseVersions, depth-baseDepth, len(distinct), time.Since(start))
	if versions-baseVersions != marks.Load() {
		t.Errorf("soak KILL: log version growth = %d, want %d staged marks", versions-baseVersions, marks.Load())
	}
	if depth-baseDepth != int64(len(distinct)) {
		t.Errorf("soak KILL: log depth growth = %d, want %d distinct staged keys", depth-baseDepth, len(distinct))
	}

	// Grade the load series against the frozen in-load bounds. Flags only;
	// GREEN reports KILLs below, RED requires at least one breach.
	var heapBreached, deadBreached bool
	secondHalf := series[len(series)/2:]
	var heapMax, deadMax int64
	for _, s := range series {
		if s.deadTup > deadMax {
			deadMax = s.deadTup
		}
	}
	for _, s := range secondHalf {
		if s.heapBytes > heapMax {
			heapMax = s.heapBytes
		}
	}
	if heapMax > soakInLoadHeapBoundBytes {
		heapBreached = true
	}
	if deadMax > soakInLoadDeadBound {
		deadBreached = true
	}
	loadEndHeap := series[len(series)-1].heapBytes
	loadEndVac := series[len(series)-1].vacCount
	inLoadVacDelta := loadEndVac - series[0].vacCount
	inLoadVacOK := inLoadVacDelta >= 1

	// Recovery: writers stopped. Poll until the daemon proves itself or the
	// quiesce window expires; break early on the full recovery state.
	var rec soakVacuumStats
	for i := 0; i < int(soakRecoveryWindow/(5*time.Second)); i++ {
		time.Sleep(5 * time.Second)
		rec = soakReadVacuumStats(t, ctx, database, start)
		t.Logf("soak recovery poll=%d depth=%d heap=%d dead=%d vac_count=%d last_vacuum=%q elapsed=%s",
			i, rec.depth, rec.heapBytes, rec.deadTup, rec.vacCount, rec.lastVacuum.String, rec.elapsed)
		if rec.depth == 0 && rec.deadTup <= soakRecoveryDeadBound && rec.vacCount > loadEndVac {
			break
		}
	}
	heapMargin := int64(2 << 20)
	if fivePct := loadEndHeap * 5 / 100; fivePct > heapMargin {
		heapMargin = fivePct
	}
	heapHeld := rec.heapBytes <= loadEndHeap+heapMargin
	vacRecoveryDelta := rec.vacCount - loadEndVac
	recoveryOK := rec.depth == 0 && rec.deadTup <= soakRecoveryDeadBound && vacRecoveryDelta >= 1 && heapHeld
	totalVacDelta := rec.vacCount - series[0].vacCount

	if redMode {
		// Conservation, deadlock, load, and canary failures above are real
		// bugs in either mode. The RED proof is the breach itself.
		if heapBreached || deadBreached || !recoveryOK {
			t.Logf("soak RED proven: bound bites with autovacuum disabled (in-load heap %d dead %d, recovery depth %d dead %d vac_delta %d heap_held %t)",
				heapMax, deadMax, rec.depth, rec.deadTup, vacRecoveryDelta, heapHeld)
			return
		}
		t.Fatalf("soak RED void: pre-registered bounds held with autovacuum disabled; bounds are too loose, tighten them")
		return
	}

	if heapBreached {
		t.Errorf("soak KILL: second-half heap max %d bytes breaks in-load bound %d", heapMax, soakInLoadHeapBoundBytes)
	}
	if deadBreached {
		t.Errorf("soak KILL: series max n_dead_tup %d breaks in-load bound %d", deadMax, soakInLoadDeadBound)
	}
	if !inLoadVacOK {
		t.Errorf("soak KILL: in-load autovacuum_count delta %d below 1: the daemon did not fire under load", inLoadVacDelta)
	}
	if !recoveryOK {
		t.Errorf("soak KILL: recovery incomplete after %s quiesce: depth %d (want 0), dead %d (want <= %d), vac recovery delta %d (want >= 1), heap %d vs cap %d (load-end %d)",
			soakRecoveryWindow, rec.depth, rec.deadTup, soakRecoveryDeadBound, vacRecoveryDelta, rec.heapBytes, loadEndHeap+heapMargin, loadEndHeap)
	}
	if totalVacDelta < soakMinVacuumCount {
		t.Errorf("soak KILL: total autovacuum_count delta %d below %d: the daemon did not fire repeatedly", totalVacDelta, soakMinVacuumCount)
	}

	if loadEnd >= quietReproveLoadCeil || loadMax >= quietReproveLoadCeil {
		t.Errorf("soak VOID: load1 reached %.2f (end %.2f, max %.2f, ceil %.1f): host went noisy, no numbers published",
			math.Max(loadStart, math.Max(loadEnd, loadMax)), loadEnd, loadMax, quietReproveLoadCeil)
	}
}
