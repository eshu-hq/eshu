// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// This file holds query-side decode wrappers for the reducer-owned package
// correlation kinds (#5461, part of epic #5455): reducer_package_ownership_correlation,
// reducer_package_consumption_correlation, and reducer_package_publication_correlation.
// package_registry_correlations.go is the only read site for these three
// kinds. Each wrapper wraps the matching sdk/go/factschema Decode* seam and,
// on a classified *factschema.DecodeError (a missing/null required identity
// field, i.e. package_id), returns a *queryDecodeError (defined in
// factschema_decode_workitem.go, reused here rather than forked) so the
// caller drops that fact's contribution instead of fabricating a
// zero-valued row — matching the #4784 ADR's "missing required fields
// dead-letter, they never silently zero out" rule.
//
// Before this change decodePackageRegistryCorrelationRow read the raw
// payload map with StringVal/BoolVal/IntVal/StringSliceVal and never
// distinguished a missing package_id from an intentionally empty one; the
// switch to the typed seam is deliberately output-preserving for every real
// fact (the writer in go/internal/reducer/package_correlation_writer.go
// always stamps package_id) while now classifying the previously-unchecked
// missing-identity case instead of returning a wrong-looking all-empty row.

// packageCorrelationDecodeInput carries one scanned package correlation fact
// row into a decode wrapper. Bundling FactID, SchemaVersion, and Payload into
// a single parameter keeps each wrapper's one-argument shape, matching the
// payload-usage manifest gate's seam parser convention (see
// factschema_decode_workitem.go's workItemDecodeInput).
type packageCorrelationDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// packageCorrelationSchemaEnvelope adapts one scanned package correlation
// fact row into the contracts-module factschema.Envelope the Decode* seam
// accepts. An empty schemaVersion normalizes to queryDefaultSchemaMajorVersion
// (factschema_decode_workitem.go), matching the version-less legacy default;
// every in-tree package correlation writer stamps a concrete major-1 version
// (facts.ReducerDerivedSchemaVersionV1), so the empty case is defensive
// rather than the production path. A present but unsupported major still
// dead-letters through the Decode* seam's default branch instead of being
// decoded as v1.
func packageCorrelationSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = queryDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeReducerPackageOwnershipCorrelation decodes one
// reducer_package_ownership_correlation fact row into the typed struct. A
// missing required field (package_id) yields a self-classifying
// *queryDecodeError.
func decodeReducerPackageOwnershipCorrelation(in packageCorrelationDecodeInput) (reducerderivedv1.PackageOwnershipCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageOwnershipCorrelation(packageCorrelationSchemaEnvelope(factschema.FactKindReducerPackageOwnershipCorrelation, in.SchemaVersion, in.Payload))
	if err != nil {
		return reducerderivedv1.PackageOwnershipCorrelation{}, newQueryDecodeError(factschema.FactKindReducerPackageOwnershipCorrelation, in.FactID, err)
	}
	return correlation, nil
}

// decodeReducerPackageConsumptionCorrelation decodes one
// reducer_package_consumption_correlation fact row into the typed struct. A
// missing required field (package_id) yields a self-classifying
// *queryDecodeError.
func decodeReducerPackageConsumptionCorrelation(in packageCorrelationDecodeInput) (reducerderivedv1.PackageConsumptionCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackageConsumptionCorrelation(packageCorrelationSchemaEnvelope(factschema.FactKindReducerPackageConsumptionCorrelation, in.SchemaVersion, in.Payload))
	if err != nil {
		return reducerderivedv1.PackageConsumptionCorrelation{}, newQueryDecodeError(factschema.FactKindReducerPackageConsumptionCorrelation, in.FactID, err)
	}
	return correlation, nil
}

// decodeReducerPackagePublicationCorrelation decodes one
// reducer_package_publication_correlation fact row into the typed struct. A
// missing required field (package_id) yields a self-classifying
// *queryDecodeError.
func decodeReducerPackagePublicationCorrelation(in packageCorrelationDecodeInput) (reducerderivedv1.PackagePublicationCorrelation, error) {
	correlation, err := factschema.DecodeReducerPackagePublicationCorrelation(packageCorrelationSchemaEnvelope(factschema.FactKindReducerPackagePublicationCorrelation, in.SchemaVersion, in.Payload))
	if err != nil {
		return reducerderivedv1.PackagePublicationCorrelation{}, newQueryDecodeError(factschema.FactKindReducerPackagePublicationCorrelation, in.FactID, err)
	}
	return correlation, nil
}

// packageCorrelationDerefInt returns the value a *int points at, or 0 when it
// is nil, matching the pre-typing IntVal(payload, key) zero-value behavior for
// a field this migration converts from a raw payload lookup to a typed
// pointer (CanonicalWrites on all three package correlation kinds).
func packageCorrelationDerefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
