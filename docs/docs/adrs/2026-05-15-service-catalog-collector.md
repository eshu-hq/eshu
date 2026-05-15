# ADR: Service Catalog Collector

**Date:** 2026-05-15
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- Issue: `#18`
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-05-09-documentation-truth-collectors-and-actuators.md`
- `2026-05-14-service-story-dossier-contract.md`
- `2026-05-15-ci-cd-run-collector.md`
- `2026-05-15-sbom-attestation-collector.md`
- `2026-05-15-vulnerability-intelligence-collector.md`

---

## Context

Eshu already observes source-code, infrastructure, deployment, package,
container, CI/CD, SBOM, vulnerability, and cloud evidence. Those sources prove
what exists and what ran. Service catalog systems add a different kind of
evidence: organizational declarations about owners, lifecycle, tier, systems,
domains, APIs, dependencies, docs, on-call links, operational tools, and
scorecards.

Catalog data is valuable, but it is not runtime truth. A catalog entry can be
stale, manually edited, auto-generated from weak inputs, or disconnected from
the code and workload that actually exist. The collector must preserve catalog
claims as source facts and let reducers compare those claims against Eshu's
code-to-cloud graph.

This ADR is design-only. Runtime implementation should wait until the current
collector deployment lane has stable proof for hosted credentials, pagination,
rate limits, redaction, and status reporting. Fixture-backed manifest parsing
can start earlier because it does not require live SaaS credentials.

## Source References

This ADR was checked against the current public contracts for the first source
set:

- Backstage descriptor format:
  <https://backstage.io/docs/features/software-catalog/descriptor-format/>
- Backstage entity references:
  <https://backstage.io/docs/features/software-catalog/references/>
- Backstage catalog API:
  <https://backstage.io/docs/features/software-catalog/software-catalog-api/>
- Backstage well-known annotations:
  <https://backstage.io/docs/features/software-catalog/well-known-annotations/>
- OpsLevel `opslevel.yml`:
  <https://docs.opslevel.com/docs/opslevel-yml>
- OpsLevel component dependencies:
  <https://docs.opslevel.com/docs/service-dependencies>
- OpsLevel GraphQL API:
  <https://docs.opslevel.com/docs/graphql>
- OpsLevel scorecards:
  <https://docs.opslevel.com/docs/scorecards>
- Cortex entity YAML:
  <https://docs.cortex.io/ingesting-data-into-cortex/entities/yaml>
- Cortex dependencies API:
  <https://docs.cortex.io/api/readme/dependencies>
- Cortex scorecards API:
  <https://docs.cortex.io/api/readme/scorecards>
- Cortex ownership:
  <https://docs.cortex.io/ingesting-data-into-cortex/entities/ownership>

## Source Contracts

The first implementation must preserve provider-native identity and avoid
collapsing provider-specific meaning too early.

| Source | Source truth | Contract notes |
| --- | --- | --- |
| Backstage descriptor files | `catalog-info.yaml` or equivalent entity descriptors with entity refs, metadata, annotations, links, relations, owners, systems, domains, APIs, and resources | Full entity references such as `component:default/payments-api` are stable anchors. Backstage documents `metadata.uid` as output and unstable, so it must not be the durable Eshu identity. |
| Backstage Catalog API | Current catalog entities, relations, status items, annotations, locations, and processor-derived relations | API relations may be stronger than raw `spec.*` fields because processors can derive ownership and links from surrounding data. Preserve both. |
| OpsLevel YAML | `opslevel.yml` component or repository declarations with aliases, owner, lifecycle, tier, system, product, repositories, dependencies, tools, alert sources, and tags | OpsLevel aliases are source-native identifiers. Locked YAML-managed fields are useful freshness signals. |
| OpsLevel GraphQL/API | Components, repositories, teams, dependencies, scorecards, checks, rubrics, and maturity results | Pagination and partial API failures must produce partial coverage, not erased catalog truth. |
| Cortex YAML | `cortex.yaml` entity descriptors based on OpenAPI with `x-cortex-*` extensions for tag, type, owners, groups, dependencies, links, CI/CD, APM, on-call, infrastructure, and other integrations | `x-cortex-tag` is the stable entity anchor. OpenAPI content is API evidence only when a real API spec is present. |
| Cortex API | Dependencies, relationships, scorecards, ownership, and catalog metadata | Scorecard/check results are standards evidence, not relationship truth. |

## Decision

Add a future collector family named `service_catalog`.

The collector owns:

- parsing repo-hosted catalog manifests
- fetching configured service catalog API snapshots
- preserving provider-native entity identifiers and aliases
- normalizing declared ownership, lifecycle, tier, system/domain/product,
  repository links, API links, dependency declarations, operational links, and
  scorecard results into typed facts
- recording source coverage, freshness, partial failures, and warnings
- redacting sensitive URLs, tokens, emails when configured, webhook secrets,
  and private integration metadata before logs or metrics

The collector does not own:

- repository sync or generic source parsing
- canonical graph writes
- deciding that a catalog service maps to a real workload
- deciding that a declared dependency is runtime dependency truth
- deciding ownership precedence between catalog, CODEOWNERS, cloud tags, IAM,
  Terraform, Kubernetes, or documentation evidence
- scorecard policy enforcement
- documentation updates or service catalog writes

Reducers own correlation and drift findings.

## Scope And Generation Model

The bounded acceptance unit is the stable catalog entity, not the repository.
Collection still happens through a source scope first, then emits entity-scoped
facts for each stable catalog entity observed inside that source.

Collector modes:

- `service_catalog_manifest` for repo-hosted descriptors such as
  `catalog-info.yaml`, `opslevel.yml`, and `cortex.yaml`.
- `service_catalog_api` for provider API snapshots from Backstage, OpsLevel,
  and Cortex.

Source scope IDs identify the collection input:

```text
service-catalog-manifest://<repo-id>/<path>
service-catalog-api://<provider>/<tenant-id-or-host>
```

`<tenant-id-or-host>` must be canonicalized before it is used in a scope ID:
strip URL scheme, query, fragment, user info, and trailing slashes; lowercase
the host; preserve only the configured tenant identifier or host plus an
operator-approved base path when two catalog tenants share one host. Raw URLs
must stay in facts as source locators, not in scope IDs.

Entity scope IDs identify the fact acceptance unit derived from either source
mode:

```text
service-catalog-entity://<provider>/<tenant-id-or-host>/<entity-ref>
```

Source generation IDs:

- Manifest mode: `<git-generation-id>:<descriptor-content-sha>`.
- API mode: provider cursor, ETag, update timestamp, or response digest plus
  observed timestamp.

Entity generation IDs should use the provider entity version where available;
otherwise use the normalized entity digest plus the source generation ID that
observed it.

When a provider has no transactional snapshot API, the collector must mark the
generation as partial or eventually consistent and preserve page/cursor
coverage.

## Fact Families

Initial fact kinds should use `collector_kind=service_catalog`.

| Fact kind | Purpose |
| --- | --- |
| `service_catalog.entity` | One catalog entity with provider, entity type, provider ref, display name/title, lifecycle, tier, system/product/domain, tags, aliases, source locator, and provider version metadata. |
| `service_catalog.ownership` | One ownership claim with team/user/group ref, inheritance mode, owner source, provider metadata, and confidence. |
| `service_catalog.repository_link` | One declared source repository link with provider, slug, URL, repo path, monorepo path, source location annotation, and descriptor source. |
| `service_catalog.dependency` | One declared dependency or relationship with source ref, target ref, direction, relationship type, optional method/path, notes, and provider metadata. |
| `service_catalog.api_link` | One provided or consumed API declaration with API ref, schema pointer, OpenAPI/AsyncAPI hint, method/path when present, and source evidence. |
| `service_catalog.operational_link` | One docs, runbook, dashboard, on-call, Slack/Teams, issue tracker, incident, metric, log, deployment, or tool link with redacted URL metadata. |
| `service_catalog.scorecard_definition` | One scorecard, rubric, level, rule, check, filter, or category definition. |
| `service_catalog.scorecard_result` | One entity scorecard/check result with status, level, score, failed checks, exemptions, observed time, and provider metadata. |
| `service_catalog.warning` | Unsupported descriptor version, invalid entity ref, missing target, duplicate entity, provider partial page, stale snapshot, redaction event, rate limit, or auth denial. |

`source_confidence` should use:

- `observed` for repo-hosted descriptor files read by Eshu from Git.
- `reported` for provider API facts returned by Backstage, OpsLevel, or Cortex.
- `derived` only for normalized helper facts computed from already stored
  service catalog facts.

## Identity And Correlation Rules

Provider-native identifiers are mandatory.

Rules:

1. Backstage attachment should use full entity refs, not `metadata.uid`.
2. OpsLevel attachment should preserve aliases and provider component IDs.
3. Cortex attachment should preserve `x-cortex-tag` and entity type.
4. Repository links can map a catalog entity to a repository only when the
   provider repo locator, descriptor source, or source-location annotation
   matches a canonical repo identity.
5. Catalog service names do not create workloads by themselves.
6. Declared dependencies and API links remain declared catalog evidence until
   reducer-owned code, contract, runtime, CI/CD, or deployment evidence
   corroborates them.
7. Scorecards attach to catalog entities, repos, or services as standards and
   maturity evidence. They must not become dependency or deployment truth.

## Reducer Correlation Contract

Reducers should admit catalog joins only when the evidence path is explicit:

```text
Catalog entity ref
  -> repository link or source descriptor
  -> canonical repository
  -> deployable-unit or workload evidence
  -> optional runtime, CI/CD, cloud, Kubernetes, SBOM, package, or vulnerability evidence
```

Candidate outcomes:

| Outcome | Meaning |
| --- | --- |
| `exact` | One catalog entity matched one canonical repo, service, API, or workload through stable source identity. |
| `derived` | A deterministic rule matched through provider-owned aliases or source-location metadata. |
| `ambiguous` | More than one canonical target matched, or a catalog alias/name collided. |
| `unresolved` | The catalog entity is valid but has no matching Eshu target. |
| `stale` | Catalog evidence conflicts with fresher code, deployment, cloud, or runtime evidence. |
| `rejected` | A name-only, weak, stale, or unsafe signal was suppressed. |

Catalog drift findings should compare declared catalog state to graph truth
without letting the catalog override graph truth.

## Freshness And Backfill

Normal freshness should use small, provider-specific updates:

- repo descriptor changes observed through Git refresh
- provider API cursors or updated timestamps
- webhook triggers where supported
- scheduled reconciliation with a bounded overlap window
- targeted entity refresh from query or incident workflows

Full tenant scans are backfill and recovery tools. They must require explicit
operator limits: maximum entities, maximum pages, maximum scorecard results,
maximum dependency edges, maximum age, and request budget.

Partial provider coverage must remain visible in status and query truth. A
SaaS outage, token scope gap, or rate limit must produce stale or unavailable
freshness, not delete catalog facts.

## Query And MCP Contract

Future read surfaces should be bounded and identity-first:

- list catalog entities that cannot map to a repo or workload
- show catalog owner, tier, lifecycle, on-call, runbook, scorecard, and source
  links for a repo or service
- compare declared catalog dependencies with contract/runtime dependencies
- show Tier 1 services with no deployment or runtime evidence
- show catalog entries whose owner conflicts with CODEOWNERS, cloud tags, or
  service metadata
- show standards/scorecard evidence for a vulnerability or service-impact
  answer

Responses must include provider, entity ref, source mode, generation ID,
truth/confidence label, freshness, deterministic ordering, `limit`, and
`truncated`. Normal use must not require raw Cypher.

## Observability Requirements

The hosted runtime must expose:

- collect duration by provider and source mode
- API request counts by provider, operation, and bounded result
- rate-limit/throttle counts by provider and operation
- entities observed by provider and entity type
- facts emitted by provider and fact kind
- dependency and API-link counts by provider
- scorecard definitions and results observed by provider
- unresolved, ambiguous, stale, exact, derived, and rejected correlation counts
- partial generation counts by provider and reason
- redaction counts by provider and field family
- source freshness lag by provider and scope
- collector claim duration, processing duration, and retry/dead-letter counts

Spans should cover scope discovery, descriptor parse, API page fetch,
dependency fetch, scorecard fetch, fact batch emission, and reducer
correlation.

Metric labels must not include entity names, repo names, team names, emails,
URLs, scorecard names, dependency targets, source paths, or credential
references. Those values belong in facts, spans, or structured logs with
redaction.

## Security And Privacy

Service catalogs can expose private service names, ownership, on-call contacts,
emails, Slack/Teams channels, runbooks, dashboards, incident links, dependency
maps, scorecard failures, and internal URLs.

Rules:

- Provider credentials must be read-only.
- Status output must not include raw URLs, tokens, emails, team membership
  lists, private channel names, or source paths unless explicitly allowed.
- Operational links are stored as facts but logged and metered only as bounded
  categories.
- Scorecard failure details may be sensitive and must respect the same evidence
  permission checks as other customer data.
- The collector must not write back to Backstage, OpsLevel, Cortex, Git, Jira,
  Slack, PagerDuty, or any catalog system.

## Implementation Gate

The first implementation should be split into small PRs:

1. Fact contracts and fixture schemas for Backstage, OpsLevel, and Cortex.
2. Backstage manifest parser for `catalog-info.yaml`.
3. OpsLevel manifest parser for `opslevel.yml`.
4. Cortex manifest parser for `cortex.yaml`.
5. Reducer correlation tests for exact, derived, ambiguous, unresolved, stale,
   and rejected outcomes.
6. Backstage API collector behind fixture-backed HTTP tests.
7. OpsLevel and Cortex API collectors behind fixture-backed HTTP tests.
8. Hosted runtime with credentials, budgets, pagination, redaction, health,
   readiness, metrics, admin/status, and ServiceMonitor proof.

Implementation must not start with graph writes or query shortcuts. Facts and
reducer contracts come first.

## Acceptance Criteria

- Collector emits versioned facts only; no direct graph writes.
- Backstage mode handles Component, API, System, Domain, Resource, Group/User
  refs, relations, status, locations, and annotations.
- OpsLevel mode handles YAML aliases, repositories, dependencies, tools, alert
  sources, checks, scorecards, and GraphQL ingestion.
- Cortex mode handles entity YAML, `x-cortex-tag`, type, owners, groups,
  dependencies, links, on-call fields, integrations, and scorecards.
- Reducer preserves unresolved, ambiguous, exact, derived, stale, and rejected
  states.
- Query responses expose truth labels and freshness.
- Tests cover stale catalog data, missing refs, renamed aliases, duplicate
  entities, pagination/rate limits, monorepo descriptors, scorecard partials,
  and conflicting dependency direction.
- Hosted runtime proof includes request-budget, rate-limit, health, readiness,
  metrics, admin/status, and ServiceMonitor evidence before production use.

## Rejected Alternatives

### Treat Catalog Ownership As Authoritative

Rejected. Catalog ownership is a declaration. It can be exact, stale, derived,
or ambiguous depending on corroborating evidence and freshness.

### Promote Declared Dependencies Directly To Runtime Edges

Rejected. Declared dependencies are useful, but runtime, contract, code, or
deployment evidence must corroborate before canonical runtime dependency
answers claim exact truth.

### Use Display Names As Durable Identity

Rejected. Display names are human labels and rename often. Provider refs,
aliases, tags, source locations, and repository links are the durable identity
surface.

### Combine Documentation And Service Catalog Collection

Rejected. Catalog systems and documentation systems overlap, but catalog
entities have provider identity, scorecard, ownership, dependency, and
operational-link semantics that need their own fact families.
