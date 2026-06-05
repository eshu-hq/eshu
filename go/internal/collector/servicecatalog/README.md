# Service Catalog Manifest Collector

## Purpose

`internal/collector/servicecatalog` owns service-catalog manifest normalization
for the `service_catalog` collector family. It turns repo-hosted catalog
descriptors (Backstage `catalog-info.yaml`, OpsLevel `opslevel.yml`, and Cortex
`cortex.yaml`) into observed-confidence `service_catalog.*` fact envelopes that
the already shipped `service_catalog_correlation` reducer domain consumes.

This package is the pure **normalizer** side. The Git collector calls it while
streaming recognized repo-hosted descriptor files, and fixture tests can call it
directly. The consumer half — projector intent
(`buildServiceCatalogCorrelationReducerIntent`), reducer handler/writer
(`ServiceCatalogCorrelationHandler`), query store
(`ListServiceCatalogCorrelations`), the `list_service_catalog_correlations` MCP
tool, and the `ServiceCatalogCorrelations` telemetry counter — already ships and
is **provenance only with zero graph writes**. This package adds no fact kinds,
no schema change, and no graph writes.

It intentionally does not implement hosted catalog API polling, credentials,
graph writes, or canonical service/workload promotion. Repo-hosted manifest
selection lives in the Git collector stream path; hosted Backstage/OpsLevel/
Cortex API collection remains a separate future runtime slice.

## Fixture-to-fact flow

```mermaid
flowchart LR
    Manifest["offline catalog-info.yaml / opslevel.yml / cortex.yaml"]
    Context["FixtureContext"]
    Normalize["BackstageManifestEnvelopes / OpsLevelManifestEnvelopes / CortexManifestEnvelopes"]
    Facts["entity, ownership, repository_link, dependency, operational_link facts"]
    Scorecards["scorecard_definition / scorecard_result facts (Cortex, carried-only)"]
    Warnings["service_catalog.warning facts"]
    Reducer["service_catalog_correlation reducer (shipped, provenance-only)"]

    Manifest --> Normalize
    Context --> Normalize
    Normalize --> Facts
    Normalize --> Scorecards
    Normalize --> Warnings
    Facts --> Reducer
    Scorecards --> Reducer
    Warnings --> Reducer
```

Every provider entry point normalizes into the same provider-agnostic
`catalogEntity` shape and reuses the shared envelope builders in
`facts_builder.go`, so payload-key fidelity is identical across providers.

## Exported surface

- `CollectorKind` — durable collector family name: `service_catalog`.
- `ProviderBackstage` — provider value used for Backstage facts.
- `ProviderOpsLevel` — provider value used for OpsLevel facts.
- `ProviderOpsLevelNamespace` — entity-ref namespace segment for OpsLevel
  components (`opslevel`), keeping OpsLevel refs distinct from other providers'
  refs in the shared reducer entity key.
- `ProviderCortex` — provider value used for Cortex facts.
- `ProviderCortexNamespace` — entity-ref namespace segment for Cortex entities
  (`cortex`), keeping Cortex refs distinct from other providers' refs in the
  shared reducer entity key (Cortex tags are unique per instance, not across
  providers).
- `FixtureContext` — scope, generation, collector instance, fencing token,
  observed time, and repo-relative source URI copied into emitted envelopes.
- `BackstageManifestEnvelopes` — parses one offline Backstage manifest (possibly
  multi-document) and returns service-catalog fact envelopes.
- `OpsLevelManifestEnvelopes` — parses one offline OpsLevel `opslevel.yml`
  manifest (possibly multi-document) and returns service-catalog fact envelopes.
- `CortexManifestEnvelopes` — parses one offline Cortex `cortex.yaml` entity
  descriptor (possibly multi-document OpenAPI) and returns service-catalog fact
  envelopes.
- `CortexScorecardEnvelopes` — parses one offline Cortex scorecard descriptor
  into carried-only `scorecard_definition` (per rule) and `scorecard_result`
  (per entity) facts.

### OpsLevel repository resolution

OpsLevel references a repository by `provider` + a `name` slug
(`Org/Group/repo`), never a full URL. The producer expands a known public git
provider (`github`, `gitlab`, `bitbucket`, `azure_devops`) plus the slug into a
derivable `repository_url` (for example `github` + `eshu-hq/checkout-api` ->
`https://github.com/eshu-hq/checkout-api`). An unknown or self-hosted provider,
or a slug that is not a path, stays a name-only `repository_name` locator, which
the reducer rejects because a bare name cannot prove repository ownership. The
producer never fabricates a `repository_id`. The OpsLevel block is always a
component; its free-form `type` (service, database, ...) flows into
`entity_type`, while the entity ref is anchored on the first declared alias (or
the slugified name) under the `opslevel` namespace.

### Cortex repository resolution

A Cortex entity descriptor is an OpenAPI 3 spec whose `info.x-cortex-tag` is the
globally-unique anchor; the producer mints the entity ref as
`<x-cortex-type>:cortex/<x-cortex-tag>`. Cortex references a repository under
`info.x-cortex-git` by a git provider plus a name slug, never a full URL. The
producer expands a known public provider (`github`, `gitlab`, `bitbucket` use
`<org>/<repo>`; `azure` uses separate `project` + `repository`) into a derivable
`repository_url`. An unknown or self-hosted provider, or a slug with no path,
stays a name-only `repository_name` locator the reducer rejects, because a bare
name cannot prove repository ownership. The producer never fabricates a
`repository_id`. When a descriptor declares more than one provider, the locator
is resolved in sorted provider order so stable fact ids stay deterministic, and
the first provider that yields a real URL wins so an unexpandable provider (for
example an Azure entry missing its project) never shadows a resolvable one.

Cortex dependencies (`info.x-cortex-dependency`) carry only a target tag; the
producer anchors each `depends_on_ref` in the same `service:cortex/<tag>` ref
shape it mints for an untyped entity, so a future reducer join can correlate a
dependency to the emitted entity by `provider` plus ref.

### Cortex scorecards (carried-only)

`CortexScorecardEnvelopes` emits one `service_catalog.scorecard_definition` fact
per rule (`scorecard_tag`, `rule_identifier`, `expression`, `level`) and one
`service_catalog.scorecard_result` fact per declared per-entity score
(`entity_ref`, `scorecard_tag`, `level`, `score`). Both anchor on `provider`
plus the scorecard/entity identity. The shipped reducer index loads these kinds
(they pass the schema-version gate) but does not classify them, so they must not
change any entity's correlation outcome; a dedicated test asserts the outcome is
unchanged when scorecards are added.

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

`dependency`, `scorecard_definition`, and `scorecard_result` facts are emitted
for read-surface completeness and forward compatibility; the reducer index does
not consume them yet, so they must not change an entity's correlation outcome.
The carried scorecard keys are:

- `service_catalog.scorecard_definition`: `provider`, `scorecard_tag`,
  `scorecard_name`, `rule_identifier`, `rule_title`, `expression`, `level`.
- `service_catalog.scorecard_result`: `provider`, `entity_ref`, `scorecard_tag`,
  `level`, and `score` when declared.

Cortex has no native `lifecycle` field in the entity descriptor, so the Cortex
producer leaves `lifecycle` blank rather than inventing one from a group label.

## Invariants

- Pure-normalizer until a hosted runtime slice is explicitly opened.
- No HTTP clients, credentials, graph writes, reducer imports, or query imports
  in production code (the reducer is imported in test code only, for the
  round-trip contract test). Repo-hosted manifest selection belongs to the Git
  collector stream path.
- Every emitted fact carries `schema_version = 1.0.0`
  (`facts.ServiceCatalogSchemaVersionV1`). For a known service-catalog fact kind,
  a blank or mismatched `schema_version` is a hard error at the projector
  (`validateServiceCatalogSchemaVersion` aborts the projection); it is never a
  benign skip, so producers must always stamp the supported version.
- `source_confidence = observed` because manifests are read directly from a repo
  artifact.
- Catalog names and owners never mint `repository_id`, `service_id`, or
  `workload_id`. Correlation is the reducer's job.
- OpsLevel-only: a repository is declared as `provider` + a `name` slug, not a
  URL. Expand only known public hosts (`github`, `gitlab`, `bitbucket`,
  `azure_devops`). Never guess a host for an unknown or self-hosted provider;
  emit the slug as a name-only `repository_name` and let the reducer reject it.
- Cortex-only: a repository is declared as a git `provider` plus a `name` slug,
  not a URL. Expand only known public hosts (`github`, `gitlab`, `bitbucket`,
  `azure`). Never guess a host for an unknown or self-hosted provider; emit the
  slug as a name-only `repository_name` and let the reducer reject it. Guessing a
  host would manufacture a wrong derivation and risk a false correlation. Resolve
  multi-provider blocks in sorted provider order for deterministic fact ids, and
  prefer the first provider that yields a real URL so an unexpandable provider
  never shadows a resolvable one.
- Token-bearing or query-string URLs are stripped before emission; redacted
  operational links emit a warning instead of dropping the entity.
- Degraded documents (unsupported version, missing name, duplicate entity) emit
  warnings, never silent drops. Multi-document manifests are parsed per
  document so one bad document does not abort the file.

## Telemetry

This package emits no metrics or spans; it is a deterministic normalizer. When
called from the Git collector, repo-hosted manifest facts are covered by the
existing Git collector observe, stream, fact-emission, fact-count, and commit
signals. Provider-specific producer counters (`service_catalog_facts_emitted_total`,
`service_catalog_manifest_warnings_total`,
`service_catalog_manifests_parsed_total`) and a `servicecatalog.collect` span
remain deferred to the hosted telemetry + Compose-proof slice. The
shipped downstream `ServiceCatalogCorrelations{outcome}` reducer counter remains
the diagnosis chain for "facts emitted but zero exact correlations."

No-Regression Evidence: fixture normalization for every provider is covered by
`go test ./internal/collector/servicecatalog -count=1`, which exercises typed
contract emission, the reducer round-trip reaching exact/derived/unresolved/
rejected/stale/ambiguous outcomes, blank `service_id`/`workload_id`/
`repository_id` assertions, warning emission for unsupported version, missing
ref, duplicate entity, and redacted link, empty input, and idempotent
re-emission — without graph writes or queue work. The OpsLevel slice adds the
same matrix over `opslevel.yml` fixtures, including the provider-host repository
URL derivation (known provider -> derivable URL; unknown/self-hosted provider ->
name-only locator that the reducer rejects). The Cortex slice adds the same
matrix over `cortex.yaml` fixtures, including the git-provider host derivation
(known provider -> derivable URL; unknown/self-hosted provider -> name-only
locator the reducer rejects; Azure project+repository split), deterministic
multi-provider resolution that prefers a resolvable provider over an unexpandable
one, and the scorecard slice (definition + result facts that pass the
schema-version gate and provably do not change any entity outcome).

Collector Performance Evidence: this slice introduces no runtime, no claim, no
worker, no queue, and no graph write. `BackstageManifestEnvelopes`,
`OpsLevelManifestEnvelopes`, `CortexManifestEnvelopes`, and
`CortexScorecardEnvelopes` are pure library functions that parse one offline
manifest in a single linear pass; cost is linear in the number of manifest
documents and emitted facts, with no hot-path Cypher and no new query. There is
no new hot path, so the performance contract is satisfied with this
no-regression note rather than a benchmark, proven by
`go test ./internal/collector/servicecatalog -count=1`.

Collector Observability Evidence: not applicable for this slice — it mounts no
runtime and exposes no `/healthz`, `/readyz`, `/metrics`, or `/admin/status`
surface. The shipped downstream `ServiceCatalogCorrelations{outcome}` reducer
counter remains the operator diagnosis chain. Producer counters and a
`servicecatalog.collect` span are deferred to the telemetry + Compose-proof
slice (design memo PR-4), which must land before live collection is enabled. See
the `No-Observability-Change:` marker below.

Git Producer Evidence: the Git collector recognizes repo-hosted
`catalog-info.yaml`/`.yml`, `opslevel.yml`/`.yaml`, and `cortex.yaml`/`.yml`
descriptors during content streaming and emits these normalizer envelopes under
the repository scope and generation. Focused coverage lives in
`go/internal/collector/git_service_catalog_facts_test.go` and proves the
two-phase and legacy content paths emit catalog facts without absolute path
leakage or fabricated `repository_id`, `service_id`, or `workload_id`.

Collector Deployment Evidence: no deployment artifact, binary, Compose service,
Helm Deployment, metrics Service, or ServiceMonitor is added or changed by this
slice. Repo-hosted descriptors run inside the existing Git collector. Wiring a
hosted claim-based catalog API runtime (with health/readiness, metrics,
ServiceMonitor, and Helm) is deferred to a future runtime slice.

No-Observability-Change: this package mounts no runtime and adds no metrics,
spans, or logs. The later telemetry slice must add fact-emission, warning, and
parse-result signals before live collection is enabled.
