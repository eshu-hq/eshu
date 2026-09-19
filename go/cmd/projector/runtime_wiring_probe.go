// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

// RunProbe runs the bounded read-only existence probe that precedes a
// bare-label retract drain (#6822) by delegating to RunWrite: the probe must
// observe the same graph state and session kind (write session) the drain
// itself uses, so a probe run in a separate read session could not silently
// disagree with what the drain sees. Split into its own file so
// runtime_wiring.go stays under the 500-line cap.
func (e projectorNeo4jExecutor) RunProbe(
	ctx context.Context,
	cypher string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	return e.RunWrite(ctx, cypher, parameters)
}
