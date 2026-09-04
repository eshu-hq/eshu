// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"github.com/eshu-hq/eshu/go/internal/query/querydecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

// This file holds query-side decode wrappers for the source-fact kinds that
// feed the advisory-evidence read model (#4795 W2b). It moved out of root
// package query with the rest of the advisory family (#6060 lane A); root's
// factschema_decode_supplychain.go keeps serving the sibling supply-chain
// read models (impact explanation and detail surfaces) that stay behind.
//
// Each wrapper wraps the matching sdk/go/factschema Decode* seam and, on a
// classified *factschema.DecodeError (a missing/null required identity
// field), returns a *querydecode.Error (the leaf that lets a handler family
// classify a decode failure without importing root package query) so the
// caller drops the fact's contribution instead of fabricating a zero-valued
// row. The shape mirrors packagereg's
// factschema_decode_package_correlations.go, the landed #6060 precedent for
// a family that carries its own decode seam.
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
// struct-gap comment in supply_chain_advisory_evidence_model.go) alongside
// the fields that do decode losslessly.
// vulnerability.affected_product's typed struct is missing six of the nine
// fields the response model reads (VersionStart/EndIncluding/Excluding,
// SourceConfigurationOperator/Negate, SourceNodeOperator/Negate), so that
// read site stays on the raw path rather than adding a wrapper this package
// could not call losslessly.

// supplyChainFactDecodeInput carries one scanned evidence-fact row into a
// decode wrapper. Bundling FactID, SchemaVersion, and Payload into a single
// parameter keeps each wrapper's one-argument shape, matching the
// payload-usage manifest gate's seam parser convention (see root package
// query's factschema_decode_workitem.go's workItemDecodeInput). The name
// keeps root's: this family is the supply-chain advisory leaf.
type supplyChainFactDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// supplyChainDefaultSchemaMajorVersion is the schema version this file
// assumes when a row carries none, matching root package query's
// queryDefaultSchemaMajorVersion (factschema_decode_workitem.go). It is a
// major-1 version because every in-tree vulnerability source-fact emitter
// stamps a concrete major-1 version; the Decode seam dispatches on the major
// component only. Kept as this family's own copy rather than an import: the
// root constant is unexported and this trivial literal has no shared-drift
// risk (same rationale as packagereg's
// packageCorrelationDefaultSchemaMajorVersion).
const supplyChainDefaultSchemaMajorVersion = "1.0.0"

// supplyChainSchemaEnvelope adapts one scanned advisory evidence fact row
// into the contracts-module factschema.Envelope the Decode* seam accepts. An
// empty schemaVersion normalizes to supplyChainDefaultSchemaMajorVersion,
// matching the version-less legacy default; every in-tree
// vulnerability source-fact emitter stamps a concrete major-1 version, so
// the empty case is defensive rather than the production path. A present but
// unsupported major still dead-letters through the Decode* seam's default
// branch instead of being decoded as v1.
func supplyChainSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = supplyChainDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeVulnerabilityCVE decodes one vulnerability.cve fact row into the
// typed struct. A missing required field (advisory_id) yields a
// self-classifying *querydecode.Error. See this file's struct-completeness
// note: callers must still read aliases/severity/cvss_v2/cvss_v3/cvss_v4/
// cvss_metrics/cwes from the raw payload — the typed struct does not
// declare them yet.
func decodeVulnerabilityCVE(in supplyChainFactDecodeInput) (vulnerabilityv1.CVE, error) {
	cve, err := factschema.DecodeVulnerabilityCVE(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityCVE, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.CVE{}, querydecode.New(factschema.FactKindVulnerabilityCVE, in.FactID, err)
	}
	return cve, nil
}

// decodeVulnerabilityAffectedPackage decodes one vulnerability.affected_package
// fact row into the typed struct. A missing required field (advisory_id)
// yields a self-classifying *querydecode.Error. See this file's
// struct-completeness note: callers must still read
// parsed_affected_range/affected_ranges from the raw payload.
func decodeVulnerabilityAffectedPackage(in supplyChainFactDecodeInput) (vulnerabilityv1.AffectedPackage, error) {
	affected, err := factschema.DecodeVulnerabilityAffectedPackage(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityAffectedPackage, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.AffectedPackage{}, querydecode.New(factschema.FactKindVulnerabilityAffectedPackage, in.FactID, err)
	}
	return affected, nil
}

// decodeVulnerabilityEPSSScore decodes one vulnerability.epss_score fact row
// into the typed struct. A missing required field (cve_id) yields a
// self-classifying *querydecode.Error. This kind decodes losslessly: every
// field the query-side AdvisoryEPSSObservation reads (probability,
// percentile, score_date) is declared on vulnerabilityv1.EPSSScore.
func decodeVulnerabilityEPSSScore(in supplyChainFactDecodeInput) (vulnerabilityv1.EPSSScore, error) {
	score, err := factschema.DecodeVulnerabilityEPSSScore(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityEPSSScore, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.EPSSScore{}, querydecode.New(factschema.FactKindVulnerabilityEPSSScore, in.FactID, err)
	}
	return score, nil
}

// decodeVulnerabilityKnownExploited decodes one vulnerability.known_exploited
// fact row into the typed struct. A missing required field (cve_id) yields a
// self-classifying *querydecode.Error. This kind decodes losslessly: every
// field the query-side AdvisoryKEVObservation reads is declared on
// vulnerabilityv1.KnownExploited.
func decodeVulnerabilityKnownExploited(in supplyChainFactDecodeInput) (vulnerabilityv1.KnownExploited, error) {
	kev, err := factschema.DecodeVulnerabilityKnownExploited(supplyChainSchemaEnvelope(factschema.FactKindVulnerabilityKnownExploited, in.SchemaVersion, in.Payload))
	if err != nil {
		return vulnerabilityv1.KnownExploited{}, querydecode.New(factschema.FactKindVulnerabilityKnownExploited, in.FactID, err)
	}
	return kev, nil
}
