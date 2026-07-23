// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// TestNonUIDRetractsRouteThroughAutocommitExecute proves the whole-scope
// (non-UID) retract methods on the writers listed in #5152 dispatch their DELETE
// statements through Execute (autocommit), never through ExecuteGroup, on a
// GroupExecutor-capable executor. The #4367 cloud-edge slice fixed only the
// *ByUIDs variants (see
// TestCloudResourceEdgeFamilyRetractByUIDsRoutesThroughAutocommitExecute); the
// whole-scope siblings still routed through the grouped dispatch, so on NornicDB
// v1.1.11 a DELETE inside the managed transaction under-applies and stale
// edges/nodes survive reprojection. Each subtest uses a GroupExecutor-capable
// recorder, so a regression back to ExecuteGroup fails here.
func TestNonUIDRetractsRouteThroughAutocommitExecute(t *testing.T) {
	t.Parallel()

	scopeIDs := []string{"scope-1"}
	const gen, src = "gen-1", "reducer/5152"

	t.Run("aws-cloud-resource-edges", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdges(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractCloudResourceEdges: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("azure-cloud-resource-edges", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewAzureCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdges(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractCloudResourceEdges: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("gcp-cloud-resource-edges", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewGCPCloudResourceEdgeWriter(rec, 0)
		if err := w.RetractCloudResourceEdges(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractCloudResourceEdges: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("code-taint-evidence", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewCodeTaintEvidenceWriter(rec, 0)
		if err := w.RetractCodeTaintEvidence(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractCodeTaintEvidence: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("stale-code-taint-evidence", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewCodeTaintEvidenceWriter(rec, 0)
		if err := w.RetractStaleCodeTaintEvidence(context.Background(), "scope-1", gen, src, 100); err != nil {
			t.Fatalf("RetractStaleCodeTaintEvidence: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("ec2-block-device-kms-posture-nodes", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewEC2BlockDeviceKMSPostureNodeWriter(rec, &echoingPostureExistenceReader{}, 0)
		if err := w.RetractEC2BlockDeviceKMSPostureNodes(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractEC2BlockDeviceKMSPostureNodes: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("ec2-internet-exposure-nodes", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewEC2InternetExposureNodeWriter(rec, &echoingPostureExistenceReader{}, 0)
		if err := w.RetractEC2InternetExposureNodes(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractEC2InternetExposureNodes: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	// Three writers with the same grouped-retract defect were found beyond the
	// issue's list during the completeness sweep for #5152; they are covered here.
	t.Run("incident-routing-evidence", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewIncidentRoutingEvidenceWriter(rec, 0)
		if err := w.RetractIncidentRoutingEvidence(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractIncidentRoutingEvidence: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("observability-coverage-edges", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewObservabilityCoverageEdgeWriter(rec, 0)
		if err := w.RetractObservabilityCoverageEdges(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractObservabilityCoverageEdges: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("rds-posture-nodes", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewRDSPostureNodeWriter(rec, &echoingPostureExistenceReader{}, 0)
		if err := w.RetractRDSPostureNodes(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractRDSPostureNodes: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})

	t.Run("s3-internet-exposure-nodes", func(t *testing.T) {
		t.Parallel()
		rec := &dispatchRouteRecorder{}
		w := NewS3InternetExposureNodeWriter(rec, &echoingPostureExistenceReader{}, 0)
		if err := w.RetractS3InternetExposureNodes(context.Background(), scopeIDs, gen, src); err != nil {
			t.Fatalf("RetractS3InternetExposureNodes: %v", err)
		}
		assertAutocommitRoute(t, rec)
	})
}
