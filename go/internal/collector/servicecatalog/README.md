# Service Catalog Manifest Collector

## Purpose

`internal/collector/servicecatalog` owns fixture-backed service-catalog manifest
normalization for the `service_catalog` collector family. It turns repo-hosted
catalog descriptors (Backstage `catalog-info.yaml`, and later OpsLevel/Cortex)
into observed-confidence `service_catalog.*` fact envelopes that the already
shipped `service_catalog_correlation` reducer domain consumes.

This package is the **producer** side. The consumer half — projector intent
(`buildServiceCatalogCorrelationReducerIntent`), reducer handler/writer
(`ServiceCatalogCorrelationHandler`), query store
(`ListServiceCatalogCorrelations`), the `list_service_catalog_correlations` MCP
tool, and the `ServiceCatalogCorrelations` telemetry counter — already ships and
is **provenance only with zero graph writes**. This package adds no fact kinds,
no schema change, and no graph writes.

It intentionally does not implement hosted catalog API polling, credentials,
filesystem discovery, graph writes, or canonical service/workload promotion.

## Fixture-to-fact flow

```mermaid
flowchart LR
    Manifest["offline catalog-info.yaml"]
    Context["FixtureContext"]
    Normalize["BackstageManifestEnvelopes"]
    Facts["entity, ownership, repository_link, dependency, operational_link facts"]
    Warnings["service_catalog.warning facts"]
    Reducer["service_catalog_correlation reducer (shipped, provenance-only)"]

    Manifest --> Normalize
    Context --> Normalize
    Normalize --> Facts
    Normalize --> Warnings
    Facts --> Reducer
    Warnings --> Reducer
```

## Exported surface

- `CollectorKind` — durable collector family name: `service_catalog`.
- `ProviderBackstage` — provider value used for Backstage facts.
- `FixtureContext` — scope, generation, collector instance, fencing token,
  observed time, and repo-relative source URI copied into emitted envelopes.
- `BackstageManifestEnvelopes` — parses one offline Backstage manifest (possibly
  multi-document) and returns service-catalog fact envelopes.

## Payload-key fidelity (the contract)

The shipped reducer index (`reducer/service_catalog_correlation_index.go`) reads
specific payload keys. Emitting a different key silently collapses correlation
to `unresolved`/`rejected`. The producer honors these keys:

- `service_catalog.entity`: `provider`, `entity_ref`, `entity_type`,
  `display_name`, `lifecycle`, `tier`. `service_id` and `workload_id` are
  **deliberately absent**.
- `service_catalog.ownership`: `provider`, `entity_ref`, `owner_ref`.
- `service_catalog.repository_link`: `provider`, `entity_ref`, and the declared
  locator — `repository_url` (verbatim declared URL) or `repository_name`
  (name-only slug). `repository_id` is **never fabricated**.
- `service_catalog.warning`: `provider`, `reason`, `message` (redacted), and
  `entity_ref` when known.

The declared repository URL is emitted verbatim into `repository_url`; the
reducer applies its own git-URL canonicalization, which preserves the
exact-vs-derived distinction (for example a `.git` suffix yields `derived`, an
identical URL yields `exact`). The producer does not pre-canonicalize into
`normalized_url`, because the reducer re-canonicalizes the value it reads and a
bare host/path key would fail re-canonicalization.

`dependency` facts are emitted for read-surface completeness and forward
compatibility; the reducer index does not consume them yet, so they must not
change an entity's correlation outcome.

## Invariants

- Fixture-backed until a hosted runtime slice is explicitly opened.
- No HTTP clients, credentials, filesystem discovery, graph writes, reducer
  imports, or query imports in production code (the reducer is imported in test
  code only, for the round-trip contract test).
- Every emitted fact carries `schema_version = 1.0.0`
  (`facts.ServiceCatalogSchemaVersionV1`); a mismatch is silently rejected at the
  projector.
- `source_confidence = observed` because manifests are read directly from a repo
  artifact.
- Catalog names and owners never mint `repository_id`, `service_id`, or
  `workload_id`. Correlation is the reducer's job.
- Token-bearing or query-string URLs are stripped before emission; redacted
  operational links emit a warning instead of dropping the entity.
- Degraded documents (unsupported version, missing name, duplicate entity) emit
  warnings, never silent drops. Multi-document manifests are parsed per
  document so one bad document does not abort the file.

## Telemetry

This slice emits no metrics or spans; it is a deterministic offline normalizer.
Producer counters (`service_catalog_facts_emitted_total`,
`service_catalog_manifest_warnings_total`,
`service_catalog_manifests_parsed_total`) and a `servicecatalog.collect` span
are deferred to the telemetry + Compose-proof slice (design memo PR-4). The
shipped downstream `ServiceCatalogCorrelations{outcome}` reducer counter remains
the diagnosis chain for "facts emitted but zero exact correlations."

No-Regression Evidence: fixture normalization is covered by
`go test ./internal/collector/servicecatalog -count=1`, which exercises typed
contract emission, the reducer round-trip reaching exact/derived/unresolved/
rejected/stale/ambiguous outcomes, blank `service_id`/`workload_id`/
`repository_id` assertions, warning emission for unsupported version, missing
ref, duplicate entity, and redacted link, empty input, and idempotent
re-emission — without graph writes or queue work.

No-Observability-Change: this package mounts no runtime and adds no metrics,
spans, or logs. The later telemetry slice must add fact-emission, warning, and
parse-result signals before live collection is enabled.
