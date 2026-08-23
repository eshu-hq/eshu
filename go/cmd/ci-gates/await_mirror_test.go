// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// The await exit codes are a three-way contract: the constants below, the
// `case "${AGGREGATE_CODE}"` arms in .github/workflows/required-gates.yml, and
// the static validator in internal/cigates that keeps those arms honest. That
// validator cannot import these constants -- they are unexported in package
// main -- so it re-declares them, and a re-declared constant is a constant
// that can drift.
//
// requiredworkflow_publisher.go has claimed since #6075 that
// TestStillRunningCodeMatchesAwaitContract asserts the pair. It did not exist;
// the mirror was held together by a comment naming a test nobody had written,
// which is exactly the shape of guard that looks green while proving nothing
// (#6189 found the same class of gap in the classification itself). These are
// that test, and the one the #6189 code needs.

// mirroredAwaitExitCode reads one mirrored constant out of
// internal/cigates/requiredworkflow_publisher.go by parsing the file, not by
// searching it for a string. A fixed-string search would still pass if the
// declaration were commented out or moved into dead code.
func mirroredAwaitExitCode(t *testing.T, name string) int {
	t.Helper()

	path := filepath.Join(repoRoot(t), "go", "internal", "cigates", "requiredworkflow_publisher.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range valueSpec.Names {
				if ident.Name != name || i >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					t.Fatalf("%s in %s is not an integer literal", name, path)
				}
				value, err := strconv.Atoi(lit.Value)
				if err != nil {
					t.Fatalf("%s in %s has non-numeric value %q: %v", name, path, lit.Value, err)
				}
				return value
			}
		}
	}
	t.Fatalf("%s declares no const %s; the publisher validator's mirror of the await exit codes is gone", path, name)
	return 0
}

// TestStillRunningCodeMatchesAwaitContract is the test
// requiredworkflow_publisher.go has always named. If the mirror drifts, the
// validator starts inspecting an arm that no longer exists and silently stops
// enforcing that unfinished gates never publish a terminal status (#6075).
func TestStillRunningCodeMatchesAwaitContract(t *testing.T) {
	t.Parallel()

	if got := mirroredAwaitExitCode(t, "awaitExitStillRunningCode"); got != awaitExitStillRunning {
		t.Fatalf("internal/cigates mirrors still-running as %d; awaitExitStillRunning is %d", got, awaitExitStillRunning)
	}
}

// TestGateCancelledCodeMatchesAwaitContract pins the #6189 half of the same
// mirror. Drift here would make the cancelled-arm validator inspect the wrong
// case arm and pass a publisher that still reports a cancelled gate as failed.
func TestGateCancelledCodeMatchesAwaitContract(t *testing.T) {
	t.Parallel()

	if got := mirroredAwaitExitCode(t, "awaitExitGateCancelledCode"); got != awaitExitGateCancelled {
		t.Fatalf("internal/cigates mirrors gate-cancelled as %d; awaitExitGateCancelled is %d", got, awaitExitGateCancelled)
	}
}
