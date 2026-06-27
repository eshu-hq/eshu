// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// Shared-projection intent actions. filterUpsertRows (shared_projection_readiness.go)
// keeps only rows whose payload action is "upsert"; "retract" rows drive the
// repo-wide retract path. Used by the code-call, inheritance, rationale, SQL, and
// documentation shared-projection intent builders.
const (
	IntentActionUpsert  = "upsert"
	IntentActionRetract = "retract"
)
