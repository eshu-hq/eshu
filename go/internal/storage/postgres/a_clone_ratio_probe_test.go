// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Clone-ratio probe for #10148: pure measurement, no production change.
//
// The reducer contention gate spends about 61% of its tests phase applying the
// full bootstrap to fresh databases. This probe measures, on the real runner,
// whether CREATE DATABASE ... TEMPLATE <bootstrapped database> is cheap enough
// to replace those bootstraps, for both flavours the gate builds today:
// "public" (all migrations, database public schema) and "isolated" (the
// public-only migrations skipped, tables in a schema that heads the
// search_path). The gate's -run filter is a prefix match, so this name runs in
// the existing job without a workflow edit, and the file name sorts before
// every other test file so its output survives a step-timeout kill.
//
// Every result is a log line starting with PROBE10148 and then key=value
// pairs; kind=verdict carries the decision rule from the arbiter ruling
// (r <= 0.35 on both flavours: GO_FULL; r <= 0.60: CLONE_CAP_RELIEF_ONLY;
// above that or unequal catalogs: STOP; any failed sample: INVALID_SAMPLES,
// with the isolated input the worse of the ruled and shared-schema tiers).
// ESHU_PROBE_10148_SAMPLES overrides
// the sample count for local runs only; the env and verdict lines print n.
const (
	probeSampleDefault  = 20
	probeFsyncSamples   = 20
	probeConcurrentSize = 4
	probeConcurrentRuns = 3
	probeSchema         = "probe10148_iso"
)

// probeEnv carries the administrative connection settings every arm shares.
type probeEnv struct {
	dsn string
	cfg *pgx.ConnConfig
}

// probeSample is one timed arm run. work covers the arm itself (create, connect,
// apply or clone, first query); total adds the teardown that follows it.
type probeSample struct {
	work, apply, total time.Duration
}

// probeBuilt is a bootstrapped database with its catalog fingerprint taken
// while connected. release closes its pools so it can serve as a template.
type probeBuilt struct {
	name    string
	release func()
	catalog map[string][]string
	applied int
}

type probeArm struct {
	name string
	run  func(*testing.T) probeSample
}

func probeLine(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf("PROBE10148 "+format, args...)
}

func probeMS(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64)
}

func probeQuote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func probeDatabaseName(t *testing.T) string {
	t.Helper()
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("random database name: %v", err)
	}
	return "eshu_probe10148_" + hex.EncodeToString(random)
}

func probeSQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// connect opens a pool on database with the writer connect option every
// service runtime uses and the given search_path, like the disposable helper.
func (e probeEnv) connect(database, searchPath string) *sql.DB {
	c := e.cfg.Copy()
	c.Database = database
	c.RuntimeParams = map[string]string{"search_path": searchPath}
	return stdlib.OpenDB(*c, inventory.WriterConnectOption())
}

// createClone opens a fresh administrative session and runs CREATE DATABASE
// ... TEMPLATE with the default strategy. It is safe to call from goroutines.
func (e probeEnv) createClone(ctx context.Context, template, name string) error {
	admin := stdlib.OpenDB(*e.cfg.Copy())
	defer func() { _ = admin.Close() }()
	if err := admin.PingContext(ctx); err != nil {
		return fmt.Errorf("ping administrative database: %w", err)
	}
	_, err := admin.ExecContext(ctx, "CREATE DATABASE "+probeQuote(name)+" TEMPLATE "+probeQuote(template))
	return err
}

func (e probeEnv) dropDatabase(name string) {
	admin := stdlib.OpenDB(*e.cfg.Copy())
	defer func() { _ = admin.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+probeQuote(name)+" WITH (FORCE)")
}

// quiesce waits until no session is connected to name, which CREATE DATABASE
// ... TEMPLATE requires of its template (SQLSTATE 55006 otherwise).
func (e probeEnv) quiesce(ctx context.Context, t *testing.T, name string) {
	t.Helper()
	admin := stdlib.OpenDB(*e.cfg.Copy())
	defer func() { _ = admin.Close() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var n int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=$1`, name).Scan(&n); err != nil {
			t.Fatalf("count template sessions: %v", err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("template %s still has %d sessions after 15s", name, n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// buildPublic bootstraps a public-flavour database through the same helper the
// gate's tests use, so the sample is the production arm, not a copy of it.
func (e probeEnv) buildPublic(t *testing.T) (probeSample, *probeBuilt) {
	t.Helper()
	var apply time.Duration
	start := time.Now()
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, e.dsn, "1", 5*time.Minute, func(ctx context.Context, d *sql.DB) error {
		s := time.Now()
		err := ApplyBootstrap(ctx, SQLDB{DB: d})
		apply = time.Since(s)
		return err
	})
	work := time.Since(start)
	return probeSample{work: work, apply: apply}, e.describe(ctx, t, db, func() { _ = db.Close() })
}

// buildIsolated bootstraps the isolated flavour in its own database: pg_trgm in
// public, the bootstrap in a schema that heads the search_path, which skips the
// public-only migrations exactly like the gate's hand-written openers do.
func (e probeEnv) buildIsolated(t *testing.T) (probeSample, *probeBuilt) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	start := time.Now()
	name := probeDatabaseName(t)
	if err := e.createClone(ctx, "template0", name); err != nil {
		t.Fatalf("create isolated database from template0: %v", err)
	}
	var plain, iso *sql.DB
	t.Cleanup(func() {
		for _, d := range []*sql.DB{plain, iso} {
			if d != nil {
				_ = d.Close()
			}
		}
		e.dropDatabase(name)
	})
	plain = e.connect(name, "public")
	postgresproof.InstallTrigramExtension(ctx, t, plain)
	if _, err := plain.ExecContext(ctx, "CREATE SCHEMA "+probeQuote(probeSchema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	iso = e.connect(name, probeSchema+",public")
	s := time.Now()
	if err := ApplyBootstrap(ctx, SQLDB{DB: iso}); err != nil {
		t.Fatalf("apply isolated bootstrap: %v", err)
	}
	apply := time.Since(s)
	work := time.Since(start)
	return probeSample{work: work, apply: apply}, e.describe(ctx, t, iso, func() { _ = plain.Close(); _ = iso.Close() })
}

func (e probeEnv) describe(ctx context.Context, t *testing.T, db *sql.DB, release func()) *probeBuilt {
	t.Helper()
	b := &probeBuilt{release: release}
	if err := db.QueryRowContext(ctx, `SELECT current_database(), (SELECT count(*) FROM eshu_schema_migrations)`).Scan(&b.name, &b.applied); err != nil {
		t.Fatalf("describe built database: %v", err)
	}
	var err error
	if b.catalog, err = probeFingerprint(ctx, db); err != nil {
		t.Fatalf("fingerprint %s: %v", b.name, err)
	}
	return b
}

// clone runs the clone arm: create, connect, and the first query a test makes.
func (e probeEnv) clone(t *testing.T, template *probeBuilt, searchPath string) (probeSample, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	start := time.Now()
	name := probeDatabaseName(t)
	var db *sql.DB
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
		e.dropDatabase(name)
	})
	if err := e.createClone(ctx, template.name, name); err != nil {
		t.Fatalf("clone %s: %v (sqlstate %s)", template.name, err, probeSQLState(err))
	}
	db = e.connect(name, searchPath)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM eshu_schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("first query on clone: %v", err)
	}
	if n != template.applied {
		t.Errorf("clone ledger rows = %d, template = %d", n, template.applied)
	}
	return probeSample{work: time.Since(start)}, db
}

func probeStats(values []float64) (lo, p25, med, p75, hi float64) {
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	at := func(q float64) float64 {
		pos := q * float64(len(v)-1)
		i := int(pos)
		if i+1 >= len(v) {
			return v[len(v)-1]
		}
		return v[i] + (v[i+1]-v[i])*(pos-float64(i))
	}
	return v[0], at(0.25), at(0.5), at(0.75), v[len(v)-1]
}

// probeSampleRuns runs every arm n times in subtests, rotating which arm goes
// first on each pass so no arm always pays the cold-cache or host-noise cost.
// A failed subtest contributes no sample: it is counted in failed instead, so
// a failed clone arm can never read as a 0 ms median.
func probeSampleRuns(t *testing.T, phase string, arms []probeArm, n int, results map[string][]probeSample, failed map[string]int) {
	t.Helper()
	for i := 0; i < n; i++ {
		for j := range arms {
			arm := arms[(i+j)%len(arms)]
			key := phase + "/" + arm.name
			var s probeSample
			start := time.Now()
			ok := t.Run(fmt.Sprintf("%s_%s_%02d", phase, arm.name, i), func(t *testing.T) { s = arm.run(t) })
			s.total = time.Since(start)
			if ok {
				results[key] = append(results[key], s)
			} else {
				failed[key]++
			}
			probeLine(t, "kind=sample phase=%s arm=%s i=%d ok=%t failed=%d work_ms=%s apply_ms=%s total_ms=%s", phase, arm.name, i, ok, failed[key], probeMS(s.work), probeMS(s.apply), probeMS(s.total))
		}
	}
}

// probeReport prints each arm's median once and the clone ratio for each
// flavour of phase. It returns the work-time ratios keyed by flavour with
// whether every arm gave n good samples and every ratio resolved; a false
// valid tiers INVALID_SAMPLES downstream, never GO.
func probeReport(t *testing.T, phase string, arms []probeArm, n int, results map[string][]probeSample, failed map[string]int) (map[string]float64, bool) {
	t.Helper()
	valid := true
	for _, a := range arms {
		if failed[phase+"/"+a.name] > 0 || len(results[phase+"/"+a.name]) < n {
			valid = false
		}
	}
	median := func(arm string, total bool) float64 {
		var v []float64
		for _, s := range results[phase+"/"+arm] {
			d := s.work
			if total {
				d = s.total
			}
			v = append(v, float64(d)/float64(time.Millisecond))
		}
		if len(v) == 0 {
			return 0
		}
		lo, p25, m, p75, hi := probeStats(v)
		if !total {
			probeLine(t, "kind=median phase=%s arm=%s n=%d work_ms_median=%.1f p25=%.1f p75=%.1f min=%.1f max=%.1f", phase, arm, len(v), m, p25, p75, lo, hi)
		}
		return m
	}
	work, total := map[string]float64{}, map[string]float64{}
	for _, a := range arms {
		work[a.name], total[a.name] = median(a.name, false), median(a.name, true)
	}
	ratios := map[string]float64{}
	for _, f := range []struct{ flavour, boot, clone string }{
		{"public", "boot_public", "clone_public"},
		{"isolated", "boot_isolated", "clone_isolated"},
		{"isolated_vs_shared_schema", "boot_isolated_shared_schema", "clone_isolated"},
	} {
		b, c := work[f.boot], work[f.clone]
		bt, ct := total[f.boot], total[f.clone]
		if b == 0 || c == 0 {
			valid = false
			continue
		}
		ratios[f.flavour] = c / b
		probeLine(t, "kind=ratio phase=%s flavour=%s r=%.3f r_with_teardown=%.3f boot_ms=%.1f clone_ms=%.1f", phase, f.flavour, c/b, ct/bt, b, c)
	}
	return ratios, valid
}

// probeTier applies the arbiter's per-flavour thresholds. Failed samples
// (valid=false) tier INVALID_SAMPLES, never GO.
func probeTier(r float64, equal, valid bool) string {
	switch {
	case !valid:
		return "INVALID_SAMPLES"
	case !equal || r > 0.60:
		return "STOP"
	case r <= 0.35:
		return "GO"
	default:
		return "RELIEF"
	}
}

// probeWorse returns the worse of two tiers: the 70 real tests pay the cheaper
// shared-schema bootstrap, so the isolated decision input is the worse of the
// ruled-denominator tier and the shared-schema tier. INVALID_SAMPLES outranks
// every tier, never GO.
func probeWorse(a, b string) string {
	rank := map[string]int{"GO": 0, "RELIEF": 1, "STOP": 2, "INVALID_SAMPLES": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func probeDecide(public, isolated string, concurrentOK bool) string {
	switch {
	case public == "INVALID_SAMPLES" || isolated == "INVALID_SAMPLES":
		return "INVALID_SAMPLES"
	case public == "STOP":
		return "STOP"
	case isolated == "STOP":
		return "PUBLIC_ONLY_ISOLATED_STOP"
	case !concurrentOK:
		return "NOT_GO_CONCURRENT_CLONES_FAILED"
	case public == "GO" && isolated == "GO":
		return "GO_FULL"
	default:
		return "CLONE_CAP_RELIEF_ONLY"
	}
}

// TestReducerContentionGateProbe10148CloneRatio measures the clone ratio, the
// catalog equality, the concurrent-clone behaviour and the bootstrap cost with
// crash durability off, for both bootstrap flavours. See the file comment.
func TestReducerContentionGateProbe10148CloneRatio(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #10148 clone-ratio probe")
	}
	if os.Getenv("ESHU_POSTGRES_DISPOSABLE") != "1" {
		t.Skip("set ESHU_POSTGRES_DISPOSABLE=1 to run the #10148 clone-ratio probe on a disposable PostgreSQL server")
	}
	adminDSN := disposableAdminDSN(t, dsn)
	cfg, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse administrative DSN: %v", err)
	}
	e := probeEnv{dsn: adminDSN, cfg: cfg}
	samples := probeSampleDefault
	if v, err := strconv.Atoi(os.Getenv("ESHU_PROBE_10148_SAMPLES")); err == nil && v > 0 {
		samples = v
	}
	ctx := t.Context()
	before, err := e.durability(ctx)
	if err != nil {
		t.Fatalf("read durability settings: %v", err)
	}
	var version string
	admin := stdlib.OpenDB(*cfg.Copy())
	if err := admin.QueryRowContext(ctx, `SELECT version()`).Scan(&version); err != nil {
		t.Fatalf("read server version: %v", err)
	}
	_ = admin.Close()
	probeLine(t, "kind=env n=%d fsync_n=%d settings=%q version=%q", samples, probeFsyncSamples, before, version)

	// Templates are built once per process and never reused across processes.
	// A fresh real bootstrap of each flavour is kept for the equality check.
	_, pubTpl := e.buildPublic(t)
	_, isoTpl := e.buildIsolated(t)
	_, pubFresh := e.buildPublic(t)
	_, isoFresh := e.buildIsolated(t)
	for _, b := range []*probeBuilt{pubTpl, isoTpl, pubFresh, isoFresh} {
		b.release()
	}
	e.quiesce(ctx, t, pubTpl.name)
	e.quiesce(ctx, t, isoTpl.name)
	pubEqual := probeEquality(t, e, "public", pubFresh, pubTpl, "public")
	isoEqual := probeEquality(t, e, "isolated", isoFresh, isoTpl, probeSchema+",public")

	arms := []probeArm{
		{"boot_public", func(t *testing.T) probeSample { s, _ := e.buildPublic(t); return s }},
		{"clone_public", func(t *testing.T) probeSample { s, _ := e.clone(t, pubTpl, "public"); return s }},
		{"boot_isolated", func(t *testing.T) probeSample { s, _ := e.buildIsolated(t); return s }},
		{"clone_isolated", func(t *testing.T) probeSample { s, _ := e.clone(t, isoTpl, probeSchema+",public"); return s }},
		{"boot_isolated_shared_schema", func(t *testing.T) probeSample {
			start := time.Now()
			openGenerationRetentionMigratedSchema(t)
			return probeSample{work: time.Since(start)}
		}},
	}
	results := map[string][]probeSample{}
	failed := map[string]int{}
	probeSampleRuns(t, "main", arms, samples, results, failed)
	ratios, mainValid := probeReport(t, "main", arms, samples, results, failed)
	pubTier, isoTier := probeTier(ratios["public"], pubEqual, mainValid), probeTier(ratios["isolated"], isoEqual, mainValid)
	sharedTier := probeTier(ratios["isolated_vs_shared_schema"], isoEqual, mainValid)
	isoDecision := probeWorse(isoTier, sharedTier)

	concurrentOK := probeConcurrent(t, e, "public", pubTpl, "public")
	concurrentOK = probeConcurrent(t, e, "isolated", isoTpl, probeSchema+",public") && concurrentOK
	probeLine(t, "kind=verdict n=%d public_r=%.3f public_equal=%t public_tier=%s isolated_r=%.3f isolated_equal=%t isolated_tier=%s isolated_vs_shared_schema_tier=%s isolated_decision=%s concurrent_ok=%t decision=%s",
		samples, ratios["public"], pubEqual, pubTier, ratios["isolated"], isoEqual, isoTier, sharedTier, isoDecision, concurrentOK, probeDecide(pubTier, isoDecision, concurrentOK))

	probeFileCopy(t, e, "public", pubTpl, "public")
	probeFileCopy(t, e, "isolated", isoTpl, probeSchema+",public")

	// Arm B, last: the same arms with crash durability off. The reset is
	// registered first so it runs on every exit; it is shown, not assumed.
	t.Run("durability_off", func(t *testing.T) {
		if !probeDurabilityArmed(t, before) {
			return
		}
		reset := func() {
			rctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := e.setDurability(rctx, false, before); err != nil {
				probeLine(t, "kind=settings stage=reset_failed err=%q", err)
				t.Errorf("durability reset failed, the rest of this run is void as a control: %v", err)
				return
			}
			probeLine(t, "kind=settings stage=reset_ok")
		}
		t.Cleanup(reset)
		probeLine(t, "kind=settings stage=before settings=%q", before)
		if err := e.setDurability(ctx, true, ""); err != nil {
			t.Fatalf("turn durability off: %v", err)
		}
		during, _ := e.durability(ctx)
		probeLine(t, "kind=settings stage=during settings=%q", during)
		fsyncResults := map[string][]probeSample{}
		fsyncFailed := map[string]int{}
		probeSampleRuns(t, "fsync_off", arms[:4], probeFsyncSamples, fsyncResults, fsyncFailed)
		probeReport(t, "fsync_off", arms[:4], probeFsyncSamples, fsyncResults, fsyncFailed)
		reset()
		after, _ := e.durability(ctx)
		probeLine(t, "kind=settings stage=after settings=%q restored=%t", after, after == before)
		if after != before {
			t.Errorf("durability settings after reset = %q, before = %q", after, before)
		}
	})
}
