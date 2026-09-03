// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Shared-projection intent actions. filterUpsertRows
// (go/internal/reducer/shared_projection_readiness.go) keeps only rows whose
// payload action is IntentActionUpsert; IntentActionRetract rows drive the
// repo-wide retract path. Used by the code-call, inheritance, rationale, SQL,
// and documentation shared-projection intent builders.
const (
	// IntentActionUpsert marks a shared-projection row that must be written.
	IntentActionUpsert = "upsert"
	// IntentActionRetract marks a shared-projection row that must be removed.
	IntentActionRetract = "retract"
)
