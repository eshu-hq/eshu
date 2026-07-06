// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "service_catalog" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_servicecatalog.go).
//
// The service_catalog registry family has nine fact kinds and is already
// registered and schema-version-admitted in
// specs/fact-kind-registry.v1.yaml (SchemaVersion "1.0.0",
// AdmissionHook facts.ValidateSchemaVersion) — unlike the codegraph family
// (Wave 4f S1), so this package's migration is additive-only: it fills the
// registry's payload_schema_overrides for the kinds a real consumer decodes,
// with NO admission-behavior change.
//
// This package types the four kinds this wave's consumers actually decode:
// Entity (service_catalog.entity) and Ownership (service_catalog.ownership)
// and RepositoryLink (service_catalog.repository_link), read by the reducer's
// correlation index (go/internal/reducer/service_catalog_correlation_index.go),
// plus OperationalLink (service_catalog.operational_link), read only by a
// raw-SQL JSONB loader in go/internal/query (see operational_link.go doc
// comment). The remaining five kinds — Dependency (service_catalog.dependency),
// APILink (service_catalog.api_link), ScorecardDefinition
// (service_catalog.scorecard_definition), ScorecardResult
// (service_catalog.scorecard_result), and Warning (service_catalog.warning) —
// are intentionally left untyped: they are loaded into the correlation
// handler's fact batch (serviceCatalogCorrelationFactKinds) but no reducer
// index builder or query loader reads their payload fields today. Typing them
// here would create a Decode* the real read path never calls. They migrate
// WITH the surface that reads them, per Contract System v1 §7.
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers carrying
// omitempty, so an absent value decodes to nil and stays distinct from an
// observed empty string — this family's provider/URL/owner fields are
// deliberately optional even where the reducer prefers them, because a
// present-but-blank value is the reducer's own existing "unresolved" or
// "rejected" correlation OUTCOME, never a malformed-payload dead-letter (see
// each struct's doc comment for the specific reducer read site that already
// tolerates the blank).
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
