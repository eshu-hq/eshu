// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// restartContainer returns the owned disposable primary container that the
// restart tests may stop, kill, and start. Both the explicit opt-in flag and
// the container name are required; no fixture target is embedded here.
func restartContainer(t *testing.T) string {
	t.Helper()
	container := strings.TrimSpace(os.Getenv("ESHU_READER_TEST_PRIMARY_CONTAINER"))
	if os.Getenv("ESHU_READER_TEST_RESTART_PRIMARY") != "1" || container == "" {
		t.Skip("owned primary restart requires explicit fixture flag and container target")
	}
	return container
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// eventually retries probe until it succeeds or the bound expires, returning
// the last error so a wedged Access reports why it never recovered.
func eventually(bound time.Duration, probe func() error) error {
	deadline := time.Now().Add(bound)
	var err error
	for time.Now().Before(deadline) {
		if err = probe(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// requireRecovered proves the same Access serves a writer statement, a
// checkpoint, a fenced read, and readiness after the primary came back.
func requireRecovered(t *testing.T, access *Access) {
	t.Helper()
	err := eventually(30*time.Second, func() error {
		if _, err := access.Writer().ExecContext(context.Background(), "SELECT 1"); err != nil {
			return fmt.Errorf("writer select: %w", err)
		}
		ctx, err := access.ContextWithCheckpoint(context.Background())
		if err != nil {
			return fmt.Errorf("checkpoint: %w", err)
		}
		var got int
		if err := access.Reader().QueryRowContext(ctx, "SELECT 7").Scan(&got); err != nil || got != 7 {
			return fmt.Errorf("fenced read=%d: %w", got, err)
		}
		return access.Ping(context.Background())
	})
	if err != nil {
		t.Fatalf("same Access never recovered after a same-cluster restart: %v", err)
	}
	// Two further rounds must succeed outright: recovery is not one lucky pass.
	for range 2 {
		if _, err := access.ContextWithCheckpoint(context.Background()); err != nil {
			t.Fatalf("checkpoint after recovery: %v", err)
		}
	}
}

func TestAccessSameClusterRestartRecoversInPlace(t *testing.T) {
	container := restartContainer(t)
	access := testAccess(t, "")
	if _, err := access.ContextWithCheckpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := docker(t, "inspect", "--format", "{{.Config.StopSignal}}", container); got != "SIGINT" {
		t.Fatalf("plain restart expects a fast-shutdown image, stop signal=%q", got)
	}
	docker(t, "stop", container)
	docker(t, "start", container)
	requireRecovered(t, access)
}

// TestAccessRestartBeforeAnyCheckpointRecovers covers the watermark seeded by
// bootstrap alone: no checkpoint or request ran before the restart.
func TestAccessRestartBeforeAnyCheckpointRecovers(t *testing.T) {
	container := restartContainer(t)
	access := testAccess(t, "")
	docker(t, "stop", container)
	docker(t, "start", container)
	requireRecovered(t, access)
}

// TestAccessTwoQuickRestartsWithConcurrentDials restarts the primary twice in
// quick succession while workers keep dialing the writer and reader pools.
// Every worker must converge on the final incarnation with no latch.
func TestAccessTwoQuickRestartsWithConcurrentDials(t *testing.T) {
	container := restartContainer(t)
	access := testAccess(t, "")
	access.writer.SetConnMaxLifetime(50 * time.Millisecond)
	access.reader.SetConnMaxLifetime(50 * time.Millisecond)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var ops atomic.Int64
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				if checked, err := access.ContextWithCheckpoint(ctx); err == nil {
					var got int
					if access.Reader().QueryRowContext(checked, "SELECT 1").Scan(&got) == nil {
						ops.Add(1)
					}
				}
				cancel()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	docker(t, "restart", "--time", "5", container)
	docker(t, "restart", "--time", "5", container)
	before := ops.Load()
	err := eventually(30*time.Second, func() error {
		if ops.Load() < before+50 {
			return errors.New("concurrent workers have not resumed")
		}
		return nil
	})
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("%v (ops before=%d after=%d)", err, before, ops.Load())
	}
	requireRecovered(t, access)
}
