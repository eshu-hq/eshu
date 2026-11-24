// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// TestPlanGoReachabilityScopeRefusesMissingFlags proves the one-shot validates
// its required flags before opening any connection: a missing flag usage-errors
// without touching the database or the config.
func TestPlanGoReachabilityScopeRefusesMissingFlags(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runPlanGoReachabilityScope(context.Background(), []string{"--instance-id", "only-this"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--request-id") {
		t.Fatalf("stderr = %q, want the required-flags usage", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no outcome document on a usage error", stdout.String())
	}
}

// TestPlanGoReachabilityScopeRejectsBadFlags proves an unparsable flag set
// exits before any wiring runs: no telemetry, no store, no network.
func TestPlanGoReachabilityScopeRejectsBadFlags(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runPlanGoReachabilityScope(context.Background(), []string{"--bogus-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no outcome document on a usage error", stdout.String())
	}
}

// TestPlanGoReachabilityScopeSurfacesDatabaseFailure proves a missing role
// contract fails the run before any planning: with the boundary required and
// no contract configured, OpenPostgres returns before connecting, the command
// exits 2, and no outcome document is written.
func TestPlanGoReachabilityScopeSurfacesDatabaseFailure(t *testing.T) {
	// Manipulates process env; must not run in parallel.
	mustSetenv(t, "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED", "true")
	mustUnsetenv(t, "ESHU_POSTGRES_ROLE_CONTRACT_JSON")
	mustUnsetenv(t, "OTEL_EXPORTER_OTLP_ENDPOINT")

	var stdout, stderr bytes.Buffer
	code := runPlanGoReachabilityScope(context.Background(), []string{
		"--instance-id", "scanner-go-reachability-test",
		"--repository-id", "repository:r_efdfb126",
		"--request-id", "10032-fixture-20261123",
		"--requested-by", "test",
		"--reason", "test",
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no outcome document when the database cannot open", stdout.String())
	}
}

// TestPlanGoReachabilityScopeWrapperDelegates proves the signal-handling entry
// point reaches the same validation as the direct call.
func TestPlanGoReachabilityScopeWrapperDelegates(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runOperatorPlanWithSignals([]string{"--instance-id", "only-this"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--request-id") {
		t.Fatalf("stderr = %q, want the required-flags usage through the wrapper", stderr.String())
	}
}

// TestOperatorCommandRoutesOnlyTheOneShot proves argv routing: the operator
// subcommand name delegates with its exit code, anything else falls through
// to the reconciler path untouched.
func TestOperatorCommandRoutesOnlyTheOneShot(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code, handled := runOperatorCommand([]string{"plan-go-reachability-scope", "--instance-id", "only-this"}, &stdout, &stderr)
	if !handled || code != 2 {
		t.Fatalf("handled = %v, exit = %d; want handled with exit 2", handled, code)
	}

	var otherOut, otherErr bytes.Buffer
	if code, handled := runOperatorCommand([]string{"--some-flag"}, &otherOut, &otherErr); handled || code != 0 {
		t.Fatalf("handled = %v, exit = %d; want fall-through with exit 0", handled, code)
	}

	var emptyOut, emptyErr bytes.Buffer
	if code, handled := runOperatorCommand(nil, &emptyOut, &emptyErr); handled || code != 0 {
		t.Fatalf("handled = %v, exit = %d; want fall-through on empty argv", handled, code)
	}
}

func mustSetenv(t *testing.T, key, value string) {
	t.Helper()
	if prev, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { _ = os.Setenv(key, prev) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Setenv(%s) error = %v", key, err)
	}
}

func mustUnsetenv(t *testing.T, key string) {
	t.Helper()
	if prev, ok := os.LookupEnv(key); ok {
		t.Cleanup(func() { _ = os.Setenv(key, prev) })
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s) error = %v", key, err)
	}
}
