//go:build !windows

package eshulocal

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const localProbeTimeout = 250 * time.Millisecond

var (
	pgCtlLookPath = exec.LookPath
	pgCtlRunner   = runPgCtlCommand
)

// DefaultReclaimDeps returns the production reclaim probes for Unix hosts.
func DefaultReclaimDeps() ReclaimDeps {
	return ReclaimDeps{
		PIDAlive:      ProcessAlive,
		SocketHealthy: SocketHealthy,
		StopPostgres:  StopEmbeddedPostgres,
	}
}

// ProcessAlive reports whether the target PID still appears to exist.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

// SocketHealthy reports whether a Unix socket accepts connections.
func SocketHealthy(path string) bool {
	if path == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", path, localProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// StopEmbeddedPostgres requests a fast shutdown for the embedded Postgres instance.
func StopEmbeddedPostgres(dataDir string) error {
	if dataDir == "" {
		return fmt.Errorf("postgres data directory is required")
	}

	pgCtlPath := derivedPgCtlPath(dataDir)
	if _, err := os.Stat(pgCtlPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat embedded pg_ctl: %w", err)
		}
		pgCtlPath, err = pgCtlLookPath("pg_ctl")
		if err != nil {
			return fmt.Errorf("find pg_ctl: %w", err)
		}
	}
	if err := pgCtlRunner(pgCtlPath, "-D", dataDir, "stop", "-m", "fast"); err != nil {
		return fmt.Errorf("run pg_ctl fast stop: %w", err)
	}
	return nil
}

func derivedPgCtlPath(dataDir string) string {
	return filepath.Join(filepath.Dir(dataDir), "binaries", "bin", "pg_ctl")
}

func runPgCtlCommand(binary string, args ...string) error {
	cmd := exec.Command(binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}
