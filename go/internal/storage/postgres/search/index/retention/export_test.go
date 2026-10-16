// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

// SetAfterBatch installs a hook that runs after each batch of the budget, so
// external live tests can cancel or race a drain between two batches.
func SetAfterBatch(budget *Budget, hook func()) { budget.afterBatch = hook }
