// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

import "context"

// GraphQueryRunner executes read-only graph queries for the SATISFIED_BY
// edge existence confirmation (issue #5476 P1-b). It is declared locally
// rather than imported from the reducer root: the root's own GraphQueryRunner
// (infrastructure_platform_lookup.go) is genuine root-owned logic shared by
// several families that have not moved out of root yet, so importing it
// would violate the rule that a family subpackage never imports the reducer
// root (issue #6061). Go interfaces are satisfied structurally, so the same
// concrete graph-query implementation root wires into other families'
// readers also satisfies this local declaration without any code
// duplication.
type GraphQueryRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}
