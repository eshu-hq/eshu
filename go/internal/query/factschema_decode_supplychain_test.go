// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestSupplyChainDecodeWrappersClassifyMissingRequiredField proves the
// accuracy guarantee eshu-contract-rigor names: a required payload key
// absent (or null) from a source-fact payload must dead-letter as a
// classified input_invalid *queryDecodeError, never a silent zero-value
// struct. Table-driven so every #4795 W2b supply-chain decode wrapper is
// covered by the same assertion.
func TestSupplyChainDecodeWrappersClassifyMissingRequiredField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		decode       func(supplyChainFactDecodeInput) error
		payload      map[string]any
		wantFactKind string
		missingField string
	}{
		{
			name: "vulnerability.cve missing advisory_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityCVE(in)
				return err
			},
			payload:      map[string]any{"cve_id": "CVE-2026-0001"},
			wantFactKind: factschema.FactKindVulnerabilityCVE,
			missingField: "advisory_id",
		},
		{
			name: "vulnerability.affected_package missing advisory_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityAffectedPackage(in)
				return err
			},
			payload:      map[string]any{"cve_id": "CVE-2026-0001", "ecosystem": "npm"},
			wantFactKind: factschema.FactKindVulnerabilityAffectedPackage,
			missingField: "advisory_id",
		},
		{
			name: "vulnerability.epss_score missing cve_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityEPSSScore(in)
				return err
			},
			payload:      map[string]any{"probability": "0.5"},
			wantFactKind: factschema.FactKindVulnerabilityEPSSScore,
			missingField: "cve_id",
		},
		{
			name: "vulnerability.known_exploited missing cve_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeVulnerabilityKnownExploited(in)
				return err
			},
			payload:      map[string]any{"vendor_project": "Example Corp"},
			wantFactKind: factschema.FactKindVulnerabilityKnownExploited,
			missingField: "cve_id",
		},
		{
			name: "sbom.document missing document_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeSBOMDocument(in)
				return err
			},
			payload:      map[string]any{"format": "cyclonedx"},
			wantFactKind: factschema.FactKindSBOMDocument,
			missingField: "document_id",
		},
		{
			name: "sbom.component missing document_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeSBOMComponent(in)
				return err
			},
			payload:      map[string]any{"purl": "pkg:npm/example@1.0.0"},
			wantFactKind: factschema.FactKindSBOMComponent,
			missingField: "document_id",
		},
		{
			name: "package_registry.package_dependency missing package_id",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodePackageRegistryPackageDependency(in)
				return err
			},
			payload: map[string]any{
				"version_id":            "v1",
				"dependency_package_id": "pkg:npm/dep",
			},
			wantFactKind: factschema.FactKindPackageRegistryPackageDependency,
			missingField: "package_id",
		},
		{
			name: "service_catalog.entity missing entity_ref",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeServiceCatalogEntity(in)
				return err
			},
			payload:      map[string]any{"provider": "backstage"},
			wantFactKind: factschema.FactKindServiceCatalogEntity,
			missingField: "entity_ref",
		},
		{
			name: "service_catalog.ownership missing entity_ref",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeServiceCatalogOwnership(in)
				return err
			},
			payload:      map[string]any{"owner_ref": "group:default/payments"},
			wantFactKind: factschema.FactKindServiceCatalogOwnership,
			missingField: "entity_ref",
		},
		{
			name: "service_catalog.repository_link missing entity_ref",
			decode: func(in supplyChainFactDecodeInput) error {
				_, err := decodeServiceCatalogRepositoryLink(in)
				return err
			},
			payload:      map[string]any{"repository_id": "repo-1"},
			wantFactKind: factschema.FactKindServiceCatalogRepositoryLink,
			missingField: "entity_ref",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.decode(supplyChainFactDecodeInput{FactID: "fact-1", Payload: tc.payload})
			if err == nil {
				t.Fatalf("decode error = nil, want classified input_invalid error for missing %q", tc.missingField)
			}
			var queryErr *queryDecodeError
			if !errors.As(err, &queryErr) {
				t.Fatalf("error = %v (%T), want *queryDecodeError", err, err)
			}
			if queryErr.Classification != factschema.ClassificationInputInvalid {
				t.Fatalf("Classification = %q, want %q", queryErr.Classification, factschema.ClassificationInputInvalid)
			}
			if queryErr.FactKind != tc.wantFactKind {
				t.Fatalf("FactKind = %q, want %q", queryErr.FactKind, tc.wantFactKind)
			}
			if queryErr.FactID != "fact-1" {
				t.Fatalf("FactID = %q, want %q", queryErr.FactID, "fact-1")
			}
			if queryErr.Field != tc.missingField {
				t.Fatalf("Field = %q, want %q", queryErr.Field, tc.missingField)
			}
		})
	}
}

// TestDecodeSupplyChainComponentEvidenceFallsBackForUnmatchedKind proves the
// dispatcher's fallback contract: an evidence fact whose kind has no landed
// factschema struct (chiefly reducer-derived kinds pending their own W1
// struct per the #4784 ADR) reports Matched=false so callers keep the
// pre-existing raw payload read unchanged.
func TestDecodeSupplyChainComponentEvidenceFallsBackForUnmatchedKind(t *testing.T) {
	t.Parallel()

	fact := SupplyChainImpactEvidenceFact{
		FactID:   "fact-unmatched",
		FactKind: "reducer_container_image_identity",
		Payload:  map[string]any{"digest": "sha256:deadbeef"},
	}

	got := decodeSupplyChainComponentEvidence(fact)
	if got.Matched {
		t.Fatalf("Matched = true, want false for an unregistered/reducer-derived fact kind")
	}
	if got.Err != nil {
		t.Fatalf("Err = %v, want nil when unmatched", got.Err)
	}
}

// TestDecodeSupplyChainComponentEvidenceDecodesKnownKinds proves the matched
// dispatch branches decode losslessly for the fields this package reads.
func TestDecodeSupplyChainComponentEvidenceDecodesKnownKinds(t *testing.T) {
	t.Parallel()

	t.Run("sbom.component", func(t *testing.T) {
		t.Parallel()
		fact := SupplyChainImpactEvidenceFact{
			FactID:   "fact-component",
			FactKind: factschema.FactKindSBOMComponent,
			Payload: map[string]any{
				"document_id":   "doc-1",
				"purl":          "pkg:npm/example@1.2.3",
				"version":       "1.2.3",
				"lockfile_path": "package-lock.json",
			},
		}
		got := decodeSupplyChainComponentEvidence(fact)
		if !got.Matched || got.Err != nil {
			t.Fatalf("Matched/Err = %v/%v, want matched with no error", got.Matched, got.Err)
		}
		if got.DocumentID != "doc-1" || got.PURL != "pkg:npm/example@1.2.3" || got.Version != "1.2.3" || got.LockfilePath != "package-lock.json" {
			t.Fatalf("decoded fields = %#v, want lossless field mapping", got)
		}
	})

	t.Run("package_registry.package_dependency", func(t *testing.T) {
		t.Parallel()
		fact := SupplyChainImpactEvidenceFact{
			FactID:   "fact-dependency",
			FactKind: factschema.FactKindPackageRegistryPackageDependency,
			Payload: map[string]any{
				"package_id":            "pkg:npm/root",
				"version_id":            "pkg:npm/root@1.0.0",
				"dependency_package_id": "pkg:npm/dep",
				"version":               "1.0.0",
				"dependency_range":      "^1.0.0",
			},
		}
		got := decodeSupplyChainComponentEvidence(fact)
		if !got.Matched || got.Err != nil {
			t.Fatalf("Matched/Err = %v/%v, want matched with no error", got.Matched, got.Err)
		}
		if got.Version != "1.0.0" || got.DependencyRange != "^1.0.0" {
			t.Fatalf("decoded fields = %#v, want Version=1.0.0 DependencyRange=^1.0.0", got)
		}
	})

	t.Run("service_catalog.ownership", func(t *testing.T) {
		t.Parallel()
		fact := SupplyChainImpactEvidenceFact{
			FactID:   "fact-ownership",
			FactKind: factschema.FactKindServiceCatalogOwnership,
			Payload: map[string]any{
				"entity_ref": "component:default/checkout",
				"owner_ref":  "group:default/payments",
			},
		}
		got := decodeSupplyChainComponentEvidence(fact)
		if !got.Matched || got.Err != nil {
			t.Fatalf("Matched/Err = %v/%v, want matched with no error", got.Matched, got.Err)
		}
		if got.EntityRef != "component:default/checkout" || got.OwnerRef != "group:default/payments" {
			t.Fatalf("decoded fields = %#v, want entity/owner refs", got)
		}
	})
}
