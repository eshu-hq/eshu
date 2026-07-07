// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

// This file holds query-side decode wrappers for the source-fact kinds that
// feed the supply-chain advisory-evidence, impact-explanation, and
// impact-path read models (#4795 W2b). Each wraps the matching
// sdk/go/factschema Decode* seam and, on a classified *factschema.DecodeError
// (a missing/null required identity field), returns a *queryDecodeError
// (defined in factschema_decode_workitem.go, reused here rather than
// forked) so the caller can drop the fact's contribution instead of
// fabricating a zero-valued row.
//
// Governance note (#4784 ADR, docs/internal/design/4784-reducer-derived-fact-governance.md):
// these wrappers cover SOURCE-FACT kinds only. The supply-chain read models
// also read several reducer-derived kinds (reducer_supply_chain_impact_finding,
// reducer_package_ownership_correlation, reducer_package_consumption_correlation,
// reducer_package_publication_correlation, reducer_sbom_attestation_attachment,
// reducer_workload_identity, reducer_service_catalog_correlation,
// reducer_container_image_identity, ...). None of those have a landed
// sdk/go/factschema struct yet — the ADR requires W1 struct authorship before
// any W2 read site can typed-decode them — so every read of those kinds stays
// on the pre-existing raw payload path, marked inline with the fact kind that
// blocks it.
//
// Struct-completeness note: two of the wrappers below (CVE, AffectedPackage)
// are deliberately partial. vulnerability/v1.CVE and
// vulnerability/v1.AffectedPackage do not yet declare every field the
// query-side AdvisorySourceEvidence/AdvisoryAffectedPackage response models
// read from real collector payloads (for example CVE has no Aliases,
// Severity, CVSSVectorV2/V3/V4, or CVSSMetrics field; AffectedPackage has no
// ParsedAffectedRange field, and its typed AffectedRanges field is a
// different Go shape than the response's []map[string]any). Reading those
// specific keys through the typed seam would silently drop real evidence
// data emitted by OSV/NVD/GitLab Gemnasium collectors, so those specific
// fields keep their pre-existing raw payload read (each marked with a
// struct-gap comment) alongside the fields that do decode losslessly.
// vulnerability.affected_product's typed struct is missing six of the nine
// fields the response model reads (VersionStart/EndIncluding/Excluding,
// SourceConfigurationOperator/Negate, SourceNodeOperator/Negate), so that
// read site is left entirely on the raw path rather than adding a wrapper
// this package would never call losslessly.

// supplyChainFactDecodeInput carries one scanned evidence-fact row into a
// decode wrapper. Bundling FactID, SchemaVersion, and Payload into a single
// parameter keeps each wrapper's one-argument shape, matching the
// payload-usage manifest gate's seam parser convention (see
// factschema_decode_workitem.go's workItemDecodeInput).
type supplyChainFactDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// supplyChainSchemaEnvelope adapts one scanned supply-chain evidence fact row
// into the contracts-module factschema.Envelope the Decode* seam accepts. An
// empty schemaVersion is normalized to the current major-1 schema version —
// every vulnerability/SBOM/service-catalog/package-registry source-fact
// emitter stamps a concrete major-1 version, and the supply-chain read models
// scan facts through a projection that does not always carry schema_version
// in its own row shape — a present-but-unsupported major still dead-letters
// through the Decode* seam's default branch.
func supplyChainSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = queryDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeVulnerabilityCVE decodes one vulnerability.cve fact row into the
// typed struct. A missing required field (advisory_id) yields a
// self-classifying *queryDecodeError. See this file's struct-completeness
// note: callers must still read aliases/severity/cvss_v2/cvss_v3/cvss_v4/
// cvss_metrics/cwes from the raw payload — the typed struct does not
// declare them yet.
func decodeVulnerabilityCVE(in supplyChainFactDecodeInput) (vulnerabilityv1.CVE, error) {
	cve, err := factschema.DecodeVulnerabilityCVE(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityCVE, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.CVE{}, newQueryDecodeError(factschema.FactKindVulnerabilityCVE, in.FactID, err)
	}
	return cve, nil
}

// decodeVulnerabilityAffectedPackage decodes one vulnerability.affected_package
// fact row into the typed struct. A missing required field (advisory_id)
// yields a self-classifying *queryDecodeError. See this file's
// struct-completeness note: callers must still read
// parsed_affected_range/affected_ranges from the raw payload.
func decodeVulnerabilityAffectedPackage(in supplyChainFactDecodeInput) (vulnerabilityv1.AffectedPackage, error) {
	affected, err := factschema.DecodeVulnerabilityAffectedPackage(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityAffectedPackage, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.AffectedPackage{}, newQueryDecodeError(factschema.FactKindVulnerabilityAffectedPackage, in.FactID, err)
	}
	return affected, nil
}

// decodeVulnerabilityEPSSScore decodes one vulnerability.epss_score fact row
// into the typed struct. A missing required field (cve_id) yields a
// self-classifying *queryDecodeError. This kind decodes losslessly: every
// field the query-side AdvisoryEPSSObservation reads (probability,
// percentile, score_date) is declared on vulnerabilityv1.EPSSScore.
func decodeVulnerabilityEPSSScore(in supplyChainFactDecodeInput) (vulnerabilityv1.EPSSScore, error) {
	score, err := factschema.DecodeVulnerabilityEPSSScore(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityEPSSScore, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.EPSSScore{}, newQueryDecodeError(factschema.FactKindVulnerabilityEPSSScore, in.FactID, err)
	}
	return score, nil
}

// decodeVulnerabilityKnownExploited decodes one vulnerability.known_exploited
// fact row into the typed struct. A missing required field (cve_id) yields a
// self-classifying *queryDecodeError. This kind decodes losslessly: every
// field the query-side AdvisoryKEVObservation reads is declared on
// vulnerabilityv1.KnownExploited.
func decodeVulnerabilityKnownExploited(in supplyChainFactDecodeInput) (vulnerabilityv1.KnownExploited, error) {
	kev, err := factschema.DecodeVulnerabilityKnownExploited(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityKnownExploited, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.KnownExploited{}, newQueryDecodeError(factschema.FactKindVulnerabilityKnownExploited, in.FactID, err)
	}
	return kev, nil
}

// decodeSBOMDocument decodes one sbom.document fact row into the typed
// struct. A missing required field (document_id) yields a self-classifying
// *queryDecodeError.
func decodeSBOMDocument(in supplyChainFactDecodeInput) (sbomv1.Document, error) {
	document, err := factschema.DecodeSBOMDocument(supplyChainSchemaEnvelope(factschema.FactKindSBOMDocument, in.SchemaVersion, in.Payload))
	if err != nil {
		return sbomv1.Document{}, newQueryDecodeError(factschema.FactKindSBOMDocument, in.FactID, err)
	}
	return document, nil
}

// decodeSBOMComponent decodes one sbom.component fact row into the typed
// struct. A missing required field (document_id) yields a self-classifying
// *queryDecodeError.
func decodeSBOMComponent(in supplyChainFactDecodeInput) (sbomv1.Component, error) {
	component, err := factschema.DecodeSBOMComponent(supplyChainSchemaEnvelope(factschema.FactKindSBOMComponent, in.SchemaVersion, in.Payload))
	if err != nil {
		return sbomv1.Component{}, newQueryDecodeError(factschema.FactKindSBOMComponent, in.FactID, err)
	}
	return component, nil
}

// decodePackageRegistryPackageDependency decodes one
// package_registry.package_dependency fact row into the typed struct. A
// missing required field (package_id, version_id, or dependency_package_id)
// yields a self-classifying *queryDecodeError.
func decodePackageRegistryPackageDependency(in supplyChainFactDecodeInput) (packageregistryv1.PackageDependency, error) {
	dependency, err := factschema.DecodePackageRegistryPackageDependency(supplyChainSchemaEnvelope(factschema.FactKindPackageRegistryPackageDependency, in.SchemaVersion, in.Payload))
	if err != nil {
		return packageregistryv1.PackageDependency{}, newQueryDecodeError(factschema.FactKindPackageRegistryPackageDependency, in.FactID, err)
	}
	return dependency, nil
}

// decodeServiceCatalogEntity decodes one service_catalog.entity fact row into
// the typed struct. A missing required field (entity_ref) yields a
// self-classifying *queryDecodeError.
func decodeServiceCatalogEntity(in supplyChainFactDecodeInput) (servicecatalogv1.Entity, error) {
	entity, err := factschema.DecodeServiceCatalogEntity(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogEntity, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.Entity{}, newQueryDecodeError(factschema.FactKindServiceCatalogEntity, in.FactID, err)
	}
	return entity, nil
}

// decodeServiceCatalogOwnership decodes one service_catalog.ownership fact
// row into the typed struct. A missing required field (entity_ref) yields a
// self-classifying *queryDecodeError.
func decodeServiceCatalogOwnership(in supplyChainFactDecodeInput) (servicecatalogv1.Ownership, error) {
	ownership, err := factschema.DecodeServiceCatalogOwnership(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogOwnership, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.Ownership{}, newQueryDecodeError(factschema.FactKindServiceCatalogOwnership, in.FactID, err)
	}
	return ownership, nil
}

// decodeServiceCatalogRepositoryLink decodes one
// service_catalog.repository_link fact row into the typed struct. A missing
// required field (entity_ref) yields a self-classifying *queryDecodeError.
func decodeServiceCatalogRepositoryLink(in supplyChainFactDecodeInput) (servicecatalogv1.RepositoryLink, error) {
	link, err := factschema.DecodeServiceCatalogRepositoryLink(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogRepositoryLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.RepositoryLink{}, newQueryDecodeError(factschema.FactKindServiceCatalogRepositoryLink, in.FactID, err)
	}
	return link, nil
}

// supplyChainDerefFloat64 returns the value a *float64 points at, or 0 when
// it is nil, matching the pre-typing floatVal(0) behavior for a field this
// migration converts from a raw payload lookup to a typed pointer.
func supplyChainDerefFloat64(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// supplyChainComponentEvidence bundles the subset of anchor/component fields
// buildSupplyChainComponentExplanation and buildSupplyChainExplanationAnchors
// (supply_chain_impact_explain_build.go) read across heterogeneous evidence
// facts. Matched is false when fact.FactKind is not one of the source-fact
// kinds this package can typed-decode yet — chiefly the reducer-derived
// correlation/finding/identity kinds pending their own W1 struct per the
// #4784 ADR (reducer_package_consumption_correlation,
// reducer_container_image_identity, reducer_platform_materialization,
// reducer_workload_identity, reducer_service_catalog_correlation, ...) —
// callers keep the pre-existing raw payload read for those kinds unchanged.
// When Matched is true and Err is non-nil, the fact's required identity
// field failed decode (classified input_invalid); callers must skip this
// fact's contribution rather than fall back to a raw read or a zero value.
type supplyChainComponentEvidence struct {
	Matched bool
	Err     error

	Version         string
	PURL            string
	DependencyRange string
	DocumentID      string
	LockfilePath    string
	SubjectDigest   string
	EntityRef       string
	OwnerRef        string
}

// decodeSupplyChainComponentEvidence dispatches one evidence fact to the
// matching factschema Decode* seam by exact FactKind, returning the anchor
// fields buildSupplyChainComponentExplanation/buildSupplyChainExplanationAnchors
// need from that kind. See supplyChainComponentEvidence's doc for the
// Matched/Err contract.
func decodeSupplyChainComponentEvidence(fact SupplyChainImpactEvidenceFact) supplyChainComponentEvidence {
	in := supplyChainFactDecodeInput{FactID: fact.FactID, Payload: fact.Payload}
	switch fact.FactKind {
	case factschema.FactKindSBOMDocument:
		document, err := decodeSBOMDocument(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:       true,
			DocumentID:    document.DocumentID,
			SubjectDigest: workItemDerefString(document.SubjectDigest),
		}
	case factschema.FactKindSBOMComponent:
		component, err := decodeSBOMComponent(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:      true,
			Version:      workItemDerefString(component.Version),
			PURL:         workItemDerefString(component.PURL),
			DocumentID:   component.DocumentID,
			LockfilePath: workItemDerefString(component.LockfilePath),
		}
	case factschema.FactKindPackageRegistryPackageDependency:
		dependency, err := decodePackageRegistryPackageDependency(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:         true,
			Version:         workItemDerefString(dependency.Version),
			DependencyRange: workItemDerefString(dependency.DependencyRange),
		}
	case factschema.FactKindServiceCatalogEntity:
		entity, err := decodeServiceCatalogEntity(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{Matched: true, EntityRef: entity.EntityRef}
	case factschema.FactKindServiceCatalogOwnership:
		ownership, err := decodeServiceCatalogOwnership(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:   true,
			EntityRef: ownership.EntityRef,
			OwnerRef:  workItemDerefString(ownership.OwnerRef),
		}
	case factschema.FactKindServiceCatalogRepositoryLink:
		link, err := decodeServiceCatalogRepositoryLink(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{Matched: true, EntityRef: link.EntityRef}
	default:
		return supplyChainComponentEvidence{}
	}
}
