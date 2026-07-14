// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifarepodependencyproof

package reducer

import "context"

// runRepoDependencyProjection preserves the shipped global 0/1 lane in every
// normal and production build. The proof-only worker coordinator is excluded.
func runRepoDependencyProjection(ctx context.Context, runner *RepoDependencyProjectionRunner) error {
	return runner.runSerial(ctx)
}
