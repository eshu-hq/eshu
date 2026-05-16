package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactPackageID     = "pkg:npm/example"
	testImpactPURL          = "pkg:npm/example@1.2.3"
	testImpactFixedPURL     = "pkg:npm/example@1.3.0"
	testImpactRepositoryID  = "repo://example/api"
	testImpactSubjectDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type stubSupplyChainImpactFactLoader struct {
	scopeFacts []facts.Envelope
	active     []facts.Envelope
	kindCalls  [][]string
	filter     SupplyChainImpactFactFilter
}

func (s *stubSupplyChainImpactFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSupplyChainImpactFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	s.filter = filter
	return append([]facts.Envelope(nil), s.active...), nil
}

type recordingSupplyChainImpactWriter struct {
	write SupplyChainImpactWrite
	calls int
}

func (w *recordingSupplyChainImpactWriter) WriteSupplyChainImpactFindings(
	_ context.Context,
	write SupplyChainImpactWrite,
) (SupplyChainImpactWriteResult, error) {
	w.calls++
	w.write = write
	return SupplyChainImpactWriteResult{
		CanonicalWrites: supplyChainImpactCanonicalWrites(write.Findings),
		FactsWritten:    len(write.Findings),
	}, nil
}

func TestBuildSupplyChainImpactFindingsClassifiesEvidencePaths(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		vulnerabilityEPSSFact("epss-1", "CVE-2026-0001", "0.71", "0.98"),
		vulnerabilityKEVFact("kev-1", "CVE-2026-0001"),
		packageVersionFact("version-1", testImpactPackageID, "pkg:npm/example@1.2.3", "1.2.3"),
		packageConsumptionFact("consume-1", testImpactPackageID, testImpactRepositoryID),

		vulnerabilityCVEFact("cve-fixed", "CVE-2026-0002", 9.8),
		vulnerabilityAffectedPackageFact("affected-fixed", "CVE-2026-0002", "pkg:npm/fixed", "npm", "fixed", "1.2.3", "1.3.0"),
		packageVersionFact("version-fixed", "pkg:npm/fixed", "pkg:npm/fixed@1.3.0", "1.3.0"),

		vulnerabilityCVEFact("cve-possible", "CVE-2026-0003", 5.0),
		vulnerabilityAffectedPackageFact("affected-possible", "CVE-2026-0003", "pkg:npm/other", "npm", "other", "", "2.0.0"),

		vulnerabilityCVEFact("cve-unknown", "CVE-2026-0004", 7.5),
	})

	got := supplyChainImpactFindingsByCVE(findings)
	assertSupplyChainImpactStatus(t, got["CVE-2026-0001"], SupplyChainImpactAffectedExact)
	if got["CVE-2026-0001"].PriorityReason == "" || !got["CVE-2026-0001"].KnownExploited {
		t.Fatalf("CVE-2026-0001 priority signals missing: %#v", got["CVE-2026-0001"])
	}
	if got["CVE-2026-0001"].RuntimeReachability == "" {
		t.Fatalf("CVE-2026-0001 RuntimeReachability = blank, want package reachability")
	}
	assertSupplyChainImpactStatus(t, got["CVE-2026-0002"], SupplyChainImpactNotAffectedKnownFixed)
	assertSupplyChainImpactStatus(t, got["CVE-2026-0003"], SupplyChainImpactPossiblyAffected)
	assertSupplyChainImpactStatus(t, got["CVE-2026-0004"], SupplyChainImpactUnknown)
}

func TestBuildSupplyChainImpactFindingsDerivesImagePathFromSBOMAttachment(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
		sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
	if !strings.Contains(strings.Join(got.EvidencePath, " -> "), "sbom.component") {
		t.Fatalf("EvidencePath = %#v, want SBOM component path", got.EvidencePath)
	}
}

func TestBuildSupplyChainImpactFindingsRequiresAffectedVersionForExactImpact(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 8.0),
		vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "", "1.3.0"),
		packageVersionFact("version-1", testImpactPackageID, testImpactPURL, "1.2.3"),
		packageConsumptionFact("consume-1", testImpactPackageID, testImpactRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-0001"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.RuntimeReachability != "unknown" {
		t.Fatalf("RuntimeReachability = %q, want unknown without affected-version proof", got.RuntimeReachability)
	}
}

func TestSupplyChainImpactHandlerLoadsActiveEvidenceAndWritesFindings(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		},
		active: []facts.Envelope{
			packageVersionFact("version-1", testImpactPackageID, testImpactPURL, "1.2.3"),
			packageConsumptionFact("consume-1", testImpactPackageID, testImpactRepositoryID),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), strings.Join(supplyChainImpactFactKinds(), ","); got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filter.PackageIDs, ","), testImpactPackageID; got != want {
		t.Fatalf("active package IDs = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestSupplyChainImpactStableFactKeyIncludesRepository(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}
	left := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0001",
		PackageID:     testImpactPackageID,
		RepositoryID:  "repo://example/api",
		SubjectDigest: testImpactSubjectDigest,
	})
	right := supplyChainImpactStableFactKey(write, SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0001",
		PackageID:     testImpactPackageID,
		RepositoryID:  "repo://example/worker",
		SubjectDigest: testImpactSubjectDigest,
	})
	if left == right {
		t.Fatalf("stable fact keys collapsed distinct repositories: %q", left)
	}
	if !strings.Contains(left, ":repo://example/api:") {
		t.Fatalf("stable fact key = %q, want repository identity segment", left)
	}
}

func TestPostgresSupplyChainImpactWriterPersistsSignalsWithoutPriorityCollapse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 16, 16, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSupplyChainImpactWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), SupplyChainImpactWrite{
		IntentID:     "intent-impact",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact",
		SourceSystem: "vulnerability_intelligence",
		Cause:        "vulnerability evidence observed",
		Findings: []SupplyChainImpactFinding{
			{
				CVEID:               "CVE-2026-0001",
				PackageID:           testImpactPackageID,
				PURL:                testImpactPURL,
				ObservedVersion:     "1.2.3",
				FixedVersion:        "1.3.0",
				Status:              SupplyChainImpactAffectedExact,
				Confidence:          "exact",
				CVSSScore:           9.8,
				EPSSProbability:     "0.71",
				EPSSPercentile:      "0.98",
				KnownExploited:      true,
				RuntimeReachability: "package_manifest",
				RepositoryID:        testImpactRepositoryID,
				CanonicalWrites:     1,
				EvidenceFactIDs:     []string{"cve-1", "affected-1", "version-1"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 || result.FactsWritten != 1 {
		t.Fatalf("result = %#v, want one canonical fact", result)
	}
	payload := unmarshalSupplyChainImpactPayload(t, db.execs[0].args[14])
	if got, want := payload["impact_status"], string(SupplyChainImpactAffectedExact); got != want {
		t.Fatalf("impact_status = %#v, want %#v", got, want)
	}
	if _, exists := payload["priority"]; exists {
		t.Fatalf("payload must keep risk signals separate instead of emitting opaque priority: %#v", payload)
	}
	if got, want := payload["known_exploited"], true; got != want {
		t.Fatalf("known_exploited = %#v, want %#v", got, want)
	}
}

func supplyChainImpactFindingsByCVE(findings []SupplyChainImpactFinding) map[string]SupplyChainImpactFinding {
	out := make(map[string]SupplyChainImpactFinding, len(findings))
	for _, finding := range findings {
		out[finding.CVEID] = finding
	}
	return out
}

func assertSupplyChainImpactStatus(
	t *testing.T,
	finding SupplyChainImpactFinding,
	status SupplyChainImpactStatus,
) {
	t.Helper()
	if finding.Status != status {
		t.Fatalf("Status = %q, want %q for %#v", finding.Status, status, finding)
	}
}

func unmarshalSupplyChainImpactPayload(t *testing.T, raw any) map[string]any {
	t.Helper()

	var payload map[string]any
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}
	return payload
}

func vulnerabilityCVEFact(factID string, cveID string, cvssScore float64) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":     cveID,
			"cvss_score": cvssScore,
			"aliases":    []any{cveID},
		},
	}
}

func vulnerabilityAffectedPackageFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	affectedVersion string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            cveID,
			"package_id":        packageID,
			"ecosystem":         ecosystem,
			"package_name":      name,
			"affected_versions": []any{affectedVersion},
			"fixed_versions":    []any{fixedVersion},
		},
	}
}

func vulnerabilityEPSSFact(factID string, cveID string, probability string, percentile string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityEPSSScoreFactKind,
		Payload: map[string]any{
			"cve_id":      cveID,
			"probability": probability,
			"percentile":  percentile,
			"score_date":  "2026-05-16",
		},
	}
}

func vulnerabilityKEVFact(factID string, cveID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityKnownExploitedFactKind,
		Payload: map[string]any{
			"cve_id":     cveID,
			"date_added": "2026-05-16",
		},
	}
}

func packageVersionFact(factID string, packageID string, purl string, version string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.PackageRegistryPackageVersionFactKind,
		Payload: map[string]any{
			"package_id": packageID,
			"purl":       purl,
			"version":    version,
		},
	}
}

func packageConsumptionFact(factID string, packageID string, repositoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: packageConsumptionCorrelationFactKind,
		Payload: map[string]any{
			"package_id":        packageID,
			"relationship_kind": "consumption",
			"repository_id":     repositoryID,
			"canonical_writes":  1,
			"evidence_fact_ids": []any{"manifest-1"},
		},
	}
}

func sbomComponentImpactFact(factID string, documentID string, purl string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			"document_id": documentID,
			"purl":        purl,
			"name":        "example",
			"version":     "1.2.3",
		},
	}
}

func sbomAttachmentImpactFact(factID string, documentID string, subjectDigest string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: sbomAttestationAttachmentFactKind,
		Payload: map[string]any{
			"document_id":       documentID,
			"subject_digest":    subjectDigest,
			"attachment_status": "attached_verified",
			"canonical_writes":  1,
		},
	}
}

func containerImageIdentityImpactFact(factID string, digest string, repositoryID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: containerImageIdentityFactKind,
		Payload: map[string]any{
			"digest":           digest,
			"repository_id":    repositoryID,
			"canonical_writes": 1,
		},
	}
}
