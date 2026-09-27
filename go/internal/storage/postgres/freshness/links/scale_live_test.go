// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// scaleDSNEnv names a database loaded by
// docs/internal/evidence/7127-link-writer-fixture.sql. The scale test is the
// measurement half of docs/internal/evidence/7127-link-writer-scale.py, which
// samples backend RssAnon around the windows this test prints.
const scaleDSNEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_DSN"

// scaleTargets are the four 1.0x scopes of the fixture.
var scaleTargets = []string{
	"git-repository-scope:repository:r_t1", "git-repository-scope:repository:r_t2",
	"git-repository-scope:repository:r_t3", "git-repository-scope:repository:r_t4",
}

// scaleGeneration mirrors the fixture's gid(): md5(scope || label) || md5(label || scope).
func scaleGeneration(t *testing.T, ctx context.Context, raw *sql.DB, scopeID, label string) string {
	t.Helper()
	var id string
	if err := raw.QueryRowContext(ctx, `SELECT md5($1 || $2) || md5($2 || $1)`, scopeID, label).Scan(&id); err != nil {
		t.Fatalf("generation id: %v", err)
	}
	return id
}

func emit(t *testing.T, record map[string]any) {
	t.Helper()
	record["unix"] = float64(time.Now().UnixNano()) / 1e9
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	fmt.Println("EVIDENCE " + string(b))
}

func hostLoad1() float64 {
	var out []byte
	var err error
	if runtime.GOOS == "darwin" {
		out, err = exec.Command("sysctl", "-n", "vm.loadavg").Output()
	} else {
		out, err = os.ReadFile("/proc/loadavg")
	}
	if err != nil {
		return -1
	}
	fields := strings.Fields(strings.Trim(string(out), "{} \n"))
	if len(fields) == 0 {
		return -1
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func tempStats(t *testing.T, ctx context.Context, raw *sql.DB) (files, bytes int64) {
	t.Helper()
	time.Sleep(1500 * time.Millisecond) // stats flush at most once a second
	if err := raw.QueryRowContext(ctx, `SELECT pg_stat_clear_snapshot(), temp_files, temp_bytes
FROM pg_stat_database WHERE datname = current_database()`).Scan(new(any), &files, &bytes); err != nil {
		t.Fatalf("temp stats: %v", err)
	}
	return files, bytes
}

func resetScaleScopes(t *testing.T, ctx context.Context, raw *sql.DB, scopes []string) {
	t.Helper()
	for _, table := range []string{
		"changed_since_link_deltas", "changed_since_link_bucket_counts", "changed_since_links",
		"changed_since_key_state", "changed_since_activations", "changed_since_scope_cursor",
	} {
		if _, err := raw.ExecContext(ctx, `DELETE FROM `+table+` WHERE scope_id = ANY($1)`, scopes); err != nil {
			t.Fatalf("reset %s: %v", table, err)
		}
	}
	for _, scopeID := range scopes {
		for _, label := range []string{"F0", "F1"} {
			if _, err := raw.ExecContext(ctx, `INSERT INTO changed_since_activations
    (scope_id, generation_id, prior_generation_id, source, activated_at) VALUES ($1, $2, NULL, 'backfill', now())`,
				scopeID, scaleGeneration(t, ctx, raw, scopeID, label)); err != nil {
				t.Fatalf("journal: %v", err)
			}
		}
	}
}

// bareAggregateSQL is the shim's bare_b: the link statement's aggregate of
// the activating generation with no writes, derived from the shipped
// statement so it cannot drift from it.
func bareAggregateSQL(t *testing.T) string {
	t.Helper()
	cut := strings.Index(linksfreshnessstore.IncrementalLinkSQL, ",\ndiff AS MATERIALIZED")
	if cut < 0 {
		t.Fatal("diff CTE boundary not found in IncrementalLinkSQL")
	}
	return linksfreshnessstore.IncrementalLinkSQL[:cut] + "\nSELECT count(*), count(state) FROM cur"
}

// TestLinkScaleEvidence measures gates G5-G8 of #7127 ruling 8.6 on the 1.0x
// fixture: temp files and link walls for 1, 2 and 4 concurrent incremental
// links with a read probe alongside, and interleaved L1b-against-bare_b
// rounds with the host load recorded per round.
func TestLinkScaleEvidence(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(scaleDSNEnv))
	if dsn == "" {
		t.Skipf("set %s to a database loaded by docs/internal/evidence/7127-link-writer-fixture.sql", scaleDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(16)
	store := postgres.SQLDB{DB: raw}
	emit(t, map[string]any{"event": "start", "cpus": runtime.NumCPU(), "load1": hostLoad1()})

	for _, n := range []int{1, 2, 4} {
		scopes := scaleTargets[:n]
		resetScaleScopes(t, ctx, raw, scopes)
		writer := linksfreshnessstore.NewLinkWriter(store)
		writer.Slots = 4
		files0, bytes0 := tempStats(t, ctx, raw)
		for _, scopeID := range scopes {
			res, err := writer.LinkNext(ctx, scopeID)
			if err != nil || res.Kind != linksfreshnessstore.LinkKindRoot {
				t.Fatalf("root %s: %+v %v", scopeID, res, err)
			}
			emit(t, map[string]any{"event": "root", "n": n, "seconds": res.Duration.Seconds(), "keys": res.Keys})
		}
		probeStop := make(chan struct{})
		var probe []float64
		var probeWG sync.WaitGroup
		probeWG.Add(1)
		go func() {
			defer probeWG.Done()
			for {
				select {
				case <-probeStop:
					return
				default:
				}
				began := time.Now()
				var c int64
				if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM fact_records
WHERE scope_id = 'git-repository-scope:repository:r_noise1' AND generation_id = md5('noise1' || 'b') || md5('b' || 'noise1')
LIMIT 2000) AS page`).Scan(&c); err == nil {
					probe = append(probe, time.Since(began).Seconds())
				}
				time.Sleep(200 * time.Millisecond)
			}
		}()
		emit(t, map[string]any{"event": "window_start", "n": n, "load1": hostLoad1()})
		var wg sync.WaitGroup
		results := make([]linksfreshnessstore.LinkResult, n)
		errs := make([]error, n)
		for i, scopeID := range scopes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = writer.LinkNext(ctx, scopeID)
			}()
		}
		wg.Wait()
		emit(t, map[string]any{"event": "window_end", "n": n})
		close(probeStop)
		probeWG.Wait()
		files1, bytes1 := tempStats(t, ctx, raw)
		walls := []float64{}
		for i := range results {
			if errs[i] != nil || results[i].Kind != linksfreshnessstore.LinkKindIncremental {
				t.Fatalf("n=%d incremental %d: %+v %v", n, i, results[i], errs[i])
			}
			walls = append(walls, results[i].Duration.Seconds())
		}
		sort.Float64s(probe)
		probeP50, probeP95 := 0.0, 0.0
		if len(probe) > 0 {
			probeP50, probeP95 = probe[len(probe)/2], probe[len(probe)*95/100]
		}
		emit(t, map[string]any{
			"event": "incremental", "n": n, "walls_seconds": walls,
			"delta_rows": results[0].DeltaRows, "keys": results[0].Keys,
			"temp_files": files1 - files0, "temp_bytes": bytes1 - bytes0,
			"probe_samples": len(probe), "probe_p50_seconds": probeP50, "probe_p95_seconds": probeP95,
		})
	}

	// G7: interleaved rounds of bare_b and the L1b statement at 256MB, both
	// rolled back, first mover alternating. The state stands at F1 after the
	// n=4 phase, so re-root the first target at F0 once.
	target := scaleTargets[0]
	resetScaleScopes(t, ctx, raw, []string{target})
	writer := linksfreshnessstore.NewLinkWriter(store)
	if _, err := writer.LinkNext(ctx, target); err != nil {
		t.Fatalf("re-root: %v", err)
	}
	f0, f1 := scaleGeneration(t, ctx, raw, target, "F0"), scaleGeneration(t, ctx, raw, target, "F1")
	bare := bareAggregateSQL(t)
	rounds, _ := strconv.Atoi(os.Getenv("ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS"))
	if rounds <= 0 {
		rounds = 6
	}
	for round := range rounds {
		load := hostLoad1()
		timed := map[string]float64{}
		order := []string{"bare_b", "l1b"}
		if round%2 == 1 {
			order = []string{"l1b", "bare_b"}
		}
		for _, name := range order {
			tx, err := raw.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			for _, s := range []string{`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`} {
				if _, err := tx.ExecContext(ctx, s); err != nil {
					t.Fatalf("%s: %v", s, err)
				}
			}
			began := time.Now()
			if name == "bare_b" {
				_, err = tx.ExecContext(ctx, bare, target, f1)
			} else {
				_, err = tx.ExecContext(ctx, linksfreshnessstore.IncrementalLinkSQL, target, f1, f0,
					linksfreshnessstore.DigestVersion, time.Now())
			}
			timed[name] = time.Since(began).Seconds()
			_ = tx.Rollback()
			if err != nil {
				t.Fatalf("round %d %s: %v", round, name, err)
			}
		}
		emit(t, map[string]any{
			"event": "g7_round", "round": round, "first": order[0], "load1_at_start": load,
			"valid":          load >= 0 && load < float64(runtime.NumCPU()),
			"bare_b_seconds": timed["bare_b"], "l1b_seconds": timed["l1b"], "ratio": timed["l1b"] / timed["bare_b"],
		})
	}
}
