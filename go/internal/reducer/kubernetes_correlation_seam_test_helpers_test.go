// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// This file holds the reducer root's own minimal copy of the fixtures the
// moved kubernetescorrelation package's own tests also define
// (go/internal/reducer/kubernetescorrelation/kubernetes_correlation_helpers_test.go
// and kubernetes_correlation_materialization_test.go), scoped to exactly what
// kubernetes_correlation_readiness_seam_test.go needs to drive the REAL
// kubernetescorrelation.KubernetesCorrelationMaterializationHandler from a
// cross-family contract test. Before issue #6061 moved the kubernetes_
// correlation family out of this package, one set of unexported test doubles
// served both sides. Go test files cannot share unexported symbols across
// packages, so the split needs its own copy on this side; keep it in sync
// with the family's copy by hand if either changes shape.

const (
	seamK8sRegistry   = "registry.example.com"
	seamK8sRepository = "team/checkout"
	seamK8sDigest     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

// seamExactDigestEdgeFixture is the canonical positive case: one live
// workload whose image digest matches an active OCI manifest source node,
// which must materialize exactly one RUNS_IMAGE edge. Mirrors the family's
// exactDigestEdgeFixture.
func seamExactDigestEdgeFixture() []facts.Envelope {
	imageRef := seamK8sRegistry + "/" + seamK8sRepository + "@" + seamK8sDigest
	objectID := "k8s://prod-eks/apps/v1/deployments/checkout/checkout"
	return []facts.Envelope{
		{
			FactID:   "seam-pod-1",
			FactKind: facts.KubernetesPodTemplateFactKind,
			Payload: map[string]any{
				"cluster_id":             "prod-eks",
				"object_id":              objectID,
				"group_version_resource": "apps/v1/deployments",
				"namespace":              "checkout",
				"name":                   "checkout",
				"uid":                    "uid-1",
				"service_account":        "default",
				"image_refs":             []string{imageRef},
				"containers": []any{
					map[string]any{"name": "checkout-c0", "image": imageRef, "init": false},
				},
				"correlation_anchors": []string{objectID, imageRef},
			},
		},
		{
			FactID:   "seam-oci-1",
			FactKind: facts.OCIImageManifestFactKind,
			Payload: map[string]any{
				"registry":      seamK8sRegistry,
				"repository":    seamK8sRepository,
				"repository_id": "oci-registry://" + seamK8sRegistry + "/" + seamK8sRepository,
				"digest":        seamK8sDigest,
				"descriptor_id": "oci-descriptor://" + seamK8sRegistry + "/" + seamK8sRepository + "@" + seamK8sDigest,
			},
		},
	}
}

// seamKubernetesCorrelationMaterializationIntent builds the RUNS_IMAGE edge
// intent the seam test drives, keyed on the SAME acceptance unit the
// production kubernetes_workload_materialization node slice publishes
// ("kubernetes_workload_materialization:<scope>"). Mirrors the family's
// kubernetesCorrelationMaterializationIntent.
func seamKubernetesCorrelationMaterializationIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-k8s-edge-seam",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainKubernetesCorrelationMaterialization,
		EntityKeys:   []string{"kubernetes_workload_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// stubSeamKubernetesCorrelationFactLoader implements the minimal
// factload.FactLoader plus ListActiveContainerImageIdentityFacts surface
// kubernetescorrelation.KubernetesCorrelationMaterializationHandler needs, all
// served from the one seamExactDigestEdgeFixture batch. Mirrors the family's
// stubKubernetesCorrelationFactLoader.
type stubSeamKubernetesCorrelationFactLoader struct {
	scopeFacts []facts.Envelope
}

func (s *stubSeamKubernetesCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSeamKubernetesCorrelationFactLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSeamKubernetesCorrelationFactLoader) ListActiveContainerImageIdentityFacts(
	context.Context,
) ([]facts.Envelope, error) {
	return nil, nil
}

// recordingSeamKubernetesCorrelationEdgeWriter captures RUNS_IMAGE edge writes
// so the seam test can assert the write count. Mirrors the family's
// recordingKubernetesCorrelationEdgeWriter.
type recordingSeamKubernetesCorrelationEdgeWriter struct {
	writeCalls int
}

func (w *recordingSeamKubernetesCorrelationEdgeWriter) WriteKubernetesCorrelationEdges(
	context.Context,
	[]map[string]any,
	string,
	string,
	string,
) error {
	w.writeCalls++
	return nil
}

func (w *recordingSeamKubernetesCorrelationEdgeWriter) RetractKubernetesCorrelationEdges(
	context.Context,
	[]string,
	string,
	string,
) error {
	return nil
}
