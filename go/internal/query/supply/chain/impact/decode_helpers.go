// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu-sdk/factschema/packageregistry/v1"
	sbomv1 "github.com/eshu-hq/eshu-sdk/factschema/sbom/v1"
	servicecatalogv1 "github.com/eshu-hq/eshu-sdk/factschema/servicecatalog/v1"
	"github.com/eshu-hq/eshu/go/internal/query/decode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"golang.org/x/mod/semver"
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

// explainAffectedRangeEvent is one introduced/fixed/last_affected/limit
// boundary inside an affected_ranges entry decoded off a raw evidence
// payload. It mirrors the OSV event shape the vulnerability collectors emit;
// the query side reads it raw because vulnerability/v1.AffectedPackage's
// typed AffectedRanges field is a different Go shape than this response
// needs (#4795 struct gap, the same reason ParsedAffectedRange stays raw).
type explainAffectedRangeEvent struct {
	introduced   string
	fixed        string
	lastAffected string
	limit        string
}

// decodeExplainAffectedRanges decodes the affected_ranges list off one raw
// evidence payload into per-entry event lists. Entries without events are
// dropped. The entry type is intentionally ignored: collectors emit
// ECOSYSTEM, SEMVER, and GIT ranges and the per-line fix pairing lives in
// the events either way.
func decodeExplainAffectedRanges(payload map[string]any) [][]explainAffectedRangeEvent {
	raw, ok := payload["affected_ranges"].([]any)
	if !ok {
		return nil
	}
	var out [][]explainAffectedRangeEvent
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rawEvents, ok := entry["events"].([]any)
		if !ok {
			continue
		}
		var events []explainAffectedRangeEvent
		for _, rawEvent := range rawEvents {
			event, ok := rawEvent.(map[string]any)
			if !ok {
				continue
			}
			events = append(events, explainAffectedRangeEvent{
				introduced:   querycontract.StringVal(event, "introduced"),
				fixed:        querycontract.StringVal(event, "fixed"),
				lastAffected: querycontract.StringVal(event, "last_affected"),
				limit:        querycontract.StringVal(event, "limit"),
			})
		}
		if len(events) == 0 {
			continue
		}
		out = append(out, events)
	}
	return out
}

// explainRangeContains reports whether one affected_ranges entry's event
// list contains the observed version under OSV range semantics: the version
// must pass any limit gates, reach an introduced boundary at or below
// itself, and neither reach a fixed boundary nor pass a last_affected
// boundary. The second value is false when any compared boundary is not a
// parseable semver-family version. The walk shares the reducer's
// versionRangeContainsDecision event order (limit/introduced/fixed/
// last_affected) but deliberately diverges from the matcher in two ways:
// it ignores the entry kind (collectors emit ECOSYSTEM ranges the
// per-ecosystem matchers skip) and it uses one generic semver-family
// compare instead of the per-ecosystem compares. The headline is
// display-only; matcher input still comes from the reducer scalar.
func explainRangeContains(events []explainAffectedRangeEvent, observed string) (bool, bool) {
	admitted := false
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.limit)
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			admitted = true
			break
		}
		cmp, ok := compareExplainSemverVersions(observed, limit)
		if !ok {
			return false, false
		}
		if cmp < 0 {
			admitted = true
			break
		}
	}
	if hasLimit && !admitted {
		return false, true
	}
	vulnerable := false
	for _, event := range events {
		switch {
		case strings.TrimSpace(event.introduced) != "":
			cmp, ok := compareExplainSemverVersions(observed, event.introduced)
			if !ok {
				return false, false
			}
			if cmp >= 0 {
				vulnerable = true
			}
		case strings.TrimSpace(event.fixed) != "":
			cmp, ok := compareExplainSemverVersions(observed, event.fixed)
			if !ok {
				return false, false
			}
			if cmp >= 0 {
				vulnerable = false
			}
		case strings.TrimSpace(event.lastAffected) != "":
			cmp, ok := compareExplainSemverVersions(observed, event.lastAffected)
			if !ok {
				return false, false
			}
			if cmp > 0 {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

// compareExplainSemverVersions orders two version strings in semver-family
// version order (npm, composer, go, cargo, and the other ecosystems whose
// fixes compare as dotted numerics with prerelease suffixes). It reports
// false when either side is not parseable, so fixed_versions arrives in
// ecosystem order instead of the collector's lexicographic sort (#9430)
// while versions no semver parser reads (rpm EVR, dpkg, apk, maven
// qualifiers) stay unordered rather than guessed.
func compareExplainSemverVersions(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeExplainVersion(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeExplainVersion(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

// normalizeExplainVersion maps one semver-family version onto canonical
// x/mod/semver form: a "v" prefix with a three-part numeric core, keeping a
// prerelease or build suffix when one is present. Short cores pad out
// ("10.0" orders as "v10.0.0") so partial versions still order instead of
// dropping out. Anything without a numeric core, or with a suffix semver
// rejects, stays unparseable.
func normalizeExplainVersion(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	core := trimmed
	suffix := ""
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		suffix = core[index:]
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return "", false
	}
	for _, part := range parts {
		if part == "" {
			return "", false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return "", false
			}
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	normalized := "v" + strings.Join(parts, ".") + suffix
	if !semver.IsValid(normalized) {
		return "", false
	}
	return normalized, true
}
