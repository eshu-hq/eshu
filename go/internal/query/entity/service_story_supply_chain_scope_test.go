// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/service"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

type serviceStoryExplainingImageIdentityStore struct {
	serviceStoryImageIdentityStore
	explanations map[string]map[string]any
}

func (s *serviceStoryExplainingImageIdentityStore) ExplainContainerImageCandidate(
	_ context.Context,
	imageRef string,
) (map[string]any, error) {
	return querycontract.CopyMap(s.explanations[imageRef]), nil
}

func TestServiceStorySupplyChainEvidenceExplainsRepoOnlyImageCandidate(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryExplainingImageIdentityStore{
		serviceStoryImageIdentityStore: serviceStoryImageIdentityStore{rowsByImageRef: map[string][]supplychain.ContainerImageIdentityRow{}},
	}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          &serviceStorySBOMAttachmentStore{},
	}
	ctx := map[string]any{
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				serviceStoryHelmValuesArtifact(serviceStoryTestImageRepository),
			},
		},
	}

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got := len(imageStore.filters); got != 0 {
		t.Fatalf("image identity store calls = %d, want no lookup for repo-only image candidate", got)
	}
	segment := service.ServiceTraceImagePackageSegment(ctx)
	if got, want := querycontract.StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if got, want := querycontract.IntVal(segment, "candidate_image_ref_count"), 1; got != want {
		t.Fatalf("candidate_image_ref_count = %d, want %d", got, want)
	}
	if refs := querycontract.StringSliceVal(segment, "candidate_image_refs"); len(refs) != 1 || refs[0] != serviceStoryTestImageRepository {
		t.Fatalf("candidate_image_refs = %#v, want %q", refs, serviceStoryTestImageRepository)
	}
	missing := querycontract.StringSliceVal(segment, "missing_evidence")
	if !slices.Contains(missing, "deployment_image_reference_repo_only") {
		t.Fatalf("missing_evidence = %#v, want deployment_image_reference_repo_only", missing)
	}
	details := querycontract.MapSliceValue(segment, "missing_evidence_details")
	if got, want := len(details), 1; got != want {
		t.Fatalf("missing_evidence_details count = %d, want %d; segment=%#v", got, want, segment)
	}
	detail := details[0]
	if got, want := querycontract.StringVal(detail, "candidate_image_ref"), serviceStoryTestImageRepository; got != want {
		t.Fatalf("candidate_image_ref = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(detail, "reason"), "deployment_image_reference_repo_only"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	if action := querycontract.StringVal(detail, "operator_action"); !strings.Contains(action, "tag or digest") {
		t.Fatalf("operator_action = %q, want tag or digest guidance", action)
	}
}

func TestServiceStorySupplyChainEvidenceExplainsOCIRegistryTargetOutsideScope(t *testing.T) {
	t.Parallel()

	imageRef := "444455556666.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"
	repositoryID := "oci-registry://444455556666.dkr.ecr.us-east-1.amazonaws.com/team/api"
	imageStore := &serviceStoryExplainingImageIdentityStore{
		serviceStoryImageIdentityStore: serviceStoryImageIdentityStore{rowsByImageRef: map[string][]supplychain.ContainerImageIdentityRow{}},
		explanations: map[string]map[string]any{
			imageRef: {
				"candidate_image_ref":     imageRef,
				"candidate_repository_id": repositoryID,
				"collector_scope":         "outside_configured_targets",
				"reason":                  "oci_registry_target_outside_scope",
				"operator_action":         "add an OCI registry collector target for " + repositoryID,
			},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
	}
	ctx := serviceStoryDeploymentImageContext(imageRef)

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got, want := len(imageStore.filters), 1; got != want {
		t.Fatalf("image identity store calls = %d, want %d", got, want)
	}
	if got, want := imageStore.filters[0].ImageRef, imageRef; got != want {
		t.Fatalf("image identity ImageRef filter = %q, want %q", got, want)
	}
	if got := len(sbomStore.filters); got != 0 {
		t.Fatalf("SBOM attachment store calls = %d, want none without image identity", got)
	}
	segment := service.ServiceTraceImagePackageSegment(ctx)
	if got, want := querycontract.StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if got := querycontract.IntVal(segment, "evidence_count"); got != 0 {
		t.Fatalf("evidence_count = %d, want 0 for out-of-scope OCI target", got)
	}
	missing := querycontract.StringSliceVal(segment, "missing_evidence")
	if !slices.Contains(missing, "oci_registry_target_outside_scope") {
		t.Fatalf("missing_evidence = %#v, want oci_registry_target_outside_scope", missing)
	}
	if slices.Contains(missing, "container_image_identity_missing") {
		t.Fatalf("missing_evidence = %#v, want specific OCI scope reason instead of generic identity missing", missing)
	}
	details := querycontract.MapSliceValue(segment, "missing_evidence_details")
	if got, want := len(details), 1; got != want {
		t.Fatalf("missing_evidence_details count = %d, want %d; segment=%#v", got, want, segment)
	}
	detail := details[0]
	if got, want := querycontract.StringVal(detail, "candidate_repository_id"), repositoryID; got != want {
		t.Fatalf("candidate_repository_id = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(detail, "collector_scope"), "outside_configured_targets"; got != want {
		t.Fatalf("collector_scope = %q, want %q", got, want)
	}
	if action := querycontract.StringVal(detail, "operator_action"); !strings.Contains(action, repositoryID) {
		t.Fatalf("operator_action = %q, want repository-specific target guidance", action)
	}
}

func TestServiceStoryContainerImageCandidateReasonHandlesPendingAndCompletedRetryState(t *testing.T) {
	t.Parallel()

	repositoryID := "oci-registry://registry.example.com/team/api"
	reason, collectorScope, _ := service.ServiceStoryContainerImageCandidateReason(
		repositoryID,
		service.ServiceStoryContainerImageCandidateState{ScopeID: repositoryID, ScopeStatus: "pending"},
	)
	if reason != "oci_registry_target_collection_pending" || collectorScope != "configured_pending" {
		t.Fatalf("pending scope reason = (%q, %q), want configured pending", reason, collectorScope)
	}

	reason, collectorScope, _ = service.ServiceStoryContainerImageCandidateReason(
		repositoryID,
		service.ServiceStoryContainerImageCandidateState{ScopeID: repositoryID},
	)
	if reason != "oci_registry_target_collection_pending" || collectorScope != "configured_pending" {
		t.Fatalf("configured scope without generation reason = (%q, %q), want configured pending", reason, collectorScope)
	}

	reason, collectorScope, _ = service.ServiceStoryContainerImageCandidateReason(
		repositoryID,
		service.ServiceStoryContainerImageCandidateState{
			ScopeID:          repositoryID,
			GenerationID:     "gen-failed",
			GenerationStatus: "failed",
		},
	)
	if reason != "oci_registry_target_unreadable" || collectorScope != "configured_unreadable" {
		t.Fatalf("failed generation reason = (%q, %q), want configured unreadable", reason, collectorScope)
	}

	reason, collectorScope, _ = service.ServiceStoryContainerImageCandidateReason(
		repositoryID,
		service.ServiceStoryContainerImageCandidateState{
			ScopeID:      repositoryID,
			WorkStatus:   "completed",
			FailureClass: "registry_auth_denied",
		},
	)
	if reason != "container_image_identity_scanned_missing" || collectorScope != "configured_scanned" {
		t.Fatalf("completed retry reason = (%q, %q), want scanned missing", reason, collectorScope)
	}
}
