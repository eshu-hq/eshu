// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
)

// openBootstrapDatabaseWithPoolBudget leaves a connection free while the
// run-scoped secret-lines bulk-load lock pins one connection from the pool.
func openBootstrapDatabaseWithPoolBudget(
	ctx context.Context,
	getenv func(string) string,
	openDBFn openBootstrapDBFn,
) (bootstrapDB, error) {
	if maxOpenConns := postgresMaxOpenConns(getenv); maxOpenConns < 2 {
		return nil, fmt.Errorf("ESHU_POSTGRES_MAX_OPEN_CONNS=%d: bootstrap-index requires at least 2 connections while its secret-lines bulk-load lock is held", maxOpenConns)
	}
	return openDBFn(ctx, getenv)
}
