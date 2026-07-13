// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import "context"

// DrainWriteResult carries rows and delete counters from one bounded drain step.
type DrainWriteResult struct {
	Rows                 []map[string]any
	NodesDeleted         int64
	RelationshipsDeleted int64
}

// DrainReader executes one bounded full-refresh delete step.
type DrainReader interface {
	RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error)
}
