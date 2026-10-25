// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection && unix

package executor

import (
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestFaultingExecutorHoldFiredWaitsForMarkerWrite pins the publish order
// HoldFired promises: an observer that sees HoldFired() == true also sees the
// hold marker, unless its write failed. The marker path is a FIFO, so the
// engaging goroutine's os.WriteFile blocks on open until this test reads it.
// That freezes the executor between engaging the hold and finishing the
// marker write, which is the window where
// TestFaultingExecutorHoldBlocksExecuteUntilSentinelRemoval
// failed intermittently (18 of 3000 runs at -cpu=4) by reading a marker that
// did not exist yet.
func TestFaultingExecutorHoldFiredWaitsForMarkerWrite(t *testing.T) {
	t.Parallel()

	inner := &faultRecordingExecutor{}
	sentinel := holdSentinel(t)
	marker := sentinel + holdFiredMarkerSuffix
	if err := syscall.Mkfifo(marker, 0o600); err != nil {
		t.Fatalf("mkfifo hold marker: %v", err)
	}
	fe := mustFaultingExecutor(t, inner, holdScript("USES_VAULT_POLICY"), sentinel)

	const matched = "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"
	released := make(chan error, 1)
	go func() {
		released <- fe.Execute(context.Background(), cypher.Statement{Cypher: matched})
	}()

	// The writer is now blocked opening the FIFO. Give it time to get there,
	// then require HoldFired to still be false: the marker is not written.
	time.Sleep(200 * time.Millisecond)
	if fe.HoldFired() {
		t.Fatal("HoldFired() = true while the marker write is still blocked; an observer would read a marker that does not exist")
	}

	reader, err := os.OpenFile(marker, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open hold marker FIFO for reading: %v", err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read hold marker FIFO: %v", err)
	}
	if !strings.Contains(string(body), matched) {
		t.Errorf("hold marker %q does not name the matched statement %q", string(body), matched)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !fe.HoldFired() {
		if time.Now().After(deadline) {
			t.Fatal("HoldFired() stayed false after the marker write completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := inner.executeCount(); got != 0 {
		t.Fatalf("inner.Execute called %d times while held, want 0", got)
	}

	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("remove sentinel: %v", err)
	}
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("Execute after release = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return within 5s of sentinel removal")
	}
}
