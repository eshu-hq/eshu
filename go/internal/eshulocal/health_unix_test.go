//go:build !windows

package eshulocal

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
