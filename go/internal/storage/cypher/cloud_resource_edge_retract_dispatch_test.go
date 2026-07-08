// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// TestCloudResourceEdgeFamilyRetractByUIDsRoutesThroughAutocommitExecute
// proves the anchored-retract methods on all four CloudResource-edge-family
// writers (AWS, Azure, GCP relationships, and observability coverage) dispatch
// their DELETE statements through Execute (autocommit), never through
// ExecuteGroup, on a GroupExecutor-capable executor. This mirrors
// TestCodeInterprocEvidenceRetractByUIDsRoutesThroughAutocommitExecute: the
// same NornicDB v1.1.9 bolt-driver bug (UNWIND … MATCH … -[rel]-> … DELETE rel
// inside session.ExecuteWrite / tx.Run silently deletes 0 rows) applies to
// every writer using the ledger-anchored retract shape, not just code-interproc.
func TestCloudResourceEdgeFamilyRetractByUIDsRoutesThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	t.Run("aws", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdgesByUIDs(context.Background(), []string{"src-1"}, []string{"scope-1"}, "reducer/aws-relationships"); err != nil {
			t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("azure", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewAzureCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdgesByUIDs(context.Background(), []string{"src-1"}, []string{"scope-1"}, "reducer/azure-relationships"); err != nil {
			t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("gcp", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewGCPCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdgesByUIDs(context.Background(), []string{"src-1"}, []string{"scope-1"}, "reducer/gcp-relationships"); err != nil {
			t.Fatalf("RetractCloudResourceEdgesByUIDs returned error: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("observability", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewObservabilityCoverageEdgeWriter(rec, 0)
		if err := w.RetractObservabilityCoverageEdgesByUIDs(context.Background(), []string{"obs-1"}, []string{"scope-1"}, "reducer/observability-coverage"); err != nil {
			t.Fatalf("RetractObservabilityCoverageEdgesByUIDs returned error: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})
}

// assertAutocommitRoute asserts exactly one Execute call and zero ExecuteGroup
// calls were recorded, i.e. the retract used the autocommit route.
func assertAutocommitRoute(t *testing.T, rec *dispatchRouteRecorder) {
	t.Helper()
	if len(rec.executeCyphers) != 1 {
		t.Fatalf("Execute calls = %d, want 1 (autocommit route)", len(rec.executeCyphers))
	}
	if len(rec.groupCyphers) != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0; DELETE must not use ExecuteGroup (NornicDB v1.1.9 tx.Run deletes 0 rows) — see #4893", len(rec.groupCyphers))
	}
}
