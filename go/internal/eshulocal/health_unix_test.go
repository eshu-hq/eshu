//go:build !windows

package eshulocal

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func TestProcessAlive(t *testing.T) {
	if ProcessAlive(0) {
		t.Fatal("ProcessAlive(0) = true, want false")
	}
	if !ProcessAlive(os.Getpid()) {
		t.Fatalf("ProcessAlive(%d) = false, want true", os.Getpid())
	}
	if ProcessAlive(999999) {
		t.Fatal("ProcessAlive(999999) = true, want false")
	}
}

func TestSocketHealthy(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "eshu.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen(unix) error = %v, want nil", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	if !SocketHealthy(socketPath) {
		t.Fatalf("SocketHealthy(%q) = false, want true", socketPath)
	}
	<-acceptDone

	if SocketHealthy(filepath.Join(t.TempDir(), "missing.sock")) {
		t.Fatal("SocketHealthy(missing) = true, want false")
	}
}

func TestStopEmbeddedPostgresUsesPgCtlFastStop(t *testing.T) {
	originalLookPath := pgCtlLookPath
	originalRunner := pgCtlRunner
	defer func() {
		pgCtlLookPath = originalLookPath
		pgCtlRunner = originalRunner
	}()

	var gotBinary string
	var gotArgs []string
	pgCtlLookPath = func(file string) (string, error) {
		if file != "pg_ctl" {
			t.Fatalf("LookPath() file = %q, want %q", file, "pg_ctl")
		}
		return "/tmp/pg_ctl", nil
	}
	pgCtlRunner = func(binary string, args ...string) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		return nil
	}

	dataDir := filepath.Join(t.TempDir(), "postgres")
	if err := StopEmbeddedPostgres(dataDir); err != nil {
		t.Fatalf("StopEmbeddedPostgres() error = %v, want nil", err)
	}

	if gotBinary != "/tmp/pg_ctl" {
		t.Fatalf("runner binary = %q, want %q", gotBinary, "/tmp/pg_ctl")
	}
	wantArgs := []string{"-D", dataDir, "stop", "-m", "fast"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("runner args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestStopEmbeddedPostgresPrefersWorkspacePgCtl(t *testing.T) {
	originalLookPath := pgCtlLookPath
	originalRunner := pgCtlRunner
	defer func() {
		pgCtlLookPath = originalLookPath
		pgCtlRunner = originalRunner
	}()

	pgCtlLookPath = func(file string) (string, error) {
		t.Fatal("LookPath() should not be called when workspace pg_ctl exists")
		return "", nil
	}

	var gotBinary string
	pgCtlRunner = func(binary string, args ...string) error {
		gotBinary = binary
		return nil
	}

	root := t.TempDir()
	dataDir := filepath.Join(root, "postgres", "data")
	if err := os.MkdirAll(filepath.Join(root, "postgres", "binaries", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(dataDir) error = %v, want nil", err)
	}
	workspacePgCtl := filepath.Join(root, "postgres", "binaries", "bin", "pg_ctl")
	if err := os.WriteFile(workspacePgCtl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(pg_ctl) error = %v, want nil", err)
	}

	if err := StopEmbeddedPostgres(dataDir); err != nil {
		t.Fatalf("StopEmbeddedPostgres() error = %v, want nil", err)
	}
	if gotBinary != workspacePgCtl {
		t.Fatalf("runner binary = %q, want %q", gotBinary, workspacePgCtl)
	}
}

func TestStopOrphanedPostgresFromLockFileStopsLiveWorkspacePostgres(t *testing.T) {
	originalRunner := pgCtlRunner
	originalReady := postmasterLockPostgresReady
	defer func() {
		pgCtlRunner = originalRunner
		postmasterLockPostgresReady = originalReady
	}()

	socketDir, err := os.MkdirTemp("/tmp", "eshu-pg-sock-")
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp) error = %v, want nil", err)
	}
	defer func() {
		_ = os.RemoveAll(socketDir)
	}()
	listener, err := net.Listen("unix", postgresSocketPath(socketDir, 62261))
	if err != nil {
		t.Fatalf("net.Listen(unix) error = %v, want nil", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	root := t.TempDir()
	dataDir := filepath.Join(root, "postgres", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(dataDir) error = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "postgres", "binaries", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(binaries) error = %v, want nil", err)
	}
	workspacePgCtl := filepath.Join(root, "postgres", "binaries", "bin", "pg_ctl")
	if err := os.WriteFile(workspacePgCtl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(pg_ctl) error = %v, want nil", err)
	}
	writePostmasterPIDForTest(t, dataDir, os.Getpid(), 62261, socketDir)

	var gotBinary string
	var gotArgs []string
	postmasterLockPostgresReady = func(lock postmasterLockFile) bool {
		return lock.port == 62261
	}
	pgCtlRunner = func(binary string, args ...string) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		if err := listener.Close(); err != nil {
			t.Fatalf("listener.Close() error = %v, want nil", err)
		}
		return nil
	}

	if err := stopOrphanedPostgresFromLockFile(dataDir); err != nil {
		t.Fatalf("stopOrphanedPostgresFromLockFile() error = %v, want nil", err)
	}
	<-acceptDone

	if gotBinary != workspacePgCtl {
		t.Fatalf("runner binary = %q, want %q", gotBinary, workspacePgCtl)
	}
	wantArgs := []string{"-D", dataDir, "stop", "-m", "fast"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("runner args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestStopOrphanedPostgresFromLockFileRequiresPostgresProtocol(t *testing.T) {
	originalRunner := pgCtlRunner
	originalReady := postmasterLockPostgresReady
	defer func() {
		pgCtlRunner = originalRunner
		postmasterLockPostgresReady = originalReady
	}()

	socketDir, err := os.MkdirTemp("/tmp", "eshu-pg-sock-")
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp) error = %v, want nil", err)
	}
	defer func() {
		_ = os.RemoveAll(socketDir)
	}()
	listener, err := net.Listen("unix", postgresSocketPath(socketDir, 62261))
	if err != nil {
		t.Fatalf("net.Listen(unix) error = %v, want nil", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	root := t.TempDir()
	dataDir := filepath.Join(root, "postgres", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(dataDir) error = %v, want nil", err)
	}
	writePostmasterPIDForTest(t, dataDir, os.Getpid(), 62261, socketDir)

	postmasterLockPostgresReady = func(lock postmasterLockFile) bool {
		return false
	}
	pgCtlRunner = func(binary string, args ...string) error {
		t.Fatal("pgCtlRunner should not be called when the recorded port does not answer as Postgres")
		return nil
	}

	if err := stopOrphanedPostgresFromLockFile(dataDir); err != nil {
		t.Fatalf("stopOrphanedPostgresFromLockFile() error = %v, want nil", err)
	}
	<-acceptDone
}

func TestStartEmbeddedPostgresStopsOwnerlessLivePostgresBeforeStart(t *testing.T) {
	originalNewEmbeddedPostgres := newEmbeddedPostgres
	originalRunner := pgCtlRunner
	originalReady := postmasterLockPostgresReady
	defer func() {
		newEmbeddedPostgres = originalNewEmbeddedPostgres
		pgCtlRunner = originalRunner
		postmasterLockPostgresReady = originalReady
	}()

	socketDir, err := os.MkdirTemp("/tmp", "eshu-pg-sock-")
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp) error = %v, want nil", err)
	}
	defer func() {
		_ = os.RemoveAll(socketDir)
	}()
	listener, err := net.Listen("unix", postgresSocketPath(socketDir, 62261))
	if err != nil {
		t.Fatalf("net.Listen(unix) error = %v, want nil", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	root := t.TempDir()
	layout := Layout{
		WorkspaceID: "workspace-id",
		PostgresDir: filepath.Join(root, "postgres"),
		CacheDir:    filepath.Join(root, "cache"),
	}
	dataDir := filepath.Join(layout.PostgresDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(dataDir) error = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(layout.PostgresDir, "binaries", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(binaries) error = %v, want nil", err)
	}
	workspacePgCtl := filepath.Join(layout.PostgresDir, "binaries", "bin", "pg_ctl")
	if err := os.WriteFile(workspacePgCtl, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(pg_ctl) error = %v, want nil", err)
	}
	writePostmasterPIDForTest(t, dataDir, os.Getpid(), 62261, socketDir)

	stopCalled := false
	postmasterLockPostgresReady = func(lock postmasterLockFile) bool {
		return lock.port == 62261
	}
	pgCtlRunner = func(binary string, args ...string) error {
		if binary != workspacePgCtl {
			t.Fatalf("runner binary = %q, want %q", binary, workspacePgCtl)
		}
		stopCalled = true
		if err := listener.Close(); err != nil {
			t.Fatalf("listener.Close() error = %v, want nil", err)
		}
		return nil
	}
	wantErr := errors.New("start reached")
	newEmbeddedPostgres = func(config embeddedpostgres.Config) embeddedPostgresRuntime {
		return fakeEmbeddedPostgresRuntime{start: func() error {
			if !stopCalled {
				t.Fatal("StartEmbeddedPostgres started postgres before stopping ownerless live postgres")
			}
			return wantErr
		}}
	}

	_, err = StartEmbeddedPostgres(context.Background(), layout)
	if !errors.Is(err, wantErr) {
		t.Fatalf("StartEmbeddedPostgres() error = %v, want %v", err, wantErr)
	}
	<-acceptDone
}

func TestDefaultReclaimDepsUseDefaultProbes(t *testing.T) {
	deps := DefaultReclaimDeps()
	if deps.PIDAlive == nil {
		t.Fatal("DefaultReclaimDeps().PIDAlive = nil, want non-nil")
	}
	if deps.SocketHealthy == nil {
		t.Fatal("DefaultReclaimDeps().SocketHealthy = nil, want non-nil")
	}
	if deps.StopPostgres == nil {
		t.Fatal("DefaultReclaimDeps().StopPostgres = nil, want non-nil")
	}
}

type fakeEmbeddedPostgresRuntime struct {
	start func() error
}

func (f fakeEmbeddedPostgresRuntime) Start() error {
	if f.start == nil {
		return nil
	}
	return f.start()
}

func (f fakeEmbeddedPostgresRuntime) Stop() error {
	return nil
}

func writePostmasterPIDForTest(t *testing.T, dataDir string, pid, port int, socketDir string) {
	t.Helper()

	content := strings.Join([]string{
		strconv.Itoa(pid),
		dataDir,
		"1715010000",
		strconv.Itoa(port),
		socketDir,
		"localhost",
		"0",
		"ready",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dataDir, "postmaster.pid"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(postmaster.pid) error = %v, want nil", err)
	}
}
