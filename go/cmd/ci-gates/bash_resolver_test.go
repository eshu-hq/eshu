// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBashAtLeast44_RealHomebrewBash proves bashAtLeast44 against a real
// bash >= 4.4 binary (macOS Homebrew installs 5.x). Skipped when the path is
// absent (e.g. Linux CI) so the test stays hermetic there rather than
// asserting anything about that host's package layout.
func TestBashAtLeast44_RealHomebrewBash(t *testing.T) {
	const homebrewBash = "/opt/homebrew/bin/bash"
	if _, err := os.Stat(homebrewBash); err != nil {
		t.Skipf("%s not present on this host: %v", homebrewBash, err)
	}
	if !bashAtLeast44(homebrewBash) {
		t.Errorf("bashAtLeast44(%q) = false, want true", homebrewBash)
	}
}

// TestBashAtLeast44_FakeOldBash proves bashAtLeast44 returns false for a
// candidate that does not satisfy the version guard. The fixture is a
// hermetic stand-in for a bash < 4.4 binary (real /bin/bash is 3.2.57 on
// macOS but may already be >= 4.x on Linux CI, so a real system bash is not
// a portable negative case): a tiny /bin/sh script that unconditionally
// exits 1, mirroring exactly what bashAtLeast44 observes from a genuine old
// bash asked to evaluate the `BASH_VERSINFO` guard expression.
func TestBashAtLeast44_FakeOldBash(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-old-bash")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture needs to be executable
		t.Fatalf("write fake bash fixture: %v", err)
	}
	if bashAtLeast44(fake) {
		t.Errorf("bashAtLeast44(%q) = true, want false", fake)
	}
}

// TestBashAtLeast44_FakeNewBash mirrors TestBashAtLeast44_FakeOldBash for the
// positive case: a fixture that unconditionally exits 0, standing in for a
// bash >= 4.4 binary whose guard expression evaluates to 0 (condition false).
func TestBashAtLeast44_FakeNewBash(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-new-bash")
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture needs to be executable
		t.Fatalf("write fake bash fixture: %v", err)
	}
	if !bashAtLeast44(fake) {
		t.Errorf("bashAtLeast44(%q) = false, want true", fake)
	}
}

// TestBashAtLeast44_MissingBinary proves a nonexistent path is treated as
// not qualifying rather than panicking or blocking. runShellCommand's caller
// (resolveBash44Dir) relies on this to skip a broken candidate cleanly.
func TestBashAtLeast44_MissingBinary(t *testing.T) {
	if bashAtLeast44(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Error("bashAtLeast44(missing path) = true, want false")
	}
}

// TestResolveBash44Dir_FallsBackCleanly proves the resolver never errors out:
// it either returns "" (no qualifying candidate found — the caller must
// leave PATH/env unchanged) or a directory that itself contains a bash
// binary which independently satisfies bashAtLeast44. This holds on every
// host without hardcoding an expected candidate, since CI's Linux `bash` via
// PATH already qualifies (Linux distros ship bash >= 4.x) while this Mac's
// PATH-resolved `bash` (/bin/bash 3.2.57) does not and the fallback
// candidates take over.
func TestResolveBash44Dir_FallsBackCleanly(t *testing.T) {
	dir := resolveBash44Dir()
	if dir == "" {
		t.Skip("no qualifying bash >= 4.4 candidate found on this host")
	}
	resolved := filepath.Join(dir, "bash")
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("resolveBash44Dir returned %q, but %q does not exist: %v", dir, resolved, err)
	}
	if !bashAtLeast44(resolved) {
		t.Errorf("resolveBash44Dir returned %q, but %q does not itself qualify as bash >= 4.4", dir, resolved)
	}
}

// TestGateSubprocessEnvPrependsResolvedBashDir proves the PATH-steering wiring
// runShellCommand depends on: when a bash >= 4.4 resolves, gateSubprocessEnv
// must return an environment whose effective PATH begins with that bash's
// directory, so a gate command's inner `bash scripts/*.sh` token resolves to
// it. When none resolves it must return nil ("inherit unchanged"). This closes
// the false-green gap the review flagged — deleting the prepend leaves the
// resolver tests green but fails this one (#5050 review P2).
func TestGateSubprocessEnvPrependsResolvedBashDir(t *testing.T) {
	dir := resolveBash44Dir()
	env := gateSubprocessEnv()

	if dir == "" {
		if env != nil {
			t.Fatalf("no qualifying bash resolved, but gateSubprocessEnv returned non-nil env %v; want nil (inherit unchanged)", env)
		}
		return
	}

	// os/exec keeps the LAST PATH= entry, so the effective value is the last one.
	pathVal := ""
	seen := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathVal = strings.TrimPrefix(kv, "PATH=")
			seen = true
		}
	}
	if !seen {
		t.Fatal("gateSubprocessEnv returned an env with no PATH entry")
	}
	// Assert the exact prepend, not just "starts with dir": an exact match can
	// never false-green on a host where dir already happens to be first in PATH.
	want := dir + string(os.PathListSeparator) + os.Getenv("PATH")
	if pathVal != want {
		t.Fatalf("effective PATH is not the resolved bash dir prepended\n  want: %q\n  got:  %q", want, pathVal)
	}
}
