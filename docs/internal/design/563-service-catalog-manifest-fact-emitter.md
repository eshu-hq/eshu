# Design Memo: Service Catalog Manifest Fact Emitter (#563)

Status: Producer package shipped; repo-hosted Git emission slice in progress.
Hosted catalog API polling remains out of scope.
Author surface: `go/internal/collector/...`, fixture corpora, package docs.
Audience: maintainers reviewing producer boundaries and follow-up runtime slices.

This is a maintainer-only internal design doc. It is not part of the public
MkDocs site (`docs_dir: public`), so the strict docs build does not cover it.

## 1. Problem and Acceptance Criteria

### Problem

The `service_catalog_correlation` reducer domain and its read surface already
shipped:

- Fact kinds: `go/internal/facts/service_catalog.go`
  (`service_catalog.entity`, `.ownership`, `.repository_link`, `.dependency`,
  `.api_link`, `.operational_link`, `.scorecard_definition`,
  `.scorecard_result`, `.warning`; `ServiceCatalogSchemaVersionV1 = "1.0.0"`).
- Projector intent wiring: `go/internal/projector/service_catalog_correlation_intents.go`
  (`buildServiceCatalogCorrelationReducerIntent`, `validateServiceCatalogSchemaVersion`).
- Reducer handler and writer: `go/internal/reducer/service_catalog_correlation.go`,
  `service_catalog_correlation_index.go`, `service_catalog_correlation_writer.go`
  (#561), writing the provenance-only `reducer_service_catalog_correlation`
  fact with six outcomes.
- Read surface: `go/internal/query/service_catalog_correlations.go` (#560) and
  MCP tool `list_service_catalog_correlations`
  (`go/internal/mcp/tools_service_catalog.go`).
- Telemetry counter: `Instruments.ServiceCatalogCorrelations`
  (`go/internal/telemetry/instruments.go`, contract in
  `contract_service_catalog.go`).

The original absence was the producer. Nothing emitted `service_catalog.*`
facts, so the consumer side had a graph with no inputs. Issue #563 shipped the
pure manifest normalizer. The follow-up producer slice wires the Git collector
content stream to that normalizer for repo-hosted Backstage, OpsLevel, and
Cortex descriptors, still emitting only typed facts into the already-defined
contract.

The issue references an ADR at the stale path `docs/docs/adrs/2026-05-15-service-catalog-collector.md`;
the current docs root is `docs/public/`. The ADR file is not present in the
tree at either path. The source-truth boundary for this collector is instead
captured in `docs/public/reference/collector-reducer-readiness.md`, which lists
`service_catalog_correlation` as "Catalog names, owners, and labels remain
provenance until explicit repository evidence admits correlation" and classes
hosted catalog API collection as not deployed. The reducer side is shipped;
repo-hosted descriptors now run inside the existing Git collector, while hosted
Backstage/OpsLevel/Cortex API polling remains undeployed.

### Acceptance criteria (verbatim intent from #563)

1. Fixture tests parse Backstage (`catalog-info.yaml`), OpsLevel
   (`opslevel.yml`), and Cortex (`cortex.yaml`) manifests into the typed fact
   contract.
2. Unsupported descriptor versions, invalid refs, duplicate entities, missing
   repository links, and redaction cases produce **warning facts** instead of
   silent drops.
3. Projector queues `service_catalog_correlation` for emitted catalog facts.
   (Already true via the shipped intent builder — see Section 4. The producer
   only has to emit at least one fact whose kind passes
   `facts.ServiceCatalogSchemaVersion`.)
4. Reducer writes `reducer_service_catalog_correlation` for at least exact,
   unresolved, ambiguous, stale, and rejected fixture cases. (Reducer logic
   exists; this is a fixture-coverage obligation on the producer, not new
   reducer code — see Section 7.)
5. API/MCP proof lists the resulting correlations by entity, repository, owner,
   and outcome. (Filters exist on `ServiceCatalogCorrelationFilter`:
   `EntityRef`, `RepositoryID`, `OwnerRef`, `Outcome` — proof only.)
6. Remote Docker Compose proof runs before any EKS rollout: manifest fixture ->
   `service_catalog.*` facts -> reducer intent ->
   `reducer_service_catalog_correlation` -> API/MCP read.

### Out of scope (verbatim)

Hosted Backstage/OpsLevel/Cortex API polling; provider credentials and
rate-limit budgets; graph writes from catalog data; canonical service/workload
creation from catalog names alone.

### Recommendation summary

**Build-safe**, with one hard constraint and two open questions. The design
surface is small, additive, and producer-only. There is no schema change, no
graph-write, no new reducer domain, and no new concurrency primitive. The risk
is almost entirely in **payload-key fidelity** to the reducer's index (Section
3) and in **not over-admitting** catalog names into repository/service/workload
truth (Sections 6, 7). Details follow.

## 2. How It Fits Eshu

Eshu is facts-first. The pipeline is
`sync -> discover -> parse -> emit facts -> enqueue work -> reducer ->
graph/content projection -> query surface` (CLAUDE.md golden rules;
`docs/public/architecture.md` write path).

This feature lives entirely in the **intake/collector stage**. The boundary
rule from `architecture.md` is non-negotiable: "collectors and webhooks observe
source truth; the resolution engine decides graph truth. Intake services do not
write canonical graph state directly." The manifest emitter observes a repo
file and commits versioned facts. It never touches the graph, the reducer
package, or the query package.

Stage-by-stage placement of this slice:

| Stage | Owner today | This feature |
| --- | --- | --- |
| sync | ingester / git collector | unchanged |
| discover | `internal/collector/discovery` + Git content stream | ordinary repo discovery selects files; the Git fact stream recognizes `catalog-info.yaml` / `opslevel.yml` / `cortex.yaml` descriptor names during content emission |
| parse | `internal/collector/servicecatalog` | YAML manifest -> typed model |
| emit facts | Git collector + servicecatalog normalizer | `service_catalog.*` envelopes under the repository scope and generation |
| enqueue work | projector (shipped) | already queues `service_catalog_correlation` |
| reducer | shipped (#561) | already classifies into six outcomes |
| graph/content | n/a for this domain | **provenance-only; no graph write** |
| query surface | shipped (#560, MCP) | already lists correlations |

The template precedent is `internal/collector/cicdrun`: a fixture-backed,
metadata-only fact emitter exposed as a library entry point
(`GitHubActionsFixtureEnvelopes`) that is not yet wired into a live runtime
claim. Its `doc.go` and `AGENTS.md` state the exact invariants we will mirror:
no HTTP clients, no credential loading, no graph writes, no reducer/query
imports, emit warnings for partial metadata, strip token-bearing URLs.

## 3. Fact / Schema Shape

**No new fact kinds and no schema-version bump.** The nine `service_catalog.*`
kinds and `ServiceCatalogSchemaVersionV1 = "1.0.0"` already exist in
`go/internal/facts/service_catalog.go`. The producer emits into that contract
unchanged. The design discipline here is **payload-key fidelity**: the reducer
index (`service_catalog_correlation_index.go`) reads specific payload keys, and
the producer must emit exactly those keys or the correlation silently degrades
to `unresolved`/`rejected`.

Required payload keys, derived from the reducer's `...FromFact` readers (these
are a contract the producer MUST honor):

- `service_catalog.entity` (read by `serviceCatalogEntityFromFact`):
  `provider`, `entity_ref`, `entity_type`, `display_name`, `lifecycle`, `tier`,
  and optionally `service_id`, `workload_id` (leave blank — see constraint
  below).
- `service_catalog.ownership` (`serviceCatalogOwnershipFromFact`): `provider`,
  `entity_ref`, `owner_ref` (the reducer also accepts legacy `owner`).
- `service_catalog.repository_link` (`serviceCatalogRepositoryLinkFromFact`):
  `provider`, `entity_ref`, plus the repository locator. The reducer matches in
  this precedence: `repository_id`/`repo_id` first, then a URL via
  `normalized_url` / `repository_url` / `raw_url` / `url`, plus
  `repository_name`. **The producer MUST NOT synthesize a `repository_id`**; it
  only knows the manifest's declared URL/slug, so it emits the declared URL into
  `repository_url` (and a best-effort `normalized_url` using the same git-URL
  canonicalization the reducer applies via `canonicalPackageSourceURLKey`).
  Emitting a fabricated `repository_id` would force a false `exact` outcome.
- `service_catalog.dependency`, `.api_link`, `.operational_link`,
  `.scorecard_definition`, `.scorecard_result`: the reducer index does not yet
  read these (only entity, ownership, repository_link, and active repository
  facts feed the index). They are still emitted for read-surface completeness
  and forward compatibility, with `provider` + `entity_ref` anchors so a future
  reducer extension can join them. This must be called out so the principal
  knows these fact families are **carried but not yet correlated**.
- `service_catalog.warning` (`facts.ServiceCatalogWarningFactKind`): `provider`,
  `reason`, `message` (redacted), and `entity_ref` when known. Mirrors
  cicdrun's `warningEnvelope`.

**Hard producer constraint (accuracy):** `service_id` and `workload_id` on the
entity/repository_link facts MUST be left blank by the manifest collector. The
collector observes a YAML file; it has no canonical service or workload
identity. The reducer only fills `ServiceID`/`WorkloadID` from a link when a
repository match is `exact`/`derived`, and even then provenance stays declared.
Letting a catalog name mint a `service_id` is exactly the "canonical
service/workload creation from catalog names alone" the issue forbids and the
`eshu-correlation-truth` skill's over-admission anti-pattern.

Envelope construction mirrors `cicdrun/envelope.go`:

- `FactID = facts.StableID("ServiceCatalogFact", {fact_kind, scope_id,
  generation_id, stable_fact_key})`.
- `StableFactKey` derived per kind from stable identity — for entity:
  `StableID(entityFactKind, {provider, entity_ref})`; for repository_link:
  `{provider, entity_ref, repository_url|repository_id}`; for ownership:
  `{provider, entity_ref, owner_ref}`. Stable keys make re-emission idempotent
  across generations.
- `SchemaVersion = facts.ServiceCatalogSchemaVersionV1` (must match exactly or
  `validateServiceCatalogSchemaVersion` rejects at the projector).
- `CollectorKind = "service_catalog"` (proposed durable family name; confirm
  with principal — Section 10).
- `SourceConfidence = facts.SourceConfidenceObserved` — manifests are read
  directly from a repo artifact, so `observed`, not `reported` (cicdrun uses
  `reported` because it normalizes provider API payloads). This matches the
  `source_confidence.go` doc comment for `observed`.
- `SourceRef`: `SourceSystem = "service_catalog"`, `SourceURI = repo-relative
  manifest path`, no token-bearing URLs (reuse a `stripSensitiveURL` analog).

### Redaction

Catalog manifests routinely carry on-call URLs, dashboard links, Slack
webhooks, PagerDuty integration URLs, and email addresses. Redaction rules:

- Strip any token-bearing or query-string URL before emission (cicdrun's
  `stripSensitiveURL`: drop if `parsed.User != nil || parsed.RawQuery != ""`).
- `operational_link` values that fail the URL safety check emit a
  `service_catalog.warning` with reason `operational_link_redacted` and the raw
  link omitted, rather than dropping the entity.
- Free-text fields (descriptions, owner emails) pass through a
  `redactSensitiveText` analog that masks embedded credential-bearing URLs.
- Never emit raw bearer tokens or API keys that appear in annotations; mask to
  `[redacted]`.

## 4. Reducer Projection / Read-Model Design

**No reducer code changes in this feature.** This section documents the
already-shipped wiring the producer plugs into, so the principal can confirm the
producer does not need to touch projection.

- Queue work kind / domain: `reducer.DomainServiceCatalogCorrelation`
  (`"service_catalog_correlation"`, `go/internal/reducer/intent.go:64`).
- Intent emission: `buildServiceCatalogCorrelationReducerIntent` already fires
  whenever any envelope's `FactKind` passes `facts.ServiceCatalogSchemaVersion`.
  `EntityKey = "service_catalog_correlation:" + scopeID`. So **the producer
  emitting one valid `service_catalog.*` fact is sufficient to satisfy
  acceptance criterion 3.** The producer must not duplicate this logic.
- Schema-version gate: `validateServiceCatalogSchemaVersion` rejects blank or
  mismatched `SchemaVersion`. The producer MUST set `SchemaVersion` to
  `"1.0.0"`; this is the single most likely silent-failure point and gets a
  direct unit assertion.
- Registry / domain definition:
  `serviceCatalogCorrelationDomainDefinition` (`registry.go`) declares
  `Ownership{CrossSource, CrossScope, CanonicalWrite, CounterEmit}` and a
  `TruthContract{CanonicalKind: "service_catalog_correlation", SourceLayers:
  [LayerSourceDeclaration]}`. Unchanged.
- Handler: `ServiceCatalogCorrelationHandler.Handle` loads catalog facts for the
  scope/generation plus active repository facts
  (`ListActiveRepositoryFacts`), builds decisions, writes them, and emits the
  six-outcome counter. Unchanged.
- Read model: the writer persists `reducer_service_catalog_correlation` facts
  keyed by `service_catalog_correlation:{scope}:{generation}:{provider}:
  {entity_ref}` with `source_confidence = inferred`. The query store
  (`ListServiceCatalogCorrelations`) and MCP tool already filter by entity,
  repository, service, workload, owner, outcome, and drift status.

Implication: the entire "reducer projection / read-model" obligation for #563 is
**fixture coverage and proof**, not new projection code. If implementation finds
the reducer index cannot reach a needed outcome from manifest-only facts, that
is a separate reducer issue and must not be smuggled into the collector PR.

## 5. Graph-Write Plan

**There is no graph write in this feature, by design and by issue scope** ("No
graph writes", "Graph writes from catalog data" out of scope). Verified: the
reducer writer (`service_catalog_correlation_writer.go`) contains no `MERGE`,
no Cypher, no node/edge creation — it writes a Postgres fact row only. The
domain is provenance-only; `source_layers` is `[source_declaration]` and only
gains `observed_resource` when a real repository match exists.

Because there is no Cypher and no graph write on this path, the
`cypher-query-rigor` checklist resolves to: **not applicable, no hot-path Cypher
introduced.** The relevant idempotency and conflict-key discipline still
applies, but at the **fact layer**, not the graph layer:

- Idempotent identity: `FactID`/`StableFactKey` are content-stable per
  `{provider, entity_ref, ...}`. Re-running the same manifest in a new
  generation produces the same stable key, so the fact store upserts rather than
  duplicates (same pattern as cicdrun `deduplicateEnvelopes` plus stable IDs).
- Conflict-key partitioning: the natural partition is `(scope_id,
  generation_id, provider, entity_ref)`. Two manifests in one repo declaring the
  same `provider`+`entity_ref` is a **duplicate-entity** case that must emit a
  `service_catalog.warning` (reason `duplicate_entity`) and keep the
  first-wins / deterministic entity, never silently overwrite — acceptance
  criterion 2.
- "Serialization Is Not A Fix": the producer is embarrassingly parallel across
  repos/manifests because every fact carries a stable, partition-scoped
  identity. There is **no shared mutable write target** at collection time
  (facts go to the append/upsert fact store keyed by stable ID), so the design
  must not introduce a global lock, a single-threaded drain, or a batch-size-1
  workaround to "fix" duplicates. Duplicates are resolved by deterministic
  stable-key identity and warning emission, not by serializing the collector.
  If the future hosted slice adds claim-based concurrency, conflict
  partitioning is already `(scope, generation, provider, entity_ref)`.

Future graph materialization (a real edge such as
`(:ServiceCatalogEntity)-[:DECLARES_REPOSITORY]->(:Repository)`) is explicitly
**out of scope** and would be a separate reducer/Cypher PR governed by
`cypher-performance.md`. This memo records that boundary so the producer PR is
not asked to grow it.

## 6. Correlation Truth Matrix

Per `eshu-correlation-truth`, the producer's job is to feed evidence such that
each outcome is reachable and **none over-admits**. The reducer owns
classification; the producer owns emitting evidence that lands in the right
class. Mapping fixture intent -> reducer outcome
(`ServiceCatalogCorrelationOutcome` in `service_catalog_correlation.go`):

| Case | Fixture intent (manifest input) | Active repo facts present? | Expected outcome | Why |
| --- | --- | --- | --- | --- |
| Positive (exact) | entity + repository_link whose `repository_id` equals a canonical repo id, OR URL that matches a repo remote exactly | yes, active | `exact` | `matchServiceCatalogRepositoryID` / exact URL match |
| Positive (derived) | repository_link URL that matches a repo remote only after git-URL canonicalization (e.g. `.git` suffix, scheme/host normalization) | yes, active | `derived` | `matchServiceCatalogRepositoryURL` non-exact |
| Negative (unresolved, no link) | entity with no `repository_link` fact | n/a | `unresolved` | "catalog entity has no repository link evidence" |
| Negative (unresolved, no match) | repository_link URL that matches no active repo | repos exist but none match | `unresolved` | link did not match any active repository |
| Negative (rejected) | repository_link with neither URL nor canonical id (name-only slug) | n/a | `rejected` | "name-only links cannot prove ownership" — the core over-admission guard |
| Ambiguous | repository_link URL/id that matches **multiple** active repos | yes, 2+ active | `ambiguous` | "matches multiple active repository facts" |
| Stale | repository_link matching only **tombstoned** repo facts | only tombstoned | `stale` | matched only tombstoned repository evidence |
| Provenance-only (always) | any entity/ownership without a confirmed active match | — | decision keeps `ProvenanceOnly = true`, blank `RepositoryID/ServiceID/WorkloadID` | name/owner never mints canonical truth |

Falsification edge classes the fixtures must include (so we prove we did not
over-admit):

- A manifest `entity_ref` that **coincidentally** equals a repo name string but
  declares no resolvable repository link -> must be `rejected`/`unresolved`, not
  `exact`. (Mirrors the skill's "repo-name coincidence is not deployment
  truth.")
- An `owner_ref` that looks like a team but no repository link -> `unresolved`,
  owner recorded as provenance only, no service created.
- A manifest declaring `spec.system`/`spec.dependsOn` (dependency facts) that
  name other entities -> dependency facts emitted but, because the reducer index
  does not consume them yet, they MUST NOT change the entity's outcome. Fixture
  asserts the dependency fact exists AND the outcome is unchanged.
- Unsupported `apiVersion` (e.g. a future Backstage version) -> warning fact
  (`unsupported_descriptor_version`), entity still emitted if minimally parseable
  or skipped-with-warning if not — never a silent drop.

Proof matrix (must all agree before "done"): focused producer unit tests
(positive/negative/ambiguous fact shapes) -> reducer unit test consuming those
facts reaches each outcome -> Compose run shows `reducer_service_catalog_correlation`
rows -> MCP `list_service_catalog_correlations` filtered by `outcome=` returns
the same rows. Graph proof is **N/A** (provenance-only); the equivalent
"canonical truth" proof is the read-model fact row plus the MCP read agreeing
with the reducer decision.

## 7. Risks

### Accuracy

- **Payload-key drift (highest risk).** If the producer emits a key the reducer
  index does not read (e.g. `repo_url` instead of `repository_url`), correlation
  silently collapses to `unresolved` with green tests on the producer side.
  Mitigation: a contract test in the producer package that round-trips emitted
  envelopes through `BuildServiceCatalogCorrelationDecisions` (importing the
  reducer in **test code only**, not production) to assert real outcomes. This
  is the single most important test in the slice.
- **Over-admission.** Emitting `service_id`/`workload_id`/`repository_id` from
  manifest text would manufacture canonical truth. Mitigation: hard constraint
  in Section 3; lint-by-review and an explicit unit asserting those fields are
  blank on producer output.
- **Schema-version mismatch** silently rejected at the projector. Mitigation:
  direct assertion that every emitted envelope has `SchemaVersion == "1.0.0"`.
- **Multi-document YAML** (`---` separated) and provider quirks (OpsLevel YAML
  vs Backstage multi-kind) parsed incompletely. Mitigation: per-document parse
  with per-document warning on failure, never abort the whole file.

### Performance

- Producer is fixture/file-bound and additive; no hot-path Cypher, no graph
  write, no new query. There is **no new hot path**, so the cypher/perf gate is
  satisfied with a `No-Regression Evidence:` note (manifest count -> fact count
  -> existing reducer drain), not a benchmark. State this explicitly in the PR.
- One scaling watch item: a monorepo with thousands of `catalog-info.yaml`
  files produces many facts and a larger reducer intent. The reducer already
  loads active repository facts once per intent; fact volume is linear in
  manifest count. Record manifest/fact counts in Compose proof.

### Concurrency

- No new worker, lease, queue, channel, or shared mutable state in this slice
  (cicdrun is a pure library function). The conflict domain `(scope,
  generation, provider, entity_ref)` is documented for the future hosted slice.
  `No-Observability-Change` / `No-Regression` evidence is appropriate; no
  contention proof is required because no contended resource is added.

### Schema

- `risk:schema` label is conservative: the feature reuses an existing,
  versioned fact contract and does **not** alter it. The genuine schema risk is
  emitting payloads that diverge from `ServiceCatalogSchemaVersionV1`'s implied
  shape. Because schema version is shared with the reducer, any field the
  reducer needs but the producer omits is a compatibility gap, not a version
  bump. If a new required field is discovered, it is a `1.x` discussion with the
  reducer owner, not a unilateral producer change (Open Question 2).

## 8. Telemetry

The reducer already emits `Instruments.ServiceCatalogCorrelations` per outcome.
For the producer (3 AM operator view of "did catalog collection work?"):

- **Counter** `service_catalog_facts_emitted_total{provider, fact_kind}` —
  volume by provider and kind, so an operator sees Backstage vs OpsLevel vs
  Cortex throughput and which fact families appear. Mirrors cicdrun's per-kind
  emission shape.
- **Counter** `service_catalog_manifest_warnings_total{provider, reason}` —
  every warning fact reason (`unsupported_descriptor_version`,
  `duplicate_entity`, `missing_repository_link`, `operational_link_redacted`,
  `invalid_ref`). This is the primary signal that manifests are present but
  degraded; a spike here at 3 AM means "manifests changed shape," not "pipeline
  down."
- **Counter** `service_catalog_manifests_parsed_total{provider, result}` with
  `result in {parsed, failed}` — distinguishes "no manifests found" from "found
  but unparseable."
- **Span** `servicecatalog.collect` around fixture/file normalization with
  attributes `provider`, `manifest_path` (repo-relative, redacted), `entities`,
  `warnings`. Lets an operator trace one slow/failing manifest.
- Existing downstream signals stay the diagnosis chain: producer counters ->
  `ServiceCatalogCorrelations{outcome}` reducer counter -> read-surface row
  count. A 3 AM operator correlates "facts emitted but zero exact correlations"
  to a repository-link payload problem using exactly these three layers.

Instrument registration follows `telemetry/instruments.go` +
`contract_service_catalog.go` (the contract test enforces naming). Telemetry
additions are a normal part of the producer PR, documented in the package
`README.md`.

## 9. Phased PR Plan (smallest-first)

Each PR is independently reviewable, test-first (TDD), and leaves the tree
green. No PR pushes to `main`; each is a worktree branch.

- **PR-0 (this PR): design memo.** No code. Principal review gate.
- **PR-1: package skeleton + Backstage `catalog-info.yaml` slice.** New
  `go/internal/collector/servicecatalog` with `doc.go`, `README.md`,
  `AGENTS.md` (the mandatory trio; `scripts/verify-package-docs.sh`), a typed
  Backstage model, `BackstageManifestEnvelopes([]byte, FixtureContext)`, the
  envelope builder, redaction helpers, and testdata
  (`backstage_catalog_info.yaml`, plus a partial/invalid fixture). Tests cover
  entity/ownership/repository_link emission, the schema-version assertion, the
  blank-`service_id` assertion, and the reducer round-trip contract test
  reaching `exact`/`unresolved`/`rejected`. Smallest viable producer.
- **PR-2: OpsLevel `opslevel.yml` slice.** Add `OpsLevelManifestEnvelopes`,
  OpsLevel model, fixtures, and provider-specific warning cases. Reuses PR-1's
  envelope/redaction core.
- **PR-3: Cortex `cortex.yaml` slice.** Add `CortexManifestEnvelopes`, model,
  fixtures, scorecard definition/result fact emission, and ambiguous/stale
  reducer-round-trip fixtures (paired with synthetic active/tombstoned repo
  facts) to satisfy acceptance criterion 4 across all six outcomes.
- **PR-4: telemetry + Compose proof.** Producer counters/span, the
  `contract_service_catalog` test additions, and the remote Docker Compose
  end-to-end proof (manifest fixture -> facts -> reducer ->
  `reducer_service_catalog_correlation` -> MCP read), with `No-Regression
  Evidence:` and `Observability Evidence:` notes. Updates
  `collector-reducer-readiness.md` to move service catalog from "design/research
  only" to "fixture-backed producer shipped."

Wiring the producer into a live claim-based runtime path (hosted polling,
discovery-driven file selection) is **deferred** beyond this slice, exactly as
cicdrun is still fixture-only.

## 10. Open Questions for the Principal

1. **CollectorKind name.** Propose `"service_catalog"` as the durable collector
   family name (matches `SourceSystem`/`source_layers` language). Confirm, since
   it becomes a durable identifier in fact provenance and telemetry attributes.
2. **Unread fact families.** The reducer index consumes only entity, ownership,
   and repository_link (plus active repo facts). `dependency`, `api_link`,
   `operational_link`, `scorecard_definition`, and `scorecard_result` are
   emitted but **not yet correlated**. Do we (a) emit them now for read-surface
   completeness and forward compatibility (my recommendation), or (b) defer
   emitting them until the reducer can consume them, to avoid carrying inert
   facts? This affects PR-3 scope.
3. **Manifest discovery vs fixture input.** Resolved for repo-hosted manifests:
   the Git collector now recognizes catalog descriptor basenames during content
   streaming and emits `service_catalog.*` facts. Hosted provider API polling,
   credentials, and claim-based catalog runtimes remain separate follow-ups.
4. **Schema-version ownership.** If a manifest carries a field the reducer needs
   but `ServiceCatalogSchemaVersionV1` does not yet imply, is a `1.x` field
   addition a joint producer+reducer change, or do we hold the line on the
   shipped shape and emit a warning? I lean toward: hold the shape, warn, and
   open a separate schema discussion.
5. **ADR provenance.** The ADR referenced by #563
   (`docs/docs/adrs/2026-05-15-service-catalog-collector.md`) is not in the
   tree. Should this memo be promoted into a real ADR under the current
   convention, or does `collector-reducer-readiness.md` remain the source of
   truth for the boundary?

## 11. Recommendation

**Build-safe. Proceed with PR-1 after principal sign-off**, subject to the
single hard constraint and the open questions above.

Rationale: the entire consumer half (fact kinds, projector intent, reducer
handler/writer, query store, MCP tool, telemetry counter) is already shipped and
provenance-only. This feature is a small, additive, producer-only collector
modeled directly on the proven `cicdrun` fixture-emitter pattern. It introduces
no schema change, no graph write, no Cypher, no new reducer domain, and no new
concurrency primitive — so the dominant correctness risk reduces to two things
the test plan pins down: (1) **payload-key fidelity** to the reducer index,
proven by a reducer round-trip contract test, and (2) **non-over-admission**,
proven by asserting blank `service_id`/`workload_id`/`repository_id` and by the
`rejected`/`unresolved` fixtures for name-only links. With those guards and the
phased plan, the slice is low-risk and decision-ready.

The one item I would not let slide: do **not** approve any variant that lets a
catalog name or owner mint a `repository_id`, `service_id`, or graph node. That
is the boundary the issue, the readiness doc, and `eshu-correlation-truth` all
draw, and it is the only way this feature can produce wrong graph truth.
