// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "context"

// GraphQueryRunner executes read-only graph queries for reducer lookups
// (moved here from the reducer root's infrastructure_platform_lookup.go,
// issue #6061).
type GraphQueryRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}
