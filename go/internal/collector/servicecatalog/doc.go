// Package servicecatalog normalizes fixture-backed service-catalog manifests
// into durable service-catalog facts.
//
// The package implements the producer half of the service_catalog_correlation
// domain. The consumer half (projector intent, reducer handler/writer, query
// store, MCP tool, and telemetry counter) already shipped and is provenance
// only with no graph writes. This package turns repo-hosted catalog descriptors
// into the already-defined service_catalog.* fact contract
// (facts.ServiceCatalogSchemaVersionV1).
//
// The package parses Backstage catalog-info.yaml manifests via
<<<<<<< HEAD
// BackstageManifestEnvelopes and OpsLevel opslevel.yml manifests via
// OpsLevelManifestEnvelopes. It does not call hosted Backstage, OpsLevel, or
// Cortex APIs, manage credentials, discover files, write graph state, or import
// the reducer or query packages. Two invariants dominate the design:
=======
// BackstageManifestEnvelopes and Cortex cortex.yaml entity descriptors via
// CortexManifestEnvelopes. Cortex scorecard descriptors are normalized via
// CortexScorecardEnvelopes into carried-only scorecard facts. It does not call
// hosted Backstage, OpsLevel, or Cortex APIs, manage credentials, discover
// files, write graph state, or import the reducer or query packages. Two
// invariants dominate the design:
>>>>>>> b715b446 (feat(servicecatalog): Cortex manifest fact emitter (producer slice, PR-3))
//
//   - Payload-key fidelity: emitted payload keys exactly match what the shipped
//     reducer index reads, so correlation does not silently degrade.
//   - Non-over-admission: the producer never fabricates a repository_id,
//     service_id, or workload_id from catalog text. A catalog name or owner
//     cannot mint canonical repository, service, or workload truth; the reducer
<<<<<<< HEAD
//     decides correlation from active repository facts. OpsLevel references a
//     repository by provider plus a name slug, which is expanded into a
//     derivable URL only for known public git hosts; an unknown or self-hosted
//     provider stays a name-only locator the reducer rejects.
=======
//     decides correlation from active repository facts. Cortex references a
//     repository by a git provider plus a name slug, which is expanded into a
//     derivable URL only for known public git hosts (github, gitlab, bitbucket,
//     azure); an unknown or self-hosted provider stays a name-only locator the
//     reducer rejects.
//
// Cortex scorecard_definition and scorecard_result facts are carried for
// read-surface completeness and forward compatibility: the shipped reducer
// index does not consume them yet, so they never change an entity's correlation
// outcome.
>>>>>>> b715b446 (feat(servicecatalog): Cortex manifest fact emitter (producer slice, PR-3))
//
// Degraded manifest shapes (unsupported descriptor versions, missing entity
// references, duplicate entities, redacted operational links) emit
// service_catalog.warning facts instead of silent drops.
package servicecatalog
