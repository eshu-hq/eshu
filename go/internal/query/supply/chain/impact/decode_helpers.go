// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/decode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

func boolPointerVal(payload map[string]any, key string) *bool {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		parsed := strings.EqualFold(trimmed, "true")
		return &parsed
	default:
		return nil
	}
}

// This file holds query-side decode wrappers for the source-fact kinds that
// feed the moved supply-chain impact-explanation read model (#4795 W2b).
// Copied from root package query's factschema_decode_supplychain.go (#6060
// lane A): the moved decodeSupplyChainComponentEvidence below calls them and
// this package must not import root, so each wrapper, the input bundle, the
// envelope adapter, the evidence struct, and the dispatching seam live here
// and MUST stay behavior-identical to their root sources. Root's
// factschema_decode_supplychain.go keeps serving the sibling supply-chain
// read models that stay behind.
//
// Each wrapper wraps the matching sdk/go/factschema Decode* seam and, on a
// classified *factschema.DecodeError (a missing/null required identity
// field), returns a *decode.Error via decode.New, the same classified
// failure root's own supply-chain decoders return. Callers drop the fact's
// contribution instead of fabricating a zero-valued row.

// supplyChainFactDecodeInput carries one scanned evidence-fact row into a
// decode wrapper. Copied from root package query's
// factschema_decode_supplychain.go; see the file doc for why it is copied.
type supplyChainFactDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// supplyChainSchemaEnvelope adapts one scanned supply-chain evidence fact
// row into the contracts-module factschema.Envelope the Decode* seam
// accepts. Copied from root package query's
// factschema_decode_supplychain.go; an empty schemaVersion normalizes to
// decode.DefaultSchemaMajorVersion, matching the version-less legacy
// default. A present but unsupported major still dead-letters through the
// Decode* seam's default branch instead of being decoded as v1.
func supplyChainSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = decode.DefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeSBOMDocument decodes one sbom.document fact row into the typed
// struct. A missing required field (document_id) yields a self-classifying
// *decode.Error. Copied from root package query's
// factschema_decode_supplychain.go.
func decodeSBOMDocument(in supplyChainFactDecodeInput) (sbomv1.Document, error) {
	document, err := factschema.DecodeSBOMDocument(supplyChainSchemaEnvelope(factschema.FactKindSBOMDocument, in.SchemaVersion, in.Payload))
	if err != nil {
		return sbomv1.Document{}, decode.New(factschema.FactKindSBOMDocument, in.FactID, err)
	}
	return document, nil
}

// decodeSBOMComponent decodes one sbom.component fact row into the typed
// struct. A missing required field (document_id) yields a self-classifying
// *decode.Error. Copied from root package query's
// factschema_decode_supplychain.go.
func decodeSBOMComponent(in supplyChainFactDecodeInput) (sbomv1.Component, error) {
	component, err := factschema.DecodeSBOMComponent(supplyChainSchemaEnvelope(factschema.FactKindSBOMComponent, in.SchemaVersion, in.Payload))
	if err != nil {
		return sbomv1.Component{}, decode.New(factschema.FactKindSBOMComponent, in.FactID, err)
	}
	return component, nil
}

// decodePackageRegistryPackageDependency decodes one
// package_registry.package_dependency fact row into the typed struct. A
// missing required field (package_id, version_id, or dependency_package_id)
// yields a self-classifying *decode.Error. Copied from root package
// query's factschema_decode_supplychain.go.
func decodePackageRegistryPackageDependency(in supplyChainFactDecodeInput) (packageregistryv1.PackageDependency, error) {
	dependency, err := factschema.DecodePackageRegistryPackageDependency(supplyChainSchemaEnvelope(factschema.FactKindPackageRegistryPackageDependency, in.SchemaVersion, in.Payload))
	if err != nil {
		return packageregistryv1.PackageDependency{}, decode.New(factschema.FactKindPackageRegistryPackageDependency, in.FactID, err)
	}
	return dependency, nil
}

// decodeServiceCatalogEntity decodes one service_catalog.entity fact row
// into the typed struct. A missing required field (entity_ref) yields a
// self-classifying *decode.Error. Copied from root package query's
// factschema_decode_supplychain.go.
func decodeServiceCatalogEntity(in supplyChainFactDecodeInput) (servicecatalogv1.Entity, error) {
	entity, err := factschema.DecodeServiceCatalogEntity(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogEntity, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.Entity{}, decode.New(factschema.FactKindServiceCatalogEntity, in.FactID, err)
	}
	return entity, nil
}

// decodeServiceCatalogOwnership decodes one service_catalog.ownership fact
// row into the typed struct. A missing required field (entity_ref) yields a
// self-classifying *decode.Error. Copied from root package query's
// factschema_decode_supplychain.go.
func decodeServiceCatalogOwnership(in supplyChainFactDecodeInput) (servicecatalogv1.Ownership, error) {
	ownership, err := factschema.DecodeServiceCatalogOwnership(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogOwnership, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.Ownership{}, decode.New(factschema.FactKindServiceCatalogOwnership, in.FactID, err)
	}
	return ownership, nil
}

// decodeServiceCatalogRepositoryLink decodes one
// service_catalog.repository_link fact row into the typed struct. A missing
// required field (entity_ref) yields a self-classifying *decode.Error.
// Copied from root package query's factschema_decode_supplychain.go.
func decodeServiceCatalogRepositoryLink(in supplyChainFactDecodeInput) (servicecatalogv1.RepositoryLink, error) {
	link, err := factschema.DecodeServiceCatalogRepositoryLink(supplyChainSchemaEnvelope(factschema.FactKindServiceCatalogRepositoryLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return servicecatalogv1.RepositoryLink{}, decode.New(factschema.FactKindServiceCatalogRepositoryLink, in.FactID, err)
	}
	return link, nil
}

// supplyChainComponentEvidence bundles the subset of anchor/component
// fields buildSupplyChainComponentExplanation and
// buildSupplyChainExplanationAnchors (explain_build.go)
// read across heterogeneous evidence facts. Matched is false when
// fact.FactKind is not one of the source-fact kinds this package can
// typed-decode yet -- chiefly the reducer-derived correlation/finding/
// identity kinds pending their own W1 struct per the #4784 ADR
// (reducer_package_consumption_correlation,
// reducer_container_image_identity, reducer_platform_materialization,
// reducer_workload_identity, reducer_service_catalog_correlation, ...) --
// callers keep the pre-existing raw payload read for those kinds unchanged.
// When Matched is true and Err is non-nil, the fact's required identity
// field failed decode (classified input_invalid); callers must skip this
// fact's contribution rather than fall back to a raw read or a zero value.
// Copied from root package query's factschema_decode_supplychain.go.
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
// Matched/Err contract. Copied from root package query's
// factschema_decode_supplychain.go.
func decodeSupplyChainComponentEvidence(fact EvidenceFact) supplyChainComponentEvidence {
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
			SubjectDigest: decode.DerefString(document.SubjectDigest),
		}
	case factschema.FactKindSBOMComponent:
		component, err := decodeSBOMComponent(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:      true,
			Version:      decode.DerefString(component.Version),
			PURL:         decode.DerefString(component.PURL),
			DocumentID:   component.DocumentID,
			LockfilePath: decode.DerefString(component.LockfilePath),
		}
	case factschema.FactKindPackageRegistryPackageDependency:
		dependency, err := decodePackageRegistryPackageDependency(in)
		if err != nil {
			return supplyChainComponentEvidence{Matched: true, Err: err}
		}
		return supplyChainComponentEvidence{
			Matched:         true,
			Version:         decode.DerefString(dependency.Version),
			DependencyRange: decode.DerefString(dependency.DependencyRange),
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
			OwnerRef:  decode.DerefString(ownership.OwnerRef),
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
