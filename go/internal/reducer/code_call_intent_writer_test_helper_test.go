// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

// recordingCodeCallIntentWriter is a local copy of the [materialization]
// package's own recordingCodeCallIntentWriter test fake, shared by the root
// tests that still exercise CodeCallIntentWriter through DefaultHandlers or
// the root compat spelling (defaults_test.go,
// defaults_semantic_entity_domain_wiring_test.go, fact_kind_loader_test.go,
// idempotency_cases_test.go, materialization_subduration_test.go,
// materialized_edge_family_blocker_shape_test.go,
// python_metaclass_materialization_test.go). Go test files cannot share
// unexported symbols across a package boundary (issue #6061).
type recordingCodeCallIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (r *recordingCodeCallIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	r.rows = append(r.rows, rows...)
	return nil
}
