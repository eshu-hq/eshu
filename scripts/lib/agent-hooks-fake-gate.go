// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ignore

// Command agent-hooks-fake-gate stands in for a running ci-gates process in the
// hook test mirrors (#9098). scripts/lib/agent-hooks-fake-gate.sh builds it
// into a temporary directory as an executable named ci-gates, so that
// `pgrep -x ci-gates` lists it. A compiled binary keeps its own name on macOS
// and Linux; a script process is named after its interpreter on macOS.
//
// It runs no gate. It waits for SIGTERM or SIGINT, or exits on its own after a
// safety timeout so a test that dies without cleanup cannot leave a process
// that blocks the live-gate guard for long.
package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// safetyTimeout bounds the lifetime of an orphaned fake.
const safetyTimeout = 120 * time.Second

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-signals:
	case <-time.After(safetyTimeout):
	}
}
