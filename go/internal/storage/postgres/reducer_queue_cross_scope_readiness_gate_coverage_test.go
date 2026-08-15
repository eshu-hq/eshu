// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestReducerContentionPostgresProofsRunInTheReducerContentionGate is the
// hermetic enrollment guard for the three live PostgreSQL proofs, and it exists
// because a DSN-gated test that quietly skips in CI proves nothing there.
//
// The live proofs need real PostgreSQL, so they skip on a developer machine
// with no DSN. CI does provide one -- the reducer contention gate runs a
// PostgreSQL service and passes ESHU_POSTGRES_DSN -- but only for the tests its
// -run filter selects. That coupling is a test NAME matching a regex in a YAML
// file, which nothing else checks: rename either side and the proofs stop
// running in CI without a single failure.
//
// This test runs everywhere, needs no database, and reads the real workflow.
func TestReducerContentionPostgresProofsRunInTheReducerContentionGate(t *testing.T) {
	t.Parallel()

	workflowPath := filepath.Join("..", "..", "..", "..", ".github", "workflows", "reducer-contention-gate.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	if !bytes.Contains(workflow, []byte("ESHU_POSTGRES_DSN:")) {
		t.Fatalf("%s no longer passes a PostgreSQL DSN: the live proofs would skip in CI", workflowPath)
	}
	if !bytes.Contains(workflow, []byte("TestReducerContentionPostgresProofsRunInTheReducerContentionGate")) {
		t.Fatalf("%s no longer names this three-proof enrollment guard; update the guard reference in lockstep", workflowPath)
	}

	runFilter := reducerContentionGateRunFilter(t, string(workflow))
	selects, err := regexp.Compile(runFilter)
	if err != nil {
		t.Fatalf("compile the gate's -run filter %q: %v", runFilter, err)
	}
	for _, name := range []string{
		"TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository",
		"TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget",
		"TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound",
	} {
		if !selects.MatchString(name) {
			t.Fatalf("the reducer contention gate's -run filter %q does not select %s", runFilter, name)
		}
	}
}

// reducerContentionGateRunFilter extracts the single-quoted -run pattern from
// the workflow's test command, so this test reads the value CI actually uses
// rather than a copy of it.
func reducerContentionGateRunFilter(t *testing.T, workflow string) string {
	t.Helper()

	matches := regexp.MustCompile(`-run '([^']+)'`).FindStringSubmatch(workflow)
	if len(matches) != 2 {
		t.Fatal("the reducer contention gate no longer runs a single-quoted -run filter; this guard cannot read it")
	}
	return matches[1]
}
