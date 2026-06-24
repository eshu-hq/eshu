// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	serviceStoryTestImageRepository = "registry.example.com/team/api"
	serviceStoryTestImageRef        = serviceStoryTestImageRepository + ":prod"
	serviceStoryTestDigest          = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type serviceStoryImageIdentityStore struct {
	rowsByImageRef map[string][]ContainerImageIdentityRow
	filters        []ContainerImageIdentityFilter
}

func (s *serviceStoryImageIdentityStore) ListContainerImageIdentities(
	_ context.Context,
	filter ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	s.filters = append(s.filters, filter)
	return append([]ContainerImageIdentityRow(nil), s.rowsByImageRef[filter.ImageRef]...), nil
}

type serviceStorySBOMAttachmentStore struct {
	rowsBySubjectDigest map[string][]SBOMAttestationAttachmentRow
	filters             []SBOMAttestationAttachmentFilter
}

func (s *serviceStorySBOMAttachmentStore) ListSBOMAttestationAttachments(
	_ context.Context,
	filter SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	s.filters = append(s.filters, filter)
	return SBOMAttestationAttachmentPage{
		Attachments: append([]SBOMAttestationAttachmentRow(nil), s.rowsBySubjectDigest[filter.SubjectDigest]...),
	}, nil
}

func TestServiceStorySupplyChainEvidenceAttachesExactImageAndSBOM(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryImageIdentityStore{
		rowsByImageRef: map[string][]ContainerImageIdentityRow{
			serviceStoryTestImageRef: {serviceStoryExactImageIdentity(serviceStoryTestDigest)},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{
		rowsBySubjectDigest: map[string][]SBOMAttestationAttachmentRow{
			serviceStoryTestDigest: {serviceStoryAttachedSBOM(serviceStoryTestDigest)},
		},
	}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
	}
	ctx := serviceStoryDeploymentImageContext(serviceStoryTestImageRef)

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got, want := len(imageStore.filters), 1; got != want {
		t.Fatalf("image identity store calls = %d, want %d", got, want)
	}
	if got, want := imageStore.filters[0].ImageRef, serviceStoryTestImageRef; got != want {
		t.Fatalf("image identity ImageRef filter = %q, want %q", got, want)
	}
	if got, want := imageStore.filters[0].Limit, serviceStorySupplyChainReadLimit+1; got != want {
		t.Fatalf("image identity Limit = %d, want probe limit %d", got, want)
	}
	if got, want := len(sbomStore.filters), 1; got != want {
		t.Fatalf("SBOM attachment store calls = %d, want %d", got, want)
	}
	if got, want := sbomStore.filters[0].SubjectDigest, serviceStoryTestDigest; got != want {
		t.Fatalf("SBOM SubjectDigest filter = %q, want %q", got, want)
	}
	if got, want := sbomStore.filters[0].Limit, serviceStorySupplyChainReadLimit+1; got != want {
		t.Fatalf("SBOM attachment Limit = %d, want probe limit %d", got, want)
	}

	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := StringVal(segment, "status"), "exact"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if got, want := StringVal(segment, "basis"), "container_image_identity_and_sbom_attachment"; got != want {
		t.Fatalf("image_package basis = %q, want %q", got, want)
	}
	evidence := mapSliceValue(segment, "evidence")
	if got, want := len(evidence), 1; got != want {
		t.Fatalf("image_package evidence count = %d, want %d; segment=%#v", got, want, segment)
	}
	row := evidence[0]
	if got, want := StringVal(row, "image_ref"), serviceStoryTestImageRef; got != want {
		t.Fatalf("evidence image_ref = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "digest"), serviceStoryTestDigest; got != want {
		t.Fatalf("evidence digest = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "sbom_attachment_status"), "attached_verified"; got != want {
		t.Fatalf("evidence sbom_attachment_status = %q, want %q", got, want)
	}
	if missing := StringSliceVal(segment, "missing_evidence"); len(missing) != 0 {
		t.Fatalf("missing_evidence = %#v, want none", missing)
	}
	if _, ok := segment["missing_evidence_details"]; ok {
		t.Fatalf("missing_evidence_details present for exact image package segment: %#v", segment["missing_evidence_details"])
	}
}

func TestGetServiceStoryEnvelopeIncludesSupplyChainEvidence(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryImageIdentityStore{
		rowsByImageRef: map[string][]ContainerImageIdentityRow{
			serviceStoryTestImageRef: {serviceStoryExactImageIdentity(serviceStoryTestDigest)},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{
		rowsBySubjectDigest: map[string][]SBOMAttestationAttachmentRow{
			serviceStoryTestDigest: {serviceStoryAttachedSBOM(serviceStoryTestDigest)},
		},
	}
	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.name = $service_name"):
					return []map[string]any{{"id": "workload:api", "name": "api", "kind": "service", "repo_id": "repo://example/api"}}, nil
				case strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") &&
					strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository"):
					return []map[string]any{serviceStoryDeploymentImageArtifact(serviceStoryTestImageRef)}, nil
				default:
					_ = params
					return nil, nil
				}
			},
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {"id": "workload:api", "name": "api", "kind": "service", "repo_id": "repo://example/api"},
			},
		},
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
		Profile:                  ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", envelope.Data)
	}
	trace := mapValue(data, "code_to_runtime_trace")
	segment := segmentByName(mapSliceValue(trace, "segments"), "image_package")
	if got, want := StringVal(segment, "status"), "exact"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	evidence := mapSliceValue(segment, "evidence")
	if got, want := len(evidence), 1; got != want {
		t.Fatalf("image_package evidence count = %d, want %d; segment=%#v", got, want, segment)
	}
	if got, want := StringVal(evidence[0], "sbom_attachment_id"), "sbom-attachment-1"; got != want {
		t.Fatalf("sbom_attachment_id = %q, want %q", got, want)
	}
}

func TestServiceStorySupplyChainEvidenceFailsClosedForAmbiguousTags(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryImageIdentityStore{
		rowsByImageRef: map[string][]ContainerImageIdentityRow{
			serviceStoryTestImageRef: {
				serviceStoryExactImageIdentity("sha256:1111111111111111111111111111111111111111111111111111111111111111"),
				serviceStoryExactImageIdentity("sha256:2222222222222222222222222222222222222222222222222222222222222222"),
			},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
	}
	ctx := serviceStoryDeploymentImageContext(serviceStoryTestImageRef)

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got := len(sbomStore.filters); got != 0 {
		t.Fatalf("SBOM attachment store calls = %d, want none for ambiguous image tag", got)
	}
	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if missing := StringSliceVal(segment, "missing_evidence"); !stringSliceContains(missing, "container_image_identity_ambiguous") {
		t.Fatalf("missing_evidence = %#v, want container_image_identity_ambiguous", missing)
	}
	if got := IntVal(segment, "evidence_count"); got != 0 {
		t.Fatalf("evidence_count = %d, want 0 for ambiguous image tag", got)
	}
}

func TestServiceStorySupplyChainEvidenceFailsClosedForStaleIdentity(t *testing.T) {
	t.Parallel()

	staleIdentity := serviceStoryExactImageIdentity(serviceStoryTestDigest)
	staleIdentity.SourceFreshness = "stale"
	imageStore := &serviceStoryImageIdentityStore{
		rowsByImageRef: map[string][]ContainerImageIdentityRow{
			serviceStoryTestImageRef: {staleIdentity},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
	}
	ctx := serviceStoryDeploymentImageContext(serviceStoryTestImageRef)

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got := len(sbomStore.filters); got != 0 {
		t.Fatalf("SBOM attachment store calls = %d, want none for stale image identity", got)
	}
	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if missing := StringSliceVal(segment, "missing_evidence"); !stringSliceContains(missing, "container_image_identity_stale") {
		t.Fatalf("missing_evidence = %#v, want container_image_identity_stale", missing)
	}
}

func TestServiceStorySupplyChainEvidenceFailsClosedForUnattachedSBOM(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryImageIdentityStore{
		rowsByImageRef: map[string][]ContainerImageIdentityRow{
			serviceStoryTestImageRef: {serviceStoryExactImageIdentity(serviceStoryTestDigest)},
		},
	}
	sbomStore := &serviceStorySBOMAttachmentStore{
		rowsBySubjectDigest: map[string][]SBOMAttestationAttachmentRow{
			serviceStoryTestDigest: {{
				AttachmentID:     "sbom-attachment-mismatch",
				SubjectDigest:    serviceStoryTestDigest,
				DocumentID:       "sbom-doc-1",
				AttachmentStatus: "subject_mismatch",
				Reason:           "subject digest did not match the resolved image",
				CanonicalWrites:  0,
				SourceFreshness:  "active",
			}},
		},
	}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          sbomStore,
	}
	ctx := serviceStoryDeploymentImageContext(serviceStoryTestImageRef)

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	if missing := StringSliceVal(segment, "missing_evidence"); !stringSliceContains(missing, "sbom_attachment_not_admissible") {
		t.Fatalf("missing_evidence = %#v, want sbom_attachment_not_admissible", missing)
	}
	if got := IntVal(segment, "evidence_count"); got != 0 {
		t.Fatalf("evidence_count = %d, want 0 for unattached SBOM", got)
	}
}

func TestServiceStoryDeploymentImageRefsIgnoreDockerComposeBuildContext(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{
					"path":            "compose.yaml",
					"artifact_family": "docker_compose",
					"evidence_kind":   "DOCKER_COMPOSE_BUILD_CONTEXT",
					"matched_value":   "../api",
				},
			},
		},
	}

	if refs := serviceStoryDeploymentImageRefs(ctx); len(refs) != 0 {
		t.Fatalf("serviceStoryDeploymentImageRefs() = %#v, want no build-context image refs", refs)
	}
}

func TestServiceStoryDeploymentImageRefsPromotesHelmValuesImageMatchedValue(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				serviceStoryHelmValuesArtifact(serviceStoryTestImageRepository),
				serviceStoryHelmValuesArtifact("charts/api/values-qa.yaml"),
			},
		},
	}

	refs := serviceStoryDeploymentImageRefs(ctx)
	if got, want := len(refs), 1; got != want {
		t.Fatalf("serviceStoryDeploymentImageRefs() = %#v, want one image ref", refs)
	}
	if got, want := refs[0], serviceStoryTestImageRepository; got != want {
		t.Fatalf("serviceStoryDeploymentImageRefs()[0] = %q, want %q", got, want)
	}
}

func TestServiceStorySupplyChainEvidenceReportsRepoOnlyHelmValuesImageRef(t *testing.T) {
	t.Parallel()

	imageStore := &serviceStoryImageIdentityStore{rowsByImageRef: map[string][]ContainerImageIdentityRow{}}
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
		t.Fatalf("image identity store calls = %d, want none for repo-only candidate", got)
	}
	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := StringVal(segment, "status"), "missing_evidence"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	missing := StringSliceVal(segment, "missing_evidence")
	if stringSliceContains(missing, "deployment_image_reference_missing") {
		t.Fatalf("missing_evidence = %#v, want repo-only candidate reason", missing)
	}
	if !stringSliceContains(missing, "deployment_image_reference_repo_only") {
		t.Fatalf("missing_evidence = %#v, want deployment_image_reference_repo_only", missing)
	}
	if got, want := IntVal(segment, "candidate_image_ref_count"), 1; got != want {
		t.Fatalf("candidate_image_ref_count = %d, want %d", got, want)
	}
}

func serviceStoryHelmValuesArtifact(value string) map[string]any {
	return map[string]any{"artifact_family": "helm", "evidence_kind": "HELM_VALUES_REFERENCE", "matched_value": value}
}

func TestServiceStorySupplyChainEvidenceBoundsImageRefLookups(t *testing.T) {
	t.Parallel()

	refs := make([]string, 0, serviceStoryItemLimit+2)
	for i := range serviceStoryItemLimit + 2 {
		refs = append(refs, fmt.Sprintf("registry.example.com/team/api:%03d", i))
	}
	imageStore := &serviceStoryImageIdentityStore{rowsByImageRef: map[string][]ContainerImageIdentityRow{}}
	handler := &EntityHandler{
		ContainerImageIdentities: imageStore,
		SBOMAttachments:          &serviceStorySBOMAttachmentStore{},
	}
	ctx := map[string]any{"image_refs": refs}

	if err := handler.enrichServiceStorySupplyChainEvidence(context.Background(), ctx); err != nil {
		t.Fatalf("enrichServiceStorySupplyChainEvidence() error = %v, want nil", err)
	}
	if got, want := len(imageStore.filters), serviceStoryItemLimit; got != want {
		t.Fatalf("image identity store calls = %d, want capped %d", got, want)
	}
	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := IntVal(segment, "candidate_image_ref_count"), len(refs); got != want {
		t.Fatalf("candidate_image_ref_count = %d, want %d", got, want)
	}
	if !BoolVal(segment, "image_refs_truncated") {
		t.Fatalf("image_refs_truncated = false, want true; segment=%#v", segment)
	}
}

func serviceStoryDeploymentImageContext(imageRef string) map[string]any {
	return map[string]any{
		"id":        "workload:api",
		"name":      "api",
		"repo_id":   "repo://example/api",
		"repo_name": "api",
		"deployment_evidence": map[string]any{
			"artifacts": []map[string]any{
				{
					"path":            "compose.yaml",
					"artifact_family": "docker_compose",
					"evidence_kind":   "DOCKER_COMPOSE_IMAGE",
					"matched_value":   imageRef,
				},
			},
		},
	}
}

func serviceStoryDeploymentImageArtifact(imageRef string) map[string]any {
	return map[string]any{
		"direction":         "incoming",
		"artifact_id":       "evidence-artifact:compose-image",
		"name":              "compose.yaml",
		"domain":            "deployment",
		"path":              "compose.yaml",
		"evidence_kind":     "DOCKER_COMPOSE_IMAGE",
		"artifact_family":   "docker_compose",
		"extractor":         "docker_compose",
		"relationship_type": "DEPLOYS_FROM",
		"resolved_id":       "resolved-compose-image",
		"generation_id":     "generation-active",
		"confidence":        0.88,
		"matched_alias":     "api",
		"matched_value":     imageRef,
		"evidence_source":   "resolver/cross-repo",
		"source_repo_id":    "repo://example/deploy",
		"source_repo_name":  "deploy",
		"target_repo_id":    "repo://example/api",
		"target_repo_name":  "api",
	}
}

func serviceStoryExactImageIdentity(digest string) ContainerImageIdentityRow {
	return ContainerImageIdentityRow{
		IdentityID:       "image-identity-" + digest,
		Digest:           digest,
		ImageRef:         serviceStoryTestImageRef,
		RepositoryID:     "oci-registry://registry.example.com/team/api",
		Outcome:          "tag_resolved",
		IdentityStrength: "tag_observation_with_digest",
		CanonicalWrites:  1,
		CanonicalID:      "canonical:container_image_identity:scope:generation:image",
		SourceLayers:     []string{"source_declaration", "observed_resource"},
		EvidenceFactIDs:  []string{"compose-image-ref", "oci-tag-observation"},
		SourceFreshness:  "active",
		SourceConfidence: "inferred",
	}
}

func serviceStoryAttachedSBOM(subjectDigest string) SBOMAttestationAttachmentRow {
	return SBOMAttestationAttachmentRow{
		AttachmentID:       "sbom-attachment-1",
		SubjectDigest:      subjectDigest,
		DocumentID:         "sbom-doc-1",
		DocumentDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		AttachmentStatus:   "attached_verified",
		ParseStatus:        "parsed",
		VerificationStatus: "passed",
		ArtifactKind:       "sbom",
		Format:             "spdx",
		SpecVersion:        "2.3",
		CanonicalWrites:    1,
		ComponentCount:     7,
		EvidenceFactIDs:    []string{"sbom-referrer", "sbom-document"},
		SourceFreshness:    "active",
		SourceConfidence:   "inferred",
	}
}
