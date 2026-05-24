package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

	if got, want := len(loader.filters), 4; got != want {
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
