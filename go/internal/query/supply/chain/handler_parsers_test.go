// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

func advisoryCatalogFilterForTest() advisory.CatalogFilter { return advisory.CatalogFilter{} }

func advisoryEvidenceFilterForTest() advisory.EvidenceFilter { return advisory.EvidenceFilter{} }

func impactAggregateFilterForTest() impact.AggregateFilter { return impact.AggregateFilter{} }

func containerImageIdentityAggregateFilterForTest() ContainerImageIdentityAggregateFilter {
	return ContainerImageIdentityAggregateFilter{}
}

func sbomAttachmentAggregateFilterForTest() SBOMAttestationAttachmentAggregateFilter {
	return SBOMAttestationAttachmentAggregateFilter{}
}

func securityAlertReconciliationAggregateFilterForTest() SecurityAlertReconciliationAggregateFilter {
	return SecurityAlertReconciliationAggregateFilter{}
}

func authContextForTest(subjectClass, subjectIDHash string) auth.AuthContext {
	return auth.AuthContext{SubjectClass: subjectClass, SubjectIDHash: subjectIDHash}
}

func requestWithQuery(target string) (*httptest.ResponseRecorder, *http.Request) {
	return httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil)
}

func TestAdvisoryCatalogParsers(t *testing.T) {
	t.Parallel()

	t.Run("RequiredLimit", func(t *testing.T) {
		t.Parallel()
		recorder, req := requestWithQuery("/advisories")
		if _, ok := requiredAdvisoryCatalogLimit(recorder, req); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("missing limit = ok(%v) status %d, want rejection", ok, recorder.Code)
		}
		recorder, req = requestWithQuery("/advisories?limit=abc")
		if _, ok := requiredAdvisoryCatalogLimit(recorder, req); ok {
			t.Fatal("non-numeric limit accepted, want rejection")
		}
		recorder, req = requestWithQuery("/advisories?limit=1000000")
		if _, ok := requiredAdvisoryCatalogLimit(recorder, req); ok {
			t.Fatal("over-max limit accepted, want rejection")
		}
		recorder, req = requestWithQuery("/advisories?limit=25")
		if limit, ok := requiredAdvisoryCatalogLimit(recorder, req); !ok || limit != 25 {
			t.Fatalf("limit = (%d, %v), want (25, true)", limit, ok)
		}
	})
	t.Run("KEVOnly", func(t *testing.T) {
		t.Parallel()
		_, req := requestWithQuery("/advisories")
		if kev, ok := parseAdvisoryCatalogKEVOnly(httptest.NewRecorder(), req); !ok || kev {
			t.Fatalf("default = (%v, %v), want (false, true)", kev, ok)
		}
		_, req = requestWithQuery("/advisories?kev=true")
		if kev, ok := parseAdvisoryCatalogKEVOnly(httptest.NewRecorder(), req); !ok || !kev {
			t.Fatalf("kev=true = (%v, %v)", kev, ok)
		}
		recorder, req := requestWithQuery("/advisories?kev=sometimes")
		if _, ok := parseAdvisoryCatalogKEVOnly(recorder, req); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("bad kev = ok(%v) status %d, want rejection", ok, recorder.Code)
		}
	})
	t.Run("Cursor", func(t *testing.T) {
		t.Parallel()
		_, req := requestWithQuery("/advisories")
		if cvss, key, ok := parseAdvisoryCatalogCursor(httptest.NewRecorder(), req); !ok || cvss != 0 || key != "" {
			t.Fatalf("empty cursor = (%v, %q, %v), want zeros", cvss, key, ok)
		}
		recorder, req := requestWithQuery("/advisories?after_cvss=7.5")
		if _, _, ok := parseAdvisoryCatalogCursor(recorder, req); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("half cursor = ok(%v) status %d, want rejection", ok, recorder.Code)
		}
		recorder, req = requestWithQuery("/advisories?after_cvss=high&after_advisory_key=k")
		if _, _, ok := parseAdvisoryCatalogCursor(recorder, req); ok {
			t.Fatal("non-numeric cvss accepted, want rejection")
		}
		_, req = requestWithQuery("/advisories?after_cvss=7.5&after_advisory_key=k-1")
		if cvss, key, ok := parseAdvisoryCatalogCursor(httptest.NewRecorder(), req); !ok || cvss != 7.5 || key != "k-1" {
			t.Fatalf("cursor = (%v, %q, %v), want (7.5, k-1, true)", cvss, key, ok)
		}
	})
	t.Run("ResponseScope", func(t *testing.T) {
		t.Parallel()
		if got := advisoryCatalogResponseScope(advisoryCatalogFilterForTest()); len(got) != 0 {
			t.Fatalf("empty scope = %v, want empty", got)
		}
	})
}

func TestAdvisoryEvidenceParsers(t *testing.T) {
	t.Parallel()

	t.Run("RequiredLimit", func(t *testing.T) {
		t.Parallel()
		recorder, req := requestWithQuery("/evidence")
		if _, ok := requiredAdvisoryEvidenceLimit(recorder, req); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("missing limit = ok(%v) status %d, want rejection", ok, recorder.Code)
		}
		recorder, req = requestWithQuery("/evidence?limit=10")
		if limit, ok := requiredAdvisoryEvidenceLimit(recorder, req); !ok || limit != 10 {
			t.Fatalf("limit = (%d, %v), want (10, true)", limit, ok)
		}
	})
	t.Run("ResponseScope", func(t *testing.T) {
		t.Parallel()
		got := advisoryEvidenceResponseScope(advisoryEvidenceFilterForTest())
		if len(got) != 0 {
			t.Fatalf("empty scope = %v, want empty", got)
		}
	})
	t.Run("DetailFilter", func(t *testing.T) {
		t.Parallel()
		if got := vulnerabilityDetailFilter("CVE-2026-0001"); got.CVEID != "CVE-2026-0001" {
			t.Fatalf("CVE filter = %+v, want the CVE routed to CVEID", got)
		}
		if got := vulnerabilityDetailFilter("GHSA-xxxx-yyyy-zzzz"); got.ID != "GHSA-xxxx-yyyy-zzzz" {
			t.Fatalf("GHSA filter = %+v, want the advisory routed to ID", got)
		}
		if !isCVEIdentifier(" cve-2026-1 ") || isCVEIdentifier("GHSA-x") {
			t.Fatal("isCVEIdentifier misclassifies the prefix")
		}
	})
}

func TestAggregateScopes(t *testing.T) {
	t.Parallel()

	// The impact scope always carries the resolved detection profile,
	// defaulting to comprehensive.
	if got := supplyChainImpactAggregateScope(impactAggregateFilterForTest()); len(got) != 1 || got["profile"] != "comprehensive" {
		t.Fatalf("empty impact scope = %v, want only the default profile", got)
	}
	full := impactAggregateFilterForTest()
	full.CVEID = "CVE-2026-0001"
	if got := supplyChainImpactAggregateScope(full); got["cve_id"] != "CVE-2026-0001" {
		t.Fatalf("impact scope = %v, want the CVE carried", got)
	}
	if got := containerImageIdentityAggregateScope(containerImageIdentityAggregateFilterForTest()); len(got) != 0 {
		t.Fatalf("empty container scope = %v, want empty", got)
	}
	if got := SBOMAttestationAttachmentAggregateScope(sbomAttachmentAggregateFilterForTest()); len(got) != 0 {
		t.Fatalf("empty SBOM scope = %v, want empty", got)
	}
	fullSBOM := sbomAttachmentAggregateFilterForTest()
	fullSBOM.SubjectDigest = "sha256:1"
	fullSBOM.AttachmentStatus = "attached_verified"
	if got := SBOMAttestationAttachmentAggregateScope(fullSBOM); got["subject_digest"] != "sha256:1" || got["attachment_status"] != "attached_verified" {
		t.Fatalf("SBOM scope = %v, want both fields carried", got)
	}
	if got := securityAlertReconciliationAggregateScope(securityAlertReconciliationAggregateFilterForTest()); len(got) != 0 {
		t.Fatalf("empty security scope = %v, want empty", got)
	}
}

func TestAggregateDimensions(t *testing.T) {
	t.Parallel()

	if !isSupportedSupplyChainImpactDimension(impact.InventoryBySeverity) {
		t.Fatal("severity dimension must be supported")
	}
	if isSupportedSupplyChainImpactDimension("nope") {
		t.Fatal("unknown impact dimension must be rejected")
	}
	if !isSupportedContainerImageIdentityDimension(ContainerImageIdentityInventoryByOutcome) {
		t.Fatal("outcome dimension must be supported")
	}
	if isSupportedContainerImageIdentityDimension("nope") {
		t.Fatal("unknown container dimension must be rejected")
	}
	if !isSupportedSBOMAttestationAttachmentDimension(SBOMAttestationAttachmentInventoryByArtifactKind) {
		t.Fatal("artifact-kind dimension must be supported")
	}
	if isSupportedSBOMAttestationAttachmentDimension("nope") {
		t.Fatal("unknown SBOM dimension must be rejected")
	}
	if !isSupportedSecurityAlertReconciliationDimension(SecurityAlertReconciliationInventoryByProvider) {
		t.Fatal("provider dimension must be supported")
	}
	if isSupportedSecurityAlertReconciliationDimension("nope") {
		t.Fatal("unknown security dimension must be rejected")
	}
}

func TestAggregateLimitOffsetParsers(t *testing.T) {
	t.Parallel()

	parseCases := []struct {
		name  string
		parse func(http.ResponseWriter, *http.Request) (int, bool)
		maxed string
	}{
		{"ImpactLimit", parseSupplyChainImpactAggregateLimit, "limit=1000000"},
		{"ImpactOffset", parseSupplyChainImpactAggregateOffset, "offset=20000"},
		{"SBOMLimit", parseSBOMAttestationAttachmentAggregateLimit, "limit=1000000"},
		{"SBOMOffset", parseSBOMAttestationAttachmentAggregateOffset, "offset=20000"},
		{"SecurityLimit", parseSecurityAlertReconciliationAggregateLimit, "limit=1000000"},
		{"SecurityOffset", parseSecurityAlertReconciliationAggregateOffset, "offset=20000"},
		{"ContainerLimit", parseContainerImageIdentityAggregateLimit, "limit=1000000"},
		{"ContainerOffset", parseContainerImageIdentityAggregateOffset, "offset=20000"},
	}
	for _, tc := range parseCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder, req := requestWithQuery("/x?" + tc.maxed)
			if _, ok := tc.parse(recorder, req); ok || recorder.Code != http.StatusBadRequest {
				t.Fatalf("over-max = ok(%v) status %d, want rejection", ok, recorder.Code)
			}
			_, req = requestWithQuery("/x")
			if value, ok := tc.parse(httptest.NewRecorder(), req); !ok || value < 0 {
				t.Fatalf("default = (%d, %v), want the default with ok", value, ok)
			}
		})
	}
}

func TestAggregateFilterValidators(t *testing.T) {
	t.Parallel()

	recorder, _ := requestWithQuery("/x")
	if !validateSBOMAttestationAttachmentAggregateFilters(recorder, sbomAttachmentAggregateFilterForTest()) {
		t.Fatal("empty SBOM filter must validate")
	}
	bad := sbomAttachmentAggregateFilterForTest()
	bad.AttachmentStatus = "bogus"
	recorder, _ = requestWithQuery("/x")
	if validateSBOMAttestationAttachmentAggregateFilters(recorder, bad) || recorder.Code != http.StatusBadRequest {
		t.Fatal("bad attachment status must be rejected")
	}
	bad = sbomAttachmentAggregateFilterForTest()
	bad.ArtifactKind = "bogus"
	recorder, _ = requestWithQuery("/x")
	if validateSBOMAttestationAttachmentAggregateFilters(recorder, bad) {
		t.Fatal("bad artifact kind must be rejected")
	}
	recorder, _ = requestWithQuery("/x")
	if !validateContainerImageIdentityAggregateOutcome(recorder, containerImageIdentityAggregateFilterForTest()) {
		t.Fatal("empty container filter must validate")
	}
}

func TestRequestedAggregateProfile(t *testing.T) {
	t.Parallel()

	precise := impactAggregateFilterForTest()
	precise.DetectionProfile = impact.ProfilePrecise
	if got := requestedSupplyChainImpactAggregateProfile(precise); got != impact.ProfilePrecise {
		t.Fatalf("profile = %q, want precise preserved", got)
	}
	if got := requestedSupplyChainImpactAggregateProfile(impactAggregateFilterForTest()); got != impact.ProfileComprehensive {
		t.Fatalf("profile = %q, want comprehensive by default", got)
	}
}

func TestContainerImageIdentityAggregateSourceBridge(t *testing.T) {
	t.Parallel()

	empty := containerImageIdentityAggregateSourceBridge("repo-a", ContainerImageIdentityAggregateCount{})
	if len(empty.MissingEvidence) != 3 {
		t.Fatalf("missing = %v, want all three evidence gaps for a zero count", empty.MissingEvidence)
	}
	if empty.SourceRepositoryID != "repo-a" {
		t.Fatalf("source = %q, want the caller repository carried", empty.SourceRepositoryID)
	}
	nonzero := containerImageIdentityAggregateSourceBridge("repo-a", ContainerImageIdentityAggregateCount{TotalIdentities: 4})
	if len(nonzero.MissingEvidence) != 0 {
		t.Fatalf("missing = %v, want none with observed identities", nonzero.MissingEvidence)
	}
}

func TestCloudRuntimeProbePerDigestLimit(t *testing.T) {
	t.Parallel()

	if got := CloudRuntimeProbePerDigestLimit(0); got != CloudRuntimeProbePerDigestMinResults {
		t.Fatalf("zero digests = %d, want the floor", got)
	}
	if got := CloudRuntimeProbePerDigestLimit(-3); got != CloudRuntimeProbePerDigestMinResults {
		t.Fatalf("negative digests = %d, want the floor", got)
	}
	if got := CloudRuntimeProbePerDigestLimit(1000000); got != CloudRuntimeProbePerDigestMinResults {
		t.Fatalf("huge fan-out = %d, want the floor", got)
	}
	if got := CloudRuntimeProbePerDigestLimit(2); got != CloudRuntimeProbeMaxResults/2 {
		t.Fatalf("two digests = %d, want the even share", got)
	}
}

func TestBuildSBOMAttestationAttachmentResult(t *testing.T) {
	t.Parallel()

	row := SBOMAttestationAttachmentRow{
		WarningSummaries: []string{"stale", "stale", "unsigned"},
	}
	got := buildSBOMAttestationAttachmentResult(row)
	if got.WarningSummaryCount != 3 || !got.WarningSummariesTruncated {
		t.Fatalf("result = %+v, want count 3 with truncation disclosed", got)
	}
	if len(got.WarningSummaries) != 2 {
		t.Fatalf("summaries = %v, want deduplicated preview", got.WarningSummaries)
	}
	override := SBOMAttestationAttachmentRow{WarningSummaries: []string{"a"}, WarningSummaryCount: 9}
	if got := buildSBOMAttestationAttachmentResult(override); got.WarningSummaryCount != 9 || !got.WarningSummariesTruncated {
		t.Fatalf("result = %+v, want the persisted total to win with truncation", got)
	}
}

func TestSuppressionAuthorAndJustification(t *testing.T) {
	t.Parallel()

	if !operatorSuppressionJustificationAllowed("not_affected") || !operatorSuppressionJustificationAllowed("ignored") {
		t.Fatal("known justifications must be allowed")
	}
	if operatorSuppressionJustificationAllowed("provider_dismissed") || operatorSuppressionJustificationAllowed("whatever") {
		t.Fatal("provider-dismissed and unknown justifications must be rejected for operator use")
	}
	if got := vulnerabilitySuppressionAuthor(authContextForTest("", "")); got != "authenticated_operator" {
		t.Fatalf("author = %q, want the default subject", got)
	}
	if got := vulnerabilitySuppressionAuthor(authContextForTest("service", "abc123")); got != "service:abc123" {
		t.Fatalf("author = %q, want class and hash joined", got)
	}
}

func TestCatalogAndEvidenceRequiredLimits(t *testing.T) {
	t.Parallel()

	recorder, req := requestWithQuery("/x")
	if _, ok := requiredSecurityAlertReconciliationLimit(recorder, req); ok {
		t.Fatal("missing security limit accepted, want rejection")
	}
	recorder, req = requestWithQuery("/x?limit=5")
	if limit, ok := requiredSecurityAlertReconciliationLimit(recorder, req); !ok || limit != 5 {
		t.Fatalf("limit = (%d, %v), want (5, true)", limit, ok)
	}
	recorder, req = requestWithQuery("/x")
	if _, ok := requiredSBOMAttestationAttachmentLimit(recorder, req); ok {
		t.Fatal("missing SBOM limit accepted, want rejection")
	}
	recorder, req = requestWithQuery("/x?limit=7")
	if limit, ok := requiredSBOMAttestationAttachmentLimit(recorder, req); !ok || limit != 7 {
		t.Fatalf("limit = (%d, %v), want (7, true)", limit, ok)
	}
	recorder, req = requestWithQuery("/x")
	if _, ok := requiredContainerImageIdentityLimit(recorder, req); ok {
		t.Fatal("missing container limit accepted, want rejection")
	}
}
