// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
)

// ExecuteOnlyExecutor forwards single-statement writes while intentionally
// hiding GroupExecutor from callers that should avoid large atomic groups.
type ExecuteOnlyExecutor struct {
	Inner Executor
}

// Execute forwards the statement to the wrapped executor.
func (e ExecuteOnlyExecutor) Execute(ctx context.Context, statement Statement) error {
	if e.Inner == nil {
		return fmt.Errorf("inner executor is required")
	}
	return e.Inner.Execute(ctx, statement)
}
