// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestCov8SupplyChainImpactAggregateScope pins the scope map: every
// set filter field renders, profile always renders, and zero values
// stay absent.
func TestCov8SupplyChainImpactAggregateScope(t *testing.T) {
	t.Parallel()

	full := impact.AggregateFilter{
		CVEID: "CVE-2026-1", AdvisoryID: "GHSA-1", PackageID: "pkg:npm/a@1",
		RepositoryID: "repo-1", SubjectDigest: "sha256:x", ImageRef: "img:1",
		Status: "open", Ecosystem: "npm", WorkloadID: "w1", ServiceID: "s1",
		Environment: "prod", Severity: "critical", DetectionProfile: impact.ProfilePrecise,
		PriorityBucket: "p1", MinPriorityScore: 70,
		SuppressionState: "suppressed", IncludeSuppressed: true,
	}
	scope := supplyChainImpactAggregateScope(full)
	want := map[string]string{
		"cve_id": "CVE-2026-1", "advisory_id": "GHSA-1", "package_id": "pkg:npm/a@1",
		"repository_id": "repo-1", "subject_digest": "sha256:x", "image_ref": "img:1",
		"impact_status": "open", "ecosystem": "npm", "workload_id": "w1",
		"service_id": "s1", "environment": "prod", "severity": "critical",
		"profile": "precise", "priority_bucket": "p1", "min_priority_score": "70",
		"suppression_state": "suppressed", "include_suppressed": "true",
	}
	if len(scope) != len(want) {
		t.Fatalf("scope = %v, want %v", scope, want)
	}
	for key, value := range want {
		if scope[key] != value {
			t.Errorf("scope[%q] = %q, want %q", key, scope[key], value)
		}
	}

	bare := supplyChainImpactAggregateScope(impact.AggregateFilter{})
	if len(bare) != 1 || bare["profile"] != impact.ProfileComprehensive {
		t.Errorf("bare scope = %v, want only the comprehensive profile", bare)
	}
	if got := requestedSupplyChainImpactAggregateProfile(
		impact.AggregateFilter{DetectionProfile: "other"}); got != impact.ProfileComprehensive {
		t.Errorf("unknown profile = %q, want comprehensive", got)
	}
}

// TestCov8SupplyChainImpactDimensions pins the closed inventory
// dimension set.
func TestCov8SupplyChainImpactDimensions(t *testing.T) {
	t.Parallel()

	for _, dimension := range []impact.InventoryDimension{
		impact.InventoryByStatus,
		impact.InventoryByPriorityBucket,
		impact.InventoryBySeverity,
		impact.InventoryByRepository,
		impact.InventoryByEcosystem,
	} {
		if !isSupportedSupplyChainImpactDimension(dimension) {
			t.Errorf("dimension %q unsupported, want supported", dimension)
		}
	}
	if isSupportedSupplyChainImpactDimension("workload") {
		t.Error("dimension workload supported, want rejected")
	}
}

// TestCov8SupplyChainImpactAggregatePaging pins limit/offset parsing:
// defaults, clamps, and the 400 arms.
func TestCov8SupplyChainImpactAggregatePaging(t *testing.T) {
	t.Parallel()

	limit, ok := parseSupplyChainImpactAggregateLimit(
		httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !ok || limit != supplyChainImpactAggregateDefaultLimit {
		t.Errorf("default limit = %d, %v; want %d, true", limit, ok, supplyChainImpactAggregateDefaultLimit)
	}
	limit, ok = parseSupplyChainImpactAggregateLimit(
		httptest.NewRecorder(), httptest.NewRequest("GET", "/x?limit=25", nil))
	if !ok || limit != 25 {
		t.Errorf("limit=25 = %d, %v; want 25, true", limit, ok)
	}
	for name, target := range map[string]string{
		"non-numeric": "/x?limit=abc",
		"zero":        "/x?limit=0",
		"over max":    "/x?limit=999999999",
	} {
		recorder := httptest.NewRecorder()
		if _, ok := parseSupplyChainImpactAggregateLimit(
			recorder, httptest.NewRequest("GET", target, nil)); ok {
			t.Errorf("limit(%s) ok, want 400 rejection", name)
		}
		if recorder.Code != 400 {
			t.Errorf("limit(%s) status = %d, want 400", name, recorder.Code)
		}
	}

	offset, ok := parseSupplyChainImpactAggregateOffset(
		httptest.NewRecorder(), httptest.NewRequest("GET", "/x?offset=10", nil))
	if !ok || offset != 10 {
		t.Errorf("offset=10 = %d, %v; want 10, true", offset, ok)
	}
	for name, target := range map[string]string{
		"negative": "/x?offset=-1",
		"over max": "/x?offset=999999999",
	} {
		recorder := httptest.NewRecorder()
		if _, ok := parseSupplyChainImpactAggregateOffset(
			recorder, httptest.NewRequest("GET", target, nil)); ok {
			t.Errorf("offset(%s) ok, want 400 rejection", name)
		}
		if recorder.Code != 400 {
			t.Errorf("offset(%s) status = %d, want 400", name, recorder.Code)
		}
	}
	if got := NextImpactAggregateOffset(0, 100, true); got != 100 {
		t.Errorf("next offset = %v, want 100", got)
	}
	if got := NextImpactAggregateOffset(0, 100, false); got != nil {
		t.Errorf("next offset untruncated = %v, want nil", got)
	}
}

// TestCov8ContainerImageIdentityAggregates pins the image aggregate
// surface: request parsing, scope rendering, the missing-evidence
// bridge, and the outcome 400 guard.
func TestCov8ContainerImageIdentityAggregates(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		"GET", "/x?digest=sha256:x&image_ref=img:1&repository_id=repo-1&outcome=exact_digest", nil)
	filter := containerImageIdentityAggregateFilterFromRequest(request, "src-1")
	if filter.Digest != "sha256:x" || filter.ImageRef != "img:1" ||
		filter.RepositoryID != "repo-1" || filter.Outcome != "exact_digest" ||
		filter.SourceRepositoryID != "src-1" {
		t.Errorf("filter from request = %+v, want parsed fields", filter)
	}
	scope := containerImageIdentityAggregateScope(filter)
	if len(scope) != 5 || scope["digest"] != "sha256:x" || scope["source_repository_id"] != "src-1" {
		t.Errorf("scope = %v, want all five fields", scope)
	}
	if scope := containerImageIdentityAggregateScope(ContainerImageIdentityAggregateFilter{}); len(scope) != 0 {
		t.Errorf("bare scope = %v, want empty", scope)
	}

	bridge := containerImageIdentityAggregateSourceBridge("src-1", ContainerImageIdentityAggregateCount{})
	if bridge.SourceRepositoryID != "src-1" || len(bridge.MissingEvidence) != 3 {
		t.Errorf("empty bridge = %+v, want three missing-evidence markers", bridge)
	}
	full := containerImageIdentityAggregateSourceBridge("src-1",
		ContainerImageIdentityAggregateCount{TotalIdentities: 2})
	if len(full.MissingEvidence) != 0 {
		t.Errorf("non-empty bridge = %+v, want no missing evidence", full)
	}

	recorder := httptest.NewRecorder()
	if !validateContainerImageIdentityAggregateOutcome(recorder, filter) {
		t.Error("valid outcome rejected, want accepted")
	}
	bad := httptest.NewRecorder()
	if validateContainerImageIdentityAggregateOutcome(bad,
		ContainerImageIdentityAggregateFilter{Outcome: "exact-digest"}) {
		t.Error("typo outcome accepted, want 400 rejection")
	}
	if bad.Code != 400 {
		t.Errorf("typo outcome status = %d, want 400", bad.Code)
	}
	for _, outcome := range []string{"exact_digest", "tag_resolved"} {
		if !isSupportedContainerImageIdentityOutcome(outcome) {
			t.Errorf("outcome %q unsupported, want supported", outcome)
		}
	}
}

// TestCov8SBOMAttachmentAggregates pins the SBOM aggregate surface:
// scope rendering, the closed dimension set, the status/kind 400
// guards, and offset continuation.
func TestCov8SBOMAttachmentAggregates(t *testing.T) {
	t.Parallel()

	filter := SBOMAttestationAttachmentAggregateFilter{
		SubjectDigest: "sha256:x", DocumentID: "doc-1", DocumentDigest: "sha256:d",
		RepositoryID: "repo-1", WorkloadID: "w1", ServiceID: "s1",
		AttachmentStatus: "attached_verified", ArtifactKind: "sbom",
	}
	scope := SBOMAttestationAttachmentAggregateScope(filter)
	if len(scope) != 8 || scope["attachment_status"] != "attached_verified" ||
		scope["artifact_kind"] != "sbom" {
		t.Errorf("scope = %v, want all eight fields", scope)
	}
	if scope := SBOMAttestationAttachmentAggregateScope(
		SBOMAttestationAttachmentAggregateFilter{}); len(scope) != 0 {
		t.Errorf("bare scope = %v, want empty", scope)
	}

	for _, dimension := range []SBOMAttestationAttachmentInventoryDimension{
		SBOMAttestationAttachmentInventoryByAttachmentStatus,
		SBOMAttestationAttachmentInventoryByArtifactKind,
		SBOMAttestationAttachmentInventoryBySubjectDigest,
	} {
		if !isSupportedSBOMAttestationAttachmentDimension(dimension) {
			t.Errorf("dimension %q unsupported, want supported", dimension)
		}
	}
	if isSupportedSBOMAttestationAttachmentDimension("workload") {
		t.Error("dimension workload supported, want rejected")
	}

	recorder := httptest.NewRecorder()
	if !validateSBOMAttestationAttachmentAggregateFilters(recorder, filter) {
		t.Error("valid filters rejected, want accepted")
	}
	badStatus := httptest.NewRecorder()
	if validateSBOMAttestationAttachmentAggregateFilters(badStatus,
		SBOMAttestationAttachmentAggregateFilter{AttachmentStatus: "attached"}) {
		t.Error("typo status accepted, want 400 rejection")
	}
	if badStatus.Code != 400 {
		t.Errorf("typo status code = %d, want 400", badStatus.Code)
	}
	badKind := httptest.NewRecorder()
	if validateSBOMAttestationAttachmentAggregateFilters(badKind,
		SBOMAttestationAttachmentAggregateFilter{ArtifactKind: "cyclonedx"}) {
		t.Error("typo kind accepted, want 400 rejection")
	}
	if badKind.Code != 400 {
		t.Errorf("typo kind code = %d, want 400", badKind.Code)
	}

	if got := NextSBOMAttestationAttachmentAggregateOffset(0, 100, true); got != 100 {
		t.Errorf("next offset = %v, want 100", got)
	}
	if got := NextSBOMAttestationAttachmentAggregateOffset(0, 100, false); got != nil {
		t.Errorf("next offset untruncated = %v, want nil", got)
	}
	if got := NextSBOMAttestationAttachmentAggregateOffset(
		sbomAttestationAttachmentAggregateMaxOffset, 100, true); got != nil {
		t.Errorf("next offset past max = %v, want nil", got)
	}
}
