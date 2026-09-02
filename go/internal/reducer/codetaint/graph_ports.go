// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetaint

import (
	"context"
	"time"
)

// GraphQueryRunner executes read-only graph queries for the taint/interproc
// backfill readers. It is declared locally rather than imported from the
// reducer root: the root's own GraphQueryRunner (infrastructure_platform_lookup.go)
// is genuine root-owned logic shared by several families that have not moved
// out of root yet, so importing it would violate the rule that a family
// subpackage never imports the reducer root (issue #6061). Go interfaces are
// satisfied structurally, so the same concrete graph-query implementation
// root wires into other families' readers also satisfies this local
// declaration without any code duplication.
type GraphQueryRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// CodeValueFlowBackfillStateMarker provides durable per-source completion
// markers for the taint/interproc ledger backfills so a partially failed
// backfill re-runs on the next startup instead of being treated as done.
//
// Declared locally for the same reason as GraphQueryRunner above: the root's
// CodeValueFlowBackfillStateMarker (code_value_flow_backfill_state_marker.go)
// is shared with the projected_source_edge_backfill family, which has not
// moved out of root, so this package cannot import it. cmd/reducer wires the
// same concrete Postgres-backed marker into both the root-staying caller and
// this package's backfillers; structural typing makes that safe.
type CodeValueFlowBackfillStateMarker interface {
	IsComplete(ctx context.Context, key string) (bool, error)
	MarkComplete(ctx context.Context, key string, at time.Time) error
}

// derefFloat64 returns the pointed-to float64, or 0 for a nil pointer.
// Mirrors payloadcore.DerefBool/DerefString, but not itself hoisted there:
// the reducer root's derefFloat64 (supply_chain_impact_match.go) is real
// logic used elsewhere in root for an unrelated domain (vulnerability.cve),
// so this package keeps its own copy rather than importing root for one
// four-line nil-guard.
func derefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
