package eshulocal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	lockHelperEnv     = "ESHULOCAL_LOCK_HELPER"
	lockHelperPathEnv = "ESHULOCAL_LOCK_PATH"
)

func TestOwnerRecordRoundTrip(t *testing.T) {
	recordPath := filepath.Join(t.TempDir(), "owner.json")
	want := OwnerRecord{
		PID:                1234,
		StartedAt:          "2026-04-22T12:00:00Z",
		Hostname:           "devbox",
		WorkspaceID:        "deadbeef",
		Version:            "v1",
		SocketPath:         "/tmp/eshu/socket",
		PostgresPID:        5678,
		PostgresPort:       15433,
		PostgresDataDir:    "/tmp/eshu/postgres",
		PostgresSocketDir:  "/tmp/eshu/socketdir",
		PostgresSocketPath: "/tmp/eshu/socketdir/.s.PGSQL.15433",
		Profile:            "local_authoritative",
		GraphBackend:       "nornicdb",
		GraphPID:           91011,
		GraphDataDir:       "/tmp/eshu/graph/nornicdb",
		GraphSocketPath:    "/tmp/eshu/graph.sock",
		GraphVersion:       "v0.0.1",
		GraphUsername:      "admin",
		GraphPassword:      "workspace-secret",
	}

	if err := WriteOwnerRecord(recordPath, want); err != nil {
		t.Fatalf("WriteOwnerRecord() error = %v, want nil", err)
	}

	got, err := ReadOwnerRecord(recordPath)
	if err != nil {
		t.Fatalf("ReadOwnerRecord() error = %v, want nil", err)
	}

	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(want) error = %v, want nil", err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(got) error = %v, want nil", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("round-trip record = %s, want %s", gotJSON, wantJSON)
	}
}

func TestAcquireOwnerLockRejectsCompetingProcess(t *testing.T) {
	if os.Getenv(lockHelperEnv) == "1" {
		runAcquireOwnerLockHelper()
		return
	}

	lockPath := filepath.Join(t.TempDir(), "owner.lock")
	cmd := exec.Command(os.Args[0], "-test.run=TestAcquireOwnerLockRejectsCompetingProcess")
	cmd.Env = append(os.Environ(), lockHelperEnv+"=1", lockHelperPathEnv+"="+lockPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v, want nil", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v, want nil", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	ready := make([]byte, len("locked\n"))
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatalf("ReadFull(stdout) error = %v, want nil", err)
	}
	if string(ready) != "locked\n" {
		t.Fatalf("helper stdout = %q, want %q", string(ready), "locked\n")
	}

	lock, err := AcquireOwnerLock(lockPath)
	if !errors.Is(err, ErrWorkspaceOwned) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("AcquireOwnerLock() error = %v, want %v", err, ErrWorkspaceOwned)
	}

	if err := stdin.Close(); err != nil {
		t.Fatalf("stdin.Close() error = %v, want nil", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v, want nil", err)
	}

	lock, err = AcquireOwnerLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireOwnerLock() after release error = %v, want nil", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("lock.Close() error = %v, want nil", err)
	}
}

func TestNilOwnerLockCloseIsSafe(t *testing.T) {
	var lock *OwnerLock
	if err := lock.Close(); err != nil {
		t.Fatalf("lock.Close() error = %v, want nil", err)
	}
}

func runAcquireOwnerLockHelper() {
	lockPath := os.Getenv(lockHelperPathEnv)
	lock, err := AcquireOwnerLock(lockPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "AcquireOwnerLock() error = %v", err)
		os.Exit(2)
	}
	defer func() {
		_ = lock.Close()
	}()

	_, _ = fmt.Fprintln(os.Stdout, "locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}
