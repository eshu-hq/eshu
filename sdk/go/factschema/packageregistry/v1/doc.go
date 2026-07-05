// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "package_registry" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_packageregistry.go).
//
// Nine fact kinds live here. Three are CONSUMED today by the projector's
// source-local canonical extractor
// (go/internal/projector/package_registry_canonical.go) and decode through the
// seam on the read path:
//
//   - Package             (package_registry.package)
//   - PackageVersion       (package_registry.package_version)
//   - PackageDependency    (package_registry.package_dependency)
//
// Six are TYPED-BUT-NOT-YET-CONSUMED by any decode-seam read path today, so
// this wave ships their struct, schema, and fixture pack without converting a
// decode site, adding an input_invalid regression test, or benchmarking a read
// path (there is none to benchmark), matching how the terraform_state family
// typed Candidate/ProviderBinding/Warning ahead of their consumer
// (terraformstate/v1/doc.go):
//
//   - SourceHint           (package_registry.source_hint)
//   - PackageArtifact      (package_registry.package_artifact)
//   - VulnerabilityHint    (package_registry.vulnerability_hint)
//   - RegistryEvent        (package_registry.registry_event)
//   - RepositoryHosting    (package_registry.repository_hosting)
//   - Warning              (package_registry.warning)
//
// SourceHint's payload IS read today, but only by the reducer's
// package_source_correlation domain (go/internal/reducer/package_source_correlation.go
// extractPackageSourceHints, via raw payloadStr calls) — a separate reducer
// family this wave does not touch (Contract System v1 Wave 4c is scoped to the
// package_registry PROJECTOR family). The projector's own
// package_source_correlation_intents.go reads only envelope.FactKind to route a
// reducer intent, never a payload field, so SourceHint has no projector
// decode-site consumer today either. It is typed here so the contract is ready
// the moment a projector or reducer conversion lands; wiring the reducer's own
// decode site is out of scope for this wave.
//
// Three of the typed-but-not-yet-consumed kinds' payload fields ARE read by a
// raw-SQL-JSONB loader that the #4573 payload-usage manifest gate cannot see
// (it scans decode-seam call sites only):
// go/internal/storage/postgres/facts_active_supply_chain_impact.go reads
// VulnerabilityHint's package_id, and
// go/internal/storage/postgres/status_registry.go reads Warning's ecosystem
// and warning_code. Those fields MUST still be declared in this wave's
// schemas; go/internal/storage/postgres/package_registry_sql_schema_lockstep_test.go
// locks that coverage so a dropped field fails the build instead of silently
// breaking the SQL read, mirroring
// TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared
// (incident_routing_sql_schema_lockstep_test.go).
//
// Required vs optional. Each struct's required fields are non-pointer with no
// omitempty tag; the decode seam rejects a payload that omits one, or supplies
// an explicit JSON null for one, with a classified ClassificationInputInvalid
// error naming the field, never a zero-value struct. Optional fields are
// pointers, slices, or maps carrying omitempty, so an absent value decodes to
// nil and stays distinct from an observed zero.
//
// The required set for each CONSUMED kind is exactly the identity/join key
// whose ABSENCE produces a broken or empty graph identity in the projector
// today — the accuracy fix Contract System v1 exists to protect:
//
//   - Package.PackageID              — the package node uid; an absent
//     package_id drops the row today (package_registry_canonical.go
//     packageRegistryPackageRow) with no operator signal; an absent value must
//     dead-letter instead.
//   - PackageVersion.PackageID, .VersionID, and .Version — the version node's
//     uid and its owning package join key; any one absent drops the row today
//     (packageRegistryVersionRow).
//   - PackageDependency.PackageID, .VersionID, and .DependencyPackageID — the
//     dependency edge's three join keys; any one absent drops the row today
//     (packageRegistryDependencyRow). The edge's own uid additionally requires
//     a non-blank StableFactKey, enforced on the envelope, not the payload.
//
// A present-but-empty required value (an empty string) is a VALID decode, not a
// dead-letter, matching the pre-typing projector behavior where an empty
// identity value was simply dropped as non-materializable rather than errored.
// Only an ABSENT key (or explicit null) dead-letters.
//
// The reducer/projector decodes only the latest struct for each kind. Version
// shims for an older schema major live in the parent factschema package's
// decode seam (decodeLatestMajor in decode.go), never in this package or in
// the projector's canonical extractor.
package v1
