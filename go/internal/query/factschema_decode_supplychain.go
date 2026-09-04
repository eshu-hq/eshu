// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

// This file holds query-side decode wrappers for the source-fact kinds that
// feed the supply-chain impact-explanation and impact-path read models
// (#4795 W2b). The vulnerability wrappers that fed the advisory-evidence
// read model moved with it to internal/query/supplychain/advisory (#6060
// lane A). Each wraps the matching
// sdk/go/factschema Decode* seam and, on a classified *factschema.DecodeError
// (a missing/null required identity field), returns a *queryDecodeError
// (defined in factschema_decode_workitem.go, reused here rather than
// forked) so the caller can drop the fact's contribution instead of
// fabricating a zero-valued row.
//
// Governance note (#4784 ADR, docs/internal/design/4784-reducer-derived-fact-governance.md):
// these wrappers cover SOURCE-FACT kinds only. The supply-chain read models
// also read several reducer-derived kinds (reducer_supply_chain_impact_finding,
// reducer_sbom_attestation_attachment, reducer_workload_identity,
// reducer_service_catalog_correlation, reducer_container_image_identity, ...).
// None of those have a landed sdk/go/factschema struct yet — the ADR requires
// W1 struct authorship before any W2 read site can typed-decode them — so
// every read of those kinds stays on the pre-existing raw payload path,
// marked inline with the fact kind that blocks it.
//
// reducer_package_ownership_correlation, reducer_package_consumption_correlation,
// and reducer_package_publication_correlation are NO LONGER in that ungoverned
// set (#5461): their sdk/go/factschema/reducerderived/v1 structs, generated
// JSON Schemas, and typed reducer writer landed, and the query-side read site
// (package_registry_correlations.go) now decodes them through the typed seam
// in factschema_decode_package_correlations.go.
//
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
// into the contracts-module factschema.Envelope the Decode* seam accepts. Each
// read-model row now carries the fact's persisted schema_version (selected and
// scanned by the advisory-evidence and impact-explain stores, then threaded
// into supplyChainFactDecodeInput), so a non-1.x (future/unsupported major)
// fact dead-letters through the Decode* seam's default branch instead of being
// decoded as v1. An empty schemaVersion normalizes to the current major-1
// schema version, matching the version-less legacy default the workitem seam
// uses; every in-tree vulnerability/SBOM/service-catalog/package-registry
// source-fact emitter stamps a concrete major-1 version, so the empty case is
// defensive rather than the production path.
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
	in := supplyChainFactDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload}
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
