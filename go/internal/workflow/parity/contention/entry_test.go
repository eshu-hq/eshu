// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// Leg wiring environment, beyond the §2 fields in spec.go.
const (
	envInTreeDSN       = "PARITY_INTREE_DSN"
	envInTreeOptIn     = "PARITY_INTREE_DISPOSABLE"
	envSatelliteURL    = "ESHU_CONTENTION_SATELLITE_URL"
	envSatelliteDSN    = "ESHU_CONTENTION_SATELLITE_DSN"
	envSatelliteDB     = "ESHU_CONTENTION_SATELLITE_DATABASE"
	envSatelliteQuotas = "ESHU_CONTENTION_SATELLITE_QUOTAS"
	envExporterURL     = "ESHU_CONTENTION_PG_EXPORTER_URL"
	envLockDatabase    = "ESHU_CONTENTION_PG_LOCK_DATABASE"
	envCoreDSN         = "ESHU_CONTENTION_CORE_DSN"
	envReportDir       = "ESHU_CONTENTION_REPORT_DIR"
	envAbortFile       = "ESHU_CONTENTION_ABORT_FILE"
	envClockSkew       = "ESHU_CONTENTION_CLOCK_SKEW"
	envSamplerDisabled = "ESHU_CONTENTION_SAMPLER_DISABLED"
	envSampleInterval  = "ESHU_CONTENTION_SAMPLE_INTERVAL"
	satelliteNoLatency = "NOT_AVAILABLE: claim-plane v0.1.1 (8311ed6) internal/telemetry registers only workflow_claim_grants_total, " +
		"workflow_claim_mutations_total, workflow_commit_pins_total, workflow_claim_reaps_total, workflow_claim_degraded_total (counters) " +
		"and workflow_queue_depth (gauge); no latency histogram exists"
	inTreeServerLatency = "in-process WorkflowControlStore call: server side equals client side on this leg"
)

// forbiddenSatelliteDatabases are names the satellite DSN must never carry:
// the core database and the maintenance databases.
var forbiddenSatelliteDatabases = map[string]bool{"eshu": true, "postgres": true, "template0": true, "template1": true}

func loadOrSkip(t *testing.T) Spec {
	t.Helper()
	if os.Getenv(EnvTarget) == "" {
		t.Skip(EnvTarget + " is not set; skipping the #9727 contention run")
	}
	spec, err := LoadSpec(os.Getenv)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return spec
}

// TestContentionInTree runs one seed of the §5 mix on the in-tree store in a
// disposable database on a loopback Postgres server.
func TestContentionInTree(t *testing.T) {
	spec := loadOrSkip(t)
	admin := os.Getenv(envInTreeDSN)
	if admin == "" {
		t.Skip(envInTreeDSN + " is not set; skipping the in-tree contention leg")
	}
	requireLoopbackDSN(t, envInTreeDSN, admin)
	requireInTreeServerSafe(t, admin)
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, admin, os.Getenv(envInTreeOptIn), 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
				return fmt.Errorf("apply bootstrap schema: %w", err)
			}
			return postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database}).EnsureSchema(ctx)
		})
	name, err := CurrentDatabase(ctx, db)
	if err != nil {
		t.Fatalf("current database: %v", err)
	}
	postgresproof.RaisePoolForConcurrentClaimers(db, spec.Workers()+30)
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db})
	store.DefaultClaimLeaseTTL, store.DefaultHeartbeatInterval = spec.LeaseTTL, spec.HeartbeatInterval
	opt := entryOptions(t, spec)
	if spec.QuotaBudget > 0 {
		quotaKind := scope.CollectorKind("package_registry")
		store.QuotaLookup = func(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
			if kind == quotaKind {
				return workflow.ClaimQuota{MaxGenerationsPerHour: spec.QuotaBudget}, true
			}
			return workflow.ClaimQuota{}, false
		}
	}
	observerDB := openObserverDB(t, admin, name)
	observer, err := NewSQLObserver(ctx, observerDB)
	if err != nil {
		t.Fatalf("observer: %v", err)
	}
	leg := Leg{
		Name: "in-tree", Backend: parity.NewInTreeBackend(store), Observer: observer,
		Headroom: SQLHeadroom{DB: observerDB}, Advisory: true, Quota: spec.QuotaBudget > 0,
		HarnessReaper: true, ServerLatency: inTreeServerLatency, DatabaseName: name,
		ClockSkew: clockSkew(t, spec),
	}
	t.Logf("in-tree leg: %s host %s, disposable database %s", envInTreeDSN, dsnHost(t, admin), name)
	runAndRecord(t, spec, leg, opt)
}

// TestContentionSatellite runs one seed of the §5 mix on a claim plane over
// HTTP: a local container, or (target live, grant named) the dark claim
// plane through a port-forward. Every address must be loopback.
func TestContentionSatellite(t *testing.T) {
	spec := loadOrSkip(t)
	baseURL, dsn := os.Getenv(envSatelliteURL), os.Getenv(envSatelliteDSN)
	if baseURL == "" || dsn == "" {
		t.Skip(envSatelliteURL + " and " + envSatelliteDSN + " are required; skipping the satellite contention leg")
	}
	requireLoopbackURL(t, baseURL)
	requireLoopbackDSN(t, envSatelliteDSN, dsn)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open satellite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	ctx := context.Background()
	name, err := CurrentDatabase(ctx, db)
	if err != nil {
		t.Fatalf("satellite current database: %v", err)
	}
	if want := os.Getenv(envSatelliteDB); want == "" || name != want || forbiddenSatelliteDatabases[name] {
		t.Fatalf("satellite DSN reaches database %q; %s must name it and it must not be one of %v", name, envSatelliteDB, forbiddenSatelliteDatabases)
	}
	quota := satelliteQuota(t, spec)
	observer, err := NewSQLObserver(ctx, db)
	if err != nil {
		t.Fatalf("observer: %v", err)
	}
	leg := Leg{
		Name: "satellite", Observer: observer, Pin: true, Quota: quota,
		Backend:       parity.NewSatelliteBackend(workflow.NewClaimPlaneClient(baseURL, claimPlaneHTTPClient())),
		HarnessReaper: spec.Target == TargetLocal, ServerLatency: satelliteNoLatency, DatabaseName: name,
		ClockSkew: clockSkew(t, spec),
	}
	if exporter := os.Getenv(envExporterURL); exporter != "" {
		leg.Headroom = ExporterHeadroom{URL: exporter, LockDatabase: os.Getenv(envLockDatabase)}
	} else if spec.Target == TargetLive {
		t.Fatalf("%s is required on the live target (pg_headroom abort ceilings)", envExporterURL)
	} else {
		leg.Headroom = SQLHeadroom{DB: db}
	}
	opt := entryOptions(t, spec)
	if core := os.Getenv(envCoreDSN); core != "" {
		opt.CoreResidue = coreResidueCounter(t, core)
	}
	t.Logf("satellite leg: %s, database %s, quotas %q, harness reaper %v", baseURL, name, os.Getenv(envSatelliteQuotas), leg.HarnessReaper)
	runAndRecord(t, spec, leg, opt)
}

// satelliteQuota reads the --quotas flag the claim plane was started with.
// It must be stated, because the harness cannot read it back.
func satelliteQuota(t *testing.T, spec Spec) bool {
	t.Helper()
	flag := strings.TrimSpace(os.Getenv(envSatelliteQuotas))
	switch {
	case flag == "none" && spec.QuotaBudget == 0:
		return false
	case flag == fmt.Sprintf("package_registry=%d", spec.QuotaBudget) && spec.QuotaBudget > 0:
		return true
	}
	t.Fatalf("%s=%q must be \"none\" with %s=%s, or \"package_registry=<budget>\" matching the budget",
		envSatelliteQuotas, flag, EnvQuotaBudget, NotApplicable)
	return false
}

func entryOptions(t *testing.T, spec Spec) Options {
	t.Helper()
	opt := Options{AbortFile: os.Getenv(envAbortFile), SamplerDisabled: os.Getenv(envSamplerDisabled) == "1", Logf: t.Logf}
	if v := os.Getenv(envSampleInterval); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("%s: %v", envSampleInterval, err)
		}
		opt.SampleInterval = d
	}
	if spec.Window < 6*time.Minute {
		opt.AdvisoryInterval = spec.Window / 6
	}
	return opt
}

func clockSkew(t *testing.T, spec Spec) time.Duration {
	t.Helper()
	if v := os.Getenv(envClockSkew); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("%s: %v", envClockSkew, err)
		}
		return d
	}
	if spec.Target == TargetLive {
		return 2 * time.Second
	}
	return 250 * time.Millisecond
}

func runAndRecord(t *testing.T, spec Spec, leg Leg, opt Options) {
	t.Helper()
	// SIGINT or SIGTERM aborts the run; Run still drains, prefix-deletes,
	// and checks residue before the test returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := Run(ctx, spec, leg, opt)
	if report != nil {
		t.Log("\n" + report.Summary())
		writeReport(t, spec, leg, report)
	}
	if err != nil {
		t.Fatalf("contention run: %v", err)
	}
	if report.Verdict != VerdictClean && report.Verdict != VerdictOverhead {
		t.Fatalf("contention %s seed %d verdict %s", leg.Name, spec.Seed, report.Verdict)
	}
}

func writeReport(t *testing.T, spec Spec, leg Leg, report *Report) {
	t.Helper()
	dir := os.Getenv(envReportDir)
	if dir == "" {
		return
	}
	base := filepath.Join(dir, fmt.Sprintf("%s-%s-seed%d-w%d-%s", leg.Name, spec.Target, spec.Seed,
		spec.Workers(), report.Phases.Start.Format("20060102T150405Z")))
	body, err := report.JSON()
	if err != nil {
		t.Errorf("report JSON: %v", err)
		return
	}
	if err := os.WriteFile(base+".json", body, 0o600); err != nil {
		t.Errorf("write report: %v", err)
	}
	if err := os.WriteFile(base+".txt", []byte(report.Summary()), 0o600); err != nil {
		t.Errorf("write summary: %v", err)
	}
	t.Logf("report written to %s.json", base)
}

func openObserverDB(t *testing.T, admin, name string) *sql.DB {
	t.Helper()
	dsn, err := postgresproof.WithDatabase(admin, name)
	if err != nil {
		t.Fatalf("observer DSN: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open observer db: %v", err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// coreResidueCounter is the third zero: a read-only transaction counting
// prefixed rows in core's workflow tables. It never writes.
func coreResidueCounter(t *testing.T, dsn string) func(context.Context) (int, error) {
	t.Helper()
	requireLoopbackDSN(t, envCoreDSN, dsn)
	return func(ctx context.Context) (int, error) {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return 0, err
		}
		defer func() { _ = db.Close() }()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return 0, err
		}
		defer func() { _ = tx.Rollback() }()
		var n int
		err = tx.QueryRowContext(ctx, `SELECT
    (SELECT count(*) FROM workflow_runs WHERE run_id LIKE $1)
  + (SELECT count(*) FROM workflow_work_items WHERE work_item_id LIKE $2)
  + (SELECT count(*) FROM workflow_claims WHERE work_item_id LIKE $2)`, runFamily, itemFamily).Scan(&n)
		return n, err
	}
}

func dsnHost(t *testing.T, dsn string) string {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	return net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
}

func requireLoopbackDSN(t *testing.T, env, dsn string) {
	t.Helper()
	if err := loopbackOnly(dsn); err != nil {
		t.Fatalf("%s: %v; the harness only reaches local containers or port-forwards", env, err)
	}
}

func requireLoopbackURL(t *testing.T, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || !isLoopback(u.Hostname()) {
		t.Fatalf("%s=%q must be a loopback URL (local container or port-forward)", envSatelliteURL, raw)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
