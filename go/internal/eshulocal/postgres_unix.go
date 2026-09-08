// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !windows

package eshulocal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Local embedded-Postgres connection budget (#4456).
//
// The runtime invariant is max_connections >= (pool-holding services) *
// per-process pool ceiling + reserved/admin headroom. internal/runtime enforces
// exactly this for the Compose stacks in
// TestComposePostgresMaxConnectionsCoversPoolBudget; the embedded local server
// had been exempt and carried a bare "35" literal, which is how it came to sit
// below the budget for even a single pool-holding process.
//
// localPostgresPoolHolders enumerates the processes that can hold a pool
// against this server at the same time, rather than stating a count. A count is
// a number someone has to keep true; a list is one a reader can check against
// the code that starts these processes. Deriving the ceiling from len() means
// adding a holder here raises the ceiling in the same edit.
//
// The first three are the local supervisor's own children
// (internal/cli/localsupervisor/host.go: StartChildProcess for eshu-reducer,
// eshu-ingester and eshu-mcp-server under the authoritative/mcp_stdio modes).
// The last two are added by `eshu vuln-scan repo`, which attaches to a running
// owner and launches a short-lived loopback eshu-api plus eshu-bootstrap-index
// against that owner's Postgres (cmd/eshu/gotchas-read-surface-commands.md).
// All five take the shared 30-connection default, so the worst case is
// concurrent, not hypothetical.
//
// KNOWN LIMIT, stated because the list implies a completeness it cannot have:
// the local supervisor also opens its own connections with a bare
// sql.Open("pgx", dsn) and never calls runtime.ConfigurePostgresPool --
// config.go:216, content_search_indexes.go:42, iac_reachability_finalizer.go:28
// and progress.go:28. database/sql defaults MaxOpenConns to 0, i.e. unlimited,
// and the first two are long-lived for a whole authoritative run. So no finite
// ceiling here is strictly sound until those are bounded; this raises the floor
// from "guaranteed exhaustion" to "covers every capped holder", which is an
// improvement rather than a proof. That is the #4456 gap still open in the
// supervisor.
var localPostgresPoolHolders = [...]string{
	"eshu-reducer",
	"eshu-ingester",
	"eshu-mcp-server",
	"eshu-api",
	"eshu-bootstrap-index",
}

const (
	// localPostgresPoolHolderCount is a compile-time len() of the array above,
	// so the ceiling cannot drift from the list it is derived from.
	localPostgresPoolHolderCount = len(localPostgresPoolHolders)

	// localPostgresPerProcessPoolConns mirrors runtime.defaultPostgresMaxOpenConns.
	// It is duplicated rather than imported because internal/eshulocal/AGENTS.md
	// requires this package stay a leaf with no internal imports.
	localPostgresPerProcessPoolConns = 30

	// localPostgresReservedConns mirrors the reserved/admin headroom the Compose
	// budget leaves above the pool sum: superuser_reserved_connections (3 on the
	// embedded server), an operator psql session, the admin-status probe, and
	// transient tooling.
	localPostgresReservedConns = 20

	// LocalPostgresMaxConnections is the max_connections floor for the embedded
	// local server, derived from the budget above rather than chosen. It is a
	// floor and not the final value: ResolveLocalPostgresMaxConnections raises
	// it when the operator configures a larger per-process pool. Configuration
	// can never lower it, which is why the scope note in
	// docs/internal/evidence/eshulocal-connection-budget.md says the budget
	// cannot be reduced below the invariant by environment.
	LocalPostgresMaxConnections = localPostgresPoolHolderCount*localPostgresPerProcessPoolConns + localPostgresReservedConns
)

// localPostgresMaxOpenConnsEnv is the shared per-process pool knob. It is named
// here rather than imported because internal/eshulocal/AGENTS.md requires this
// package stay a leaf with no internal imports; runtime.LoadPostgresConfig reads
// the same key with the same default.
const localPostgresMaxOpenConnsEnv = "ESHU_POSTGRES_MAX_OPEN_CONNS"

// ResolveLocalPostgresMaxConnections returns the max_connections the embedded
// local server must start with for the pool size the operator has actually
// configured.
//
// WHY THIS IS NOT A CONSTANT. localsupervisor.ChildEnv ends in
// procexec.MergeEnvironment(procexec.Environ(), values) and does not set
// ESHU_POSTGRES_MAX_OPEN_CONNS, so every supervised child inherits the
// operator's value verbatim and sizes its own pool from it. The knob is
// documented and tunable upward -- docs/public/reference/postgres-tuning.md
// states the invariant as sum(pool-holding services * ESHU_POSTGRES_MAX_OPEN_CONNS)
// and tells operators to raise it when workers block on connections. A ceiling
// that hard-codes the default 30 therefore contradicts that documented contract
// the moment the knob is raised: five holders at 60 want 320 against a fixed
// 170, which is the same exhaustion this budget exists to prevent (#6603
// review).
//
// Parse rules mirror runtime.LoadPostgresConfig: unset or empty means the
// default, and a non-positive or unparseable value is not honoured. This
// function does not report that error because it sits on a config-construction
// path with no error return; it falls back to the default pool, which yields
// the documented floor. The child process that reads the same variable through
// runtime.LoadPostgresConfig still fails loudly on it, so the bad value is
// reported rather than swallowed -- it is simply not this function's job to
// report it.
func ResolveLocalPostgresMaxConnections(getenv func(string) string) int {
	perProcess := localPostgresPerProcessPoolConns
	if getenv != nil {
		if raw := strings.TrimSpace(getenv(localPostgresMaxOpenConnsEnv)); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				perProcess = parsed
			}
		}
	}
	derived := localPostgresPoolHolderCount*perProcess + localPostgresReservedConns
	if derived < LocalPostgresMaxConnections {
		return LocalPostgresMaxConnections
	}
	return derived
}

const (
	localPostgresHost     = "127.0.0.1"
	localPostgresUser     = "eshu"
	localPostgresPassword = "change-me"
	localPostgresDatabase = "postgres"
	localQueryProfileName = "local_lightweight"
)

var (
	newEmbeddedPostgres = func(config embeddedpostgres.Config) embeddedPostgresRuntime {
		return embeddedpostgres.NewDatabase(config)
	}
	postgresRuntimeDir = func(layout Layout) string {
		return runtimeSocketDir(layout, os.TempDir())
	}
	postmasterLockPostgresReady = postgresReadyFromPostmasterLock
)

type embeddedPostgresRuntime interface {
	Start() error
	Stop() error
}

// ManagedPostgres captures the embedded Postgres runtime owned by one workspace.
type ManagedPostgres struct {
	DSN        string
	Port       int
	DataDir    string
	SocketDir  string
	SocketPath string
	PID        int
	CtlPath    string
	runtime    embeddedPostgresRuntime
}

// Close stops the embedded Postgres runtime.
func (m *ManagedPostgres) Close() error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(m.CtlPath) != "" && strings.TrimSpace(m.DataDir) != "" {
		if err := pgCtlRunner(m.CtlPath, "-D", m.DataDir, "stop", "-m", "fast"); err != nil {
			return fmt.Errorf("stop embedded postgres with pg_ctl: %w", err)
		}
		return nil
	}
	if m.runtime == nil {
		return nil
	}
	return m.runtime.Stop()
}

// StartEmbeddedPostgres boots the per-workspace embedded Postgres instance.
func StartEmbeddedPostgres(ctx context.Context, layout Layout) (*ManagedPostgres, error) {
	socketDir := postgresRuntimeDir(layout)
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres socket directory: %w", err)
	}
	if err := os.MkdirAll(layout.PostgresDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres data root: %w", err)
	}
	if err := os.MkdirAll(layout.CacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres cache root: %w", err)
	}

	dataDir := filepath.Join(layout.PostgresDir, "data")
	runtimeDir := filepath.Join(layout.PostgresDir, "runtime")
	binariesDir := filepath.Join(layout.PostgresDir, "binaries")
	cacheDir := filepath.Join(layout.CacheDir, "embedded-postgres")
	if err := stopOrphanedPostgresFromLockFile(dataDir); err != nil {
		return nil, err
	}

	var (
		port    int
		runtime embeddedPostgresRuntime
		err     error
	)
	for attempts := 0; attempts < 3; attempts++ {
		port, err = reserveLocalPostgresPort()
		if err != nil {
			return nil, err
		}

		postgresLog, closePostgresLog := postgresStartupLogWriter(layout)
		runtime = newEmbeddedPostgres(embeddedPostgresConfig(dataDir, runtimeDir, binariesDir, cacheDir, socketDir, port, postgresLog))
		if closePostgresLog != nil {
			defer closePostgresLog()
		}
		if err = runtime.Start(); err == nil {
			break
		}
		if !strings.Contains(err.Error(), "process already listening on port") {
			return nil, fmt.Errorf("start embedded postgres: %w", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("start embedded postgres: %w", err)
	}

	dsn := PostgresDSN(localPostgresHost, port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		_ = runtime.Stop()
		return nil, fmt.Errorf("open local postgres connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = runtime.Stop()
		return nil, fmt.Errorf("ping local postgres: %w", err)
	}

	pid, err := readPostmasterPID(dataDir)
	if err != nil {
		_ = runtime.Stop()
		return nil, err
	}

	return &ManagedPostgres{
		DSN:        dsn,
		Port:       port,
		DataDir:    dataDir,
		SocketDir:  socketDir,
		SocketPath: postgresSocketPath(socketDir, port),
		PID:        pid,
		CtlPath:    filepath.Join(binariesDir, "bin", "pg_ctl"),
		runtime:    runtime,
	}, nil
}

func embeddedPostgresConfig(
	dataDir string,
	runtimeDir string,
	binariesDir string,
	cacheDir string,
	socketDir string,
	port int,
	logs io.Writer,
) embeddedpostgres.Config {
	if logs == nil {
		logs = io.Discard
	}
	return embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Username(localPostgresUser).
		Password(localPostgresPassword).
		Database(localPostgresDatabase).
		Port(uint32(port)). // #nosec G115 -- port is reserved via net.Listen and validated > 0, safely fits uint32
		StartTimeout(45 * time.Second).
		RuntimePath(runtimeDir).
		DataPath(dataDir).
		BinariesPath(binariesDir).
		CachePath(cacheDir).
		Logger(logs).
		StartParameters(map[string]string{
			"listen_addresses":        "localhost",
			"max_connections":         strconv.Itoa(ResolveLocalPostgresMaxConnections(os.Getenv)),
			"unix_socket_directories": socketDir,
		})
}

func postgresStartupLogWriter(layout Layout) (io.Writer, func()) {
	if strings.TrimSpace(layout.LogsDir) == "" {
		return io.Discard, nil
	}
	if err := os.MkdirAll(layout.LogsDir, 0o700); err != nil {
		return io.Discard, nil
	}
	logPath := filepath.Join(layout.LogsDir, "postgres.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- path is program-constructed under operator-controlled layout.LogsDir
	if err != nil {
		return io.Discard, nil
	}
	return logFile, func() { _ = logFile.Close() }
}

// PostgresDSN returns the loopback TCP connection string for the local workspace database.
func PostgresDSN(host string, port int) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host,
		port,
		localPostgresUser,
		localPostgresPassword,
		localPostgresDatabase,
	)
}

// LocalQueryProfile returns the query-profile name used for local lightweight mode.
func LocalQueryProfile() string {
	return localQueryProfileName
}

func runtimeSocketDir(layout Layout, baseTempDir string) string {
	primary := filepath.Join(baseTempDir, "eshu", layout.WorkspaceID)
	if socketPathLength(postgresSocketPath(primary, 65535)) <= 103 {
		return primary
	}

	shortID := layout.WorkspaceID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	for _, base := range []string{"/tmp", "/private/tmp"} {
		candidate := filepath.Join(base, "eshu", shortID)
		if socketPathLength(postgresSocketPath(candidate, 65535)) <= 103 {
			return candidate
		}
	}
	return filepath.Join("/tmp", "eshu", shortID)
}

func postgresSocketPath(socketDir string, port int) string {
	return filepath.Join(socketDir, fmt.Sprintf(".s.PGSQL.%d", port))
}

func socketPathLength(path string) int {
	return len(path)
}

func reserveLocalPostgresPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(localPostgresHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("reserve local postgres port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("reserve local postgres port: invalid tcp address %T", listener.Addr())
	}
	return addr.Port, nil
}

func readPostmasterPID(dataDir string) (int, error) {
	content, err := os.ReadFile(filepath.Join(dataDir, "postmaster.pid")) // #nosec G304 -- path is program-constructed from the embedded-postgres data directory
	if err != nil {
		return 0, fmt.Errorf("read embedded postgres pid: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return 0, fmt.Errorf("read embedded postgres pid: empty pid file")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, fmt.Errorf("read embedded postgres pid: %w", err)
	}
	return pid, nil
}

// stopOrphanedPostgresFromLockFile reclaims a live Postgres left behind after
// owner metadata was removed but the workspace owner lock became available.
func stopOrphanedPostgresFromLockFile(dataDir string) error {
	lock, err := readPostmasterLockFile(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if lock.pid <= 0 || lock.port <= 0 || lock.socketPath == "" {
		return nil
	}
	if !ProcessAlive(lock.pid) || !SocketHealthy(lock.socketPath) {
		return nil
	}
	if !postmasterLockPostgresReady(lock) {
		return nil
	}
	if err := StopEmbeddedPostgres(dataDir); err != nil {
		return fmt.Errorf("stop orphaned embedded postgres: %w", err)
	}
	if SocketHealthy(lock.socketPath) {
		return fmt.Errorf("%w: pid=%d socket=%q data_dir=%q", ErrEmbeddedPostgresActive, lock.pid, lock.socketPath, dataDir)
	}
	return nil
}

type postmasterLockFile struct {
	pid        int
	port       int
	socketPath string
}

func readPostmasterLockFile(dataDir string) (postmasterLockFile, error) {
	content, err := os.ReadFile(filepath.Join(dataDir, "postmaster.pid")) // #nosec G304 -- path is program-constructed from the embedded-postgres data directory
	if err != nil {
		return postmasterLockFile{}, err
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) < 5 {
		return postmasterLockFile{}, nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return postmasterLockFile{}, nil
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[3]))
	if err != nil || port <= 0 {
		return postmasterLockFile{pid: pid}, nil
	}
	socketDir := strings.TrimSpace(lines[4])
	if socketDir == "" {
		return postmasterLockFile{pid: pid}, nil
	}
	return postmasterLockFile{
		pid:        pid,
		port:       port,
		socketPath: postgresSocketPath(socketDir, port),
	}, nil
}

func postgresReadyFromPostmasterLock(lock postmasterLockFile) bool {
	if lock.port <= 0 {
		return false
	}
	db, err := sql.Open("pgx", PostgresDSN(localPostgresHost, lock.port))
	if err != nil {
		return false
	}
	defer func() {
		_ = db.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), localProbeTimeout)
	defer cancel()
	return db.PingContext(ctx) == nil
}
