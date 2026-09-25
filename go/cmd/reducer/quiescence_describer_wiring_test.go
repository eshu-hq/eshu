// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/projection"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestCanonicalQuiescenceCheckerNamesItsBlockers pins the #7133 wiring: the
// code-call runner names blocking scopes only through an optional type
// assertion, so if the production checker stopped implementing the describer
// port the lane-blocked log would silently lose its scope ids.
func TestCanonicalQuiescenceCheckerNamesItsBlockers(t *testing.T) {
	t.Parallel()

	var checker projection.CanonicalCodeQuiescenceChecker = postgres.NewReducerGraphDrain(nil)
	if _, ok := checker.(projection.CanonicalCodeQuiescenceDescriber); !ok {
		t.Fatal("postgres.ReducerGraphDrain does not implement projection.CanonicalCodeQuiescenceDescriber")
	}
}
