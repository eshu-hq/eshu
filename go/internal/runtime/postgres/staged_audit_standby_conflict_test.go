// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestStagedAuditStandbyRecoveryConflictHarness is the #11353 failing-first
// harness: a disposable Docker primary plus a physical standby under WAL
// replay, with a short max_standby_streaming_delay. A long per-scope snapshot
// read on the standby races primary UPDATE+VACUUM replay and must die with a
// real recovery conflict (SQLSTATE 40001 carrying "conflict with recovery"),
// which the test proves three ways: the error classifies with
// IsRecoveryConflict, pg_stat_database_conflicts.confl_snapshot on the standby
// advances, and the staged-audit runner aborts the tick as
// conflict_exhausted without writer contact. Every probe statement carries a
// scope predicate and a row bound; the probe never touches fact_records.
//
// Opt-in with ESHU_11353_STANDBY_HARNESS=1. With ESHU_11353_WRITER_DSN and
// ESHU_11353_STANDBY_DSN set it uses that owned pair and provisions nothing;
// otherwise it starts disposable containers on ephemeral loopback ports (no
// hardcoded session host, port, or container target) using
// ESHU_11353_POSTGRES_IMAGE (default postgres:18-alpine). It skips when
// Docker or the image is unavailable. Probe seeding, the conflict race, the
// live fleet adapter, and the bounded-scan guard live in
// staged_audit_standby_probe_test.go.
func TestStagedAuditStandbyRecoveryConflictHarness(t *testing.T) {
	if os.Getenv("ESHU_11353_STANDBY_HARNESS") != "1" {
		t.Skip("standby recovery-conflict harness requires ESHU_11353_STANDBY_HARNESS=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	topology := requireStandbyHarnessTopology(t, ctx)
	const conflictScope = "eco-1"
	table, censusTable, scopes := seedStandbyHarnessProbe(t, ctx, topology.writerDSN)
	waitStandbyHarnessReplay(t, ctx, topology.readerDSN, table, 4*standbyHarnessRowsPerScope)

	// Phase 1: prove a real confl_snapshot on a long snapshot read.
	t.Logf("phase 1: long snapshot read on %s races UPDATE+VACUUM replay", conflictScope)
	snapshotBefore := standbyHarnessSnapshotConflicts(t, ctx, topology.readerDSN)
	liveErr := raceStandbyHarnessSnapshot(t, ctx, topology, table, conflictScope)
	if liveErr == nil {
		t.Fatalf("RED: long snapshot read survived UPDATE+VACUUM replay with no error (no recovery conflict reproduced)")
	}
	if !IsRecoveryConflict(liveErr) {
		t.Fatalf("RED: snapshot read failed without a recovery conflict: %v", liveErr)
	}
	if reason, _ := ClassifyStagedAuditError(liveErr); reason != StagedAuditDeferConflictExhausted {
		t.Fatalf("RED: live conflict classified as %q, want conflict_exhausted", reason)
	}
	snapshotAfter := standbyHarnessSnapshotConflicts(t, ctx, topology.readerDSN)
	t.Logf("phase 1: replay lag=%v confl_snapshot %d->%d live error: %v",
		standbyHarnessReplayLag(ctx, topology.readerDSN), snapshotBefore, snapshotAfter, liveErr)
	if snapshotAfter-snapshotBefore < 1 {
		t.Fatalf("RED: error classified as a conflict but confl_snapshot did not advance (%d->%d)", snapshotBefore, snapshotAfter)
	}

	// Phase 2: the runner must surface the same live conflict as a
	// conflict_exhausted deferral with no writer contact.
	t.Logf("phase 2: staged-audit tick over the live standby pair")
	fleet := &standbyHarnessFleet{readerDSN: topology.readerDSN, member: "standby-11353"}
	telemetry := NewStagedAuditTelemetry()
	runner := NewStagedAuditRunner(fleet, telemetry, StagedAuditRunnerOptions{BatchTimeout: 30 * time.Second})
	batchStarted := make(chan string, 8)
	verified := false
	go func() {
		for scope := range batchStarted {
			if scope == conflictScope {
				churnStandbyHarnessScope(context.Background(), topology.writerDSN, table, scope, 3)
				return
			}
		}
	}()
	plan := standbyHarnessTickPlan(table, censusTable, scopes, conflictScope, batchStarted, func() { verified = true })
	_, runErr := runner.RunTick(ctx, plan)
	close(batchStarted)
	if runErr == nil {
		t.Fatal("RED: tick completed while a recovery conflict was replaying underneath it")
	}
	var deferred *StagedAuditDeferredError
	if !isStagedAuditDeferredAs(runErr, StagedAuditDeferConflictExhausted, &deferred) {
		t.Fatalf("RED: tick aborted as %v, want conflict_exhausted (countersProve=%v)", runErr, snapshotAfter > snapshotBefore)
	}
	if verified {
		t.Fatal("RED: aborted tick contacted the writer-side verify")
	}
	if got := telemetry.Deferrals(StagedAuditDeferConflictExhausted); got != 1 {
		t.Fatalf("conflict_exhausted deferrals = %d, want 1", got)
	}
	if got := telemetry.Ticks(); got != 0 {
		t.Fatalf("completed ticks = %d, want 0 after the conflict abort", got)
	}
	t.Logf("phase 2: member=%s replay lag=%v conflict_exhausted=1 writerContacts=0",
		fleet.member, standbyHarnessReplayLag(ctx, topology.readerDSN))
	assertStandbyHarnessBounded(t, fleet.statements)
}

// standbyHarnessRowsPerScope keeps the probe small: four per-ecosystem chunks
// that each scan in seconds with pg_sleep pacing, never a full-table sweep.
const standbyHarnessRowsPerScope = 1500

// standbyHarnessTopology is a primary/standby pair the harness may churn.
type standbyHarnessTopology struct {
	writerDSN string
	readerDSN string
}

// requireStandbyHarnessTopology returns the owned pair from the environment
// or provisions a disposable Docker pair on ephemeral ports.
func requireStandbyHarnessTopology(t *testing.T, ctx context.Context) *standbyHarnessTopology {
	t.Helper()
	if writer, reader := os.Getenv("ESHU_11353_WRITER_DSN"), os.Getenv("ESHU_11353_STANDBY_DSN"); writer != "" && reader != "" {
		t.Logf("using owned primary/standby pair from ESHU_11353_*_DSN")
		return &standbyHarnessTopology{writerDSN: writer, readerDSN: reader}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is unavailable to provision the disposable standby pair")
	}
	image := os.Getenv("ESHU_11353_POSTGRES_IMAGE")
	if image == "" {
		image = "postgres:18-alpine"
	}
	if _, err := exec.CommandContext(ctx, "docker", "image", "inspect", image).CombinedOutput(); err != nil {
		if out, perr := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); perr != nil {
			t.Skipf("standby image %s is unavailable: %s", image, strings.TrimSpace(string(out)))
		}
	}
	uid := fmt.Sprintf("%d%d", os.Getpid(), time.Now().UnixNano()%100000)
	primaryName, standbyName := "eshu-11353-p-"+uid, "eshu-11353-s-"+uid
	network, volume := "eshu-11353-net-"+uid, "eshu-11353-vol-"+uid
	primaryPort, standbyPort := standbyHarnessFreePort(t), standbyHarnessFreePort(t)
	const password = "harness11353pw"
	cleanups := []func(){}
	defer func() {
		if t.Failed() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		}
	}()
	standbyHarnessDocker(t, ctx, "network", "create", "--label", "eshu.harness=11353", network)
	cleanups = append(cleanups, func() { _ = exec.Command("docker", "network", "rm", network).Run() })
	standbyHarnessDocker(t, ctx, "volume", "create", "--label", "eshu.harness=11353", volume)
	cleanups = append(cleanups, func() { _ = exec.Command("docker", "volume", "rm", volume).Run() })
	// Pin PGDATA explicitly: the alpine image defaults to a versioned path,
	// so every container must agree on where the data directory lives.
	const pgdata = "/var/lib/postgresql/data"
	standbyHarnessDocker(t, ctx, "run", "-d", "--name", primaryName, "--network", network,
		"--label", "eshu.harness=11353", "-p", fmt.Sprintf("127.0.0.1:%d:5432", primaryPort),
		"-e", "POSTGRES_PASSWORD="+password, "-e", "PGDATA="+pgdata, image,
		"-c", "wal_level=replica", "-c", "max_wal_senders=8", "-c", "max_replication_slots=4")
	cleanups = append(cleanups, func() { _ = exec.Command("docker", "rm", "-f", primaryName).Run() })
	writerDSN := fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres?sslmode=disable", password, primaryPort)
	standbyHarnessWaitReady(t, ctx, primaryName, writerDSN)
	primary := mustStandbyHarnessConnect(t, ctx, writerDSN)
	if _, err := primary.Exec(ctx, "CREATE ROLE harness_repl WITH LOGIN REPLICATION"); err != nil {
		t.Fatalf("create replication role: %v", err)
	}
	// Trust only the disposable replication role from any host: the pair
	// lives on its own bridge network and is removed with the test.
	standbyHarnessDocker(t, ctx, "exec", "-u", "postgres", primaryName, "sh", "-c",
		`printf "%s\n" "host replication harness_repl all trust" | cat - "$PGDATA/pg_hba.conf" > "$PGDATA/pg_hba.new" && cat "$PGDATA/pg_hba.new" > "$PGDATA/pg_hba.conf" && rm "$PGDATA/pg_hba.new"`)
	if _, err := primary.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		t.Fatalf("reload primary pg_hba: %v", err)
	}
	_ = primary.Close(ctx)
	standbyHarnessDocker(t, ctx, "run", "--rm", "-v", volume+":"+pgdata, image,
		"chown", "-R", "postgres:postgres", pgdata)
	standbyHarnessDocker(t, ctx, "run", "--rm", "--user", "postgres", "--network", network,
		"-v", volume+":"+pgdata, image,
		"pg_basebackup", "-h", primaryName, "-p", "5432", "-U", "harness_repl",
		"-D", pgdata, "-R", "-X", "stream", "--checkpoint=fast")
	standbyHarnessDocker(t, ctx, "run", "-d", "--name", standbyName, "--network", network,
		"--label", "eshu.harness=11353", "-p", fmt.Sprintf("127.0.0.1:%d:5432", standbyPort),
		"-e", "PGDATA="+pgdata, "-v", volume+":"+pgdata, image,
		"-c", "hot_standby_feedback=off",
		"-c", "max_standby_streaming_delay=1s",
		"-c", "max_standby_archive_delay=1s")
	cleanups = append(cleanups, func() { _ = exec.Command("docker", "rm", "-f", standbyName).Run() })
	readerDSN := fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres?sslmode=disable", password, standbyPort)
	standbyHarnessWaitStandby(t, ctx, standbyName, readerDSN)
	topology := &standbyHarnessTopology{writerDSN: writerDSN, readerDSN: readerDSN}
	t.Cleanup(func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	})
	return topology
}

// standbyHarnessDocker runs one docker command and fails the test with its
// combined output.
func standbyHarnessDocker(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	timeout, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(timeout, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// standbyHarnessFreePort allocates an ephemeral loopback port for a
// disposable container instead of hardcoding a session port.
func standbyHarnessFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ephemeral port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

// standbyHarnessWaitReady waits for the containerized primary to accept SQL
// through the published port, not just on the container-local socket: a
// pg_isready success inside the container can precede a usable TCP proxy.
func standbyHarnessWaitReady(t *testing.T, ctx context.Context, container, dsn string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		execErr := exec.CommandContext(timeout, "docker", "exec", container, "pg_isready", "-U", "postgres").Run()
		cancel()
		if execErr == nil {
			timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			conn, connErr := pgx.Connect(timeout, dsn)
			if connErr == nil {
				var one int
				queryErr := conn.QueryRow(timeout, "SELECT 1").Scan(&one)
				_ = conn.Close(context.Background())
				cancel()
				if queryErr == nil && one == 1 {
					return
				}
			} else {
				cancel()
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("primary %s never became ready: %v", container, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("primary %s never became ready", container)
}

// standbyHarnessWaitStandby waits until the reader answers in recovery,
// dumping the standby log once when it never does.
func standbyHarnessWaitStandby(t *testing.T, ctx context.Context, container, readerDSN string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := pgx.Connect(ctx, readerDSN)
		if err == nil {
			var recovery bool
			if qerr := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&recovery); qerr == nil && recovery {
				_ = conn.Close(ctx)
				return
			}
			_ = conn.Close(ctx)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("standby %s never entered recovery: %v", container, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	timeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(timeout, "docker", "logs", "--tail", "40", container).CombinedOutput()
	t.Fatalf("standby %s never entered recovery; logs: %s", container, strings.TrimSpace(string(out)))
}
