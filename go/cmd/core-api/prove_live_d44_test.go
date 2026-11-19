// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const proveLiveD44Skip = "set ESHU_D44_LIVE=1 to run the Wave-6 D4.4 live proof (needs docker wave6-pg/wave6-neo4j plus an eshu-api checkout at the pin)"

// TestCoreAPILiveD44Proof drives scripts/prove-core-api-live.sh, the
// rerunnable D4.4 demonstration: freshly built eshu-core-api against live
// backends (/readyz 200 both-up, 503 per backend down with no Retry-After,
// 200 restored), byte-identical satellite relay comparisons at pin
// 08ebde4, satellite /readyz 503 vs dead core, and the latency pins.
// Opt-in only: without ESHU_D44_LIVE=1 it skips, so normal `go test`
// stays hermetic.
func TestCoreAPILiveD44Proof(t *testing.T) {
	t.Parallel()
	if os.Getenv("ESHU_D44_LIVE") != "1" {
		t.Skip(proveLiveD44Skip)
	}
	if runtime.GOOS != "linux" {
		t.Skip("the D4.4 proof drives docker containers and setsid processes (linux only)")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file for repo root")
	}
	script := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "scripts", "prove-core-api-live.sh")
	if st, err := os.Stat(script); err != nil || st.IsDir() {
		t.Fatalf("proof script missing: %s", script)
	}

	cmd := exec.Command("bash", script)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	t.Logf("prove-core-api-live.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("prove-core-api-live.sh failed: %v", err)
	}
}
