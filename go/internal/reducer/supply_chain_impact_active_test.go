// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testImpactReferrerDigest      = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	testImpactRepositoryFilterIDs = "git-repository-scope:" + testImpactRepositoryID + "," + testImpactRepositoryID
)

func TestSupplyChainImpactHandlerExpandsActiveEvidenceUntilSBOMImagePathIsLoaded(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			packageRegistryPackageImpactFact("package-1", testImpactPackageID),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			switch {
			case len(filter.PackageIDs) > 0:
				affected := vulnerabilityAffectedPackageFact(
					"affected-1",
					"CVE-2026-0001",
					testImpactPackageID,
					"npm",
					"example",
					"1.2.3",
					"1.3.0",
				)
				affected.Payload["purl"] = testImpactPURL
				return []facts.Envelope{
					affected,
				}
			case len(filter.PURLs) > 0:
				return []facts.Envelope{
					vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
					sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
				}
			case len(filter.DocumentIDs) > 0:
				return []facts.Envelope{
					sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
				}
			case len(filter.SubjectDigests) > 0:
				return []facts.Envelope{
					containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
				}
			default:
				return nil
			}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "package-registry:npm:example",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Domain:       DomainSupplyChainImpact,
		Cause:        "package registry identity observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(loader.filters), 5; got != want {
		t.Fatalf("active evidence loads = %d, want %d: %#v", got, want, loader.filters)
	}
	if got, want := strings.Join(loader.filters[1].PURLs, ","), testImpactPURL; got != want {
		t.Fatalf("follow-up PURLs = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[2].DocumentIDs, ","), "doc-1"; got != want {
		t.Fatalf("follow-up document IDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[3].SubjectDigests, ","), testImpactSubjectDigest; got != want {
		t.Fatalf("follow-up subject digests = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[4].RepositoryIDs, ","), testImpactRepositoryFilterIDs; got != want {
		t.Fatalf("follow-up repository IDs = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	assertSupplyChainImpactStatus(t, writer.write.Findings[0], SupplyChainImpactAffectedDerived)
	if writer.write.Findings[0].SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", writer.write.Findings[0].SubjectDigest, testImpactSubjectDigest)
	}
	if writer.write.Findings[0].RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", writer.write.Findings[0].RepositoryID, testImpactRepositoryID)
	}
}

func TestSupplyChainImpactHandlerExpandsActiveEvidenceFromOCIReferrer(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			ociImageReferrerFact(
				"referrer-1",
				testImpactSubjectDigest,
				testImpactReferrerDigest,
				"application/vnd.cyclonedx+json",
			),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			switch {
			case len(filter.SubjectDigests) > 0:
				return []facts.Envelope{
					sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
					containerImageIdentityImpactFact("image-1", testImpactSubjectDigest, testImpactRepositoryID),
				}
			case len(filter.DocumentIDs) > 0:
				return []facts.Envelope{
					sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
				}
			case len(filter.PURLs) > 0:
				affected := vulnerabilityAffectedPackageFact(
					"affected-1",
					"CVE-2026-1457",
					testImpactPackageID,
					"npm",
					"example",
					"1.2.3",
					"1.3.0",
				)
				affected.Payload["purl"] = testImpactPURL
				return []facts.Envelope{
					vulnerabilityCVEFact("cve-1", "CVE-2026-1457", 9.4),
					affected,
				}
			default:
				return nil
			}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact-oci",
		ScopeID:      "oci-registry://registry.example.com/team/api",
		GenerationID: "generation-oci",
		SourceSystem: "oci_registry",
		Domain:       DomainSupplyChainImpact,
		Cause:        "OCI image subject evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(loader.filters), 4; got != want {
		t.Fatalf("active evidence loads = %d, want %d: %#v", got, want, loader.filters)
	}
	if got, want := strings.Join(loader.filters[0].SubjectDigests, ","), testImpactSubjectDigest+","+testImpactReferrerDigest; got != want {
		t.Fatalf("initial subject digests = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[1].DocumentIDs, ","), "doc-1"; got != want {
		t.Fatalf("follow-up document IDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[1].RepositoryIDs, ","), testImpactRepositoryFilterIDs; got != want {
		t.Fatalf("document follow-up repository IDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.filters[2].PURLs, ","), testImpactPURL; got != want {
		t.Fatalf("follow-up PURLs = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	got := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedDerived)
	if got.SubjectDigest != testImpactSubjectDigest {
		t.Fatalf("SubjectDigest = %q, want %q", got.SubjectDigest, testImpactSubjectDigest)
	}
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	assertContainsString(t, got.EvidenceFactIDs, "attachment-1")
	assertContainsString(t, got.EvidenceFactIDs, "image-1")
}

func TestSupplyChainImpactHandlerLoadsActiveWorkloadIdentityForRepositoryFinding(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0680", 9.1),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0680", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			if strings.Join(filter.RepositoryIDs, ",") != testImpactRepositoryFilterIDs {
				return nil
			}
			return []facts.Envelope{
				workloadIdentityImpactFact("workload-1", testImpactRepositoryID, testImpactWorkloadID),
			}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      testImpactRepositoryID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       DomainSupplyChainImpact,
		Cause:        "repository dependency observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	got := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.WorkloadIDs, testImpactWorkloadID)
	assertContainsString(t, got.EvidenceFactIDs, "workload-1")
	assertNotContainsString(t, got.MissingEvidence, "workload evidence missing")
	assertNotContainsString(t, got.MissingEvidence, "service evidence missing")
	assertContainsString(t, got.MissingEvidence, "service catalog correlation evidence missing")
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no service identity without service catalog evidence", got.ServiceIDs)
	}
}

func TestSupplyChainImpactHandlerLoadsRepositoryPackageConsumptionFollowUp(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-1457", 9.4),
			vulnerabilityAffectedPackageFact(
				"affected-1",
				"CVE-2026-1457",
				testImpactPackageID,
				"npm",
				"example",
				"1.2.3",
				"1.3.0",
			),
			securityAlertRepositoryAlertImpactFact(
				"alert-1",
				testImpactRepositoryID,
				testImpactPackageID,
				"CVE-2026-1457",
			),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			if strings.Join(filter.RepositoryIDs, ",") != testImpactRepositoryFilterIDs {
				return nil
			}
			return []facts.Envelope{
				packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
			}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      "security-alerts:repo",
		GenerationID: "generation-alert",
		SourceSystem: "security_alert",
		Domain:       DomainSupplyChainImpact,
		Cause:        "provider alert observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	got := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	assertContainsString(t, got.EvidenceFactIDs, "consume-1")
}

func TestSupplyChainImpactHandlerStopsActiveEvidenceExpansionConservatively(t *testing.T) {
	t.Parallel()

	activeCalls := 0
	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-2635", 8.1),
			vulnerabilityAffectedPackageFact(
				"affected-1",
				"CVE-2026-2635",
				testImpactPackageID,
				"npm",
				"example",
				"1.2.3",
				"1.3.0",
			),
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			if len(filter.PackageIDs) == 0 {
				return nil
			}
			activeCalls++
			return []facts.Envelope{
				packageRegistryPackageImpactFact(
					"package-expansion-"+string(rune('a'+activeCalls)),
					"pkg:npm/expansion-"+string(rune('a'+activeCalls)),
				),
			}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact",
		ScopeID:      testImpactRepositoryID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       DomainSupplyChainImpact,
		Cause:        "repository dependency observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(loader.filters), maxSupplyChainImpactActiveEvidenceLoads; got != want {
		t.Fatalf("active evidence loads = %d, want %d: %#v", got, want, loader.filters)
	}
	if got, want := writer.calls, 1; got != want {
		t.Fatalf("writer calls = %d, want %d", got, want)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	got := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	assertContainsString(t, got.MissingEvidence, supplyChainMissingActiveEvidenceExpansionLimit)
	if !strings.Contains(result.EvidenceSummary, "active_evidence_truncated=true") {
		t.Fatalf("EvidenceSummary = %q, want active evidence truncation marker", result.EvidenceSummary)
	}
}

func TestSupplyChainImpactHandlerRequestsParserFilesOnlyForNPMReachability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		affected              facts.Envelope
		consumptionPackageID  string
		wantFileRepositoryIDs string
	}{
		{
			name: "maven finding keeps repository follow-up but does not request parser files",
			affected: vulnerabilityAffectedPackageFact(
				"affected-maven",
				"CVE-2026-118505",
				"pkg:maven/org.example/vulnerable-api",
				"maven",
				"org.example:vulnerable-api",
				"1.2.3",
				"1.3.0",
			),
			consumptionPackageID: "pkg:maven/org.example/vulnerable-api",
		},
		{
			name: "npm finding requests parser files for JS TS package API reachability",
			affected: vulnerabilityAffectedPackageFact(
				"affected-npm",
				"CVE-2026-118506",
				"pkg:npm/vulnerable-api",
				"npm",
				"vulnerable-api",
				"1.2.3",
				"1.3.0",
			),
			consumptionPackageID:  "pkg:npm/vulnerable-api",
			wantFileRepositoryIDs: testImpactRepositoryID,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			loader := &stubSupplyChainImpactFactLoader{
				scopeFacts: []facts.Envelope{
					vulnerabilityCVEFact("cve-1", payloadStr(tc.affected.Payload, "cve_id"), 7.8),
					tc.affected,
					packageConsumptionFactWithRange("consume-1", tc.consumptionPackageID, testImpactRepositoryID, "1.2.3"),
				},
			}
			writer := &recordingSupplyChainImpactWriter{}
			handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

			_, err := handler.Handle(context.Background(), Intent{
				IntentID:     "intent-impact",
				ScopeID:      testImpactRepositoryID,
				GenerationID: "generation-repo",
				SourceSystem: "git",
				Domain:       DomainSupplyChainImpact,
				Cause:        "repository dependency observed",
			})
			if err != nil {
				t.Fatalf("Handle() error = %v, want nil", err)
			}

			if got, want := len(loader.filters), 1; got != want {
				t.Fatalf("active evidence loads = %d, want %d: %#v", got, want, loader.filters)
			}
			filter := loader.filters[0]
			if got, want := strings.Join(filter.RepositoryIDs, ","), testImpactRepositoryFilterIDs; got != want {
				t.Fatalf("RepositoryIDs = %q, want %q", got, want)
			}
			if got := strings.Join(filter.FileRepositoryIDs, ","); got != tc.wantFileRepositoryIDs {
				t.Fatalf("FileRepositoryIDs = %q, want %q", got, tc.wantFileRepositoryIDs)
			}
		})
	}
}

func securityAlertRepositoryAlertImpactFact(
	factID string,
	repositoryID string,
	packageID string,
	cveID string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SecurityAlertRepositoryAlertFactKind,
		Payload: map[string]any{
			"repository_id": repositoryID,
			"package_id":    packageID,
			"cve_ids":       []any{cveID},
		},
	}
}
