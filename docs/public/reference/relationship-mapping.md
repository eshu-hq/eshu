# Relationship Mapping

Eshu relationship mapping turns source evidence into typed cross-repository and
runtime relationships. The important contract is stage ownership: extraction
describes evidence, resolution admits canonical relationship rows, graph writers
materialize edges, and query surfaces shape stories from the materialized truth.

Use this page as the route map. The details are split by job:

- [Relationship Evidence And Resolution](relationship-mapping-evidence.md)
  explains evidence families, canonical relationship types, assertions,
  confidence, and resolver behavior.
- [Relationship Runtime And Stories](relationship-mapping-runtime-stories.md)
  explains graph readiness, runtime topology, deployment overview fields, and
  story shaping.
- [Relationship Mapping Observability](relationship-mapping-observability.md)
  explains logs, traces, metrics, and proof expectations.
- [Relationship Graph Examples](../guides/relationship-graphs.md) gives
  example-driven diagrams for public readers.

## Stage Ownership

| Stage | Owns | Emits | Must not do |
| --- | --- | --- | --- |
| Indexing | parsed files, graph entities, raw source properties | source-local graph and facts | infer cross-repo truth from partial data |
| Evidence extraction | explainable source signals from facts and files | `EvidenceFact` rows with kind, type, confidence, rationale, and details | collapse every signal into `DEPENDS_ON` for convenience |
| Resolution | candidate grouping, assertions, rejection, confidence filtering | `ResolvedRelationship` rows | write graph edges or shape user prose |
| Reducer materialization | canonical workload, platform, dependency, and shared graph writes | Neo4j/NornicDB edges and Postgres readiness rows | invent evidence that did not survive resolution |
| Query and story shaping | concise read-side summaries | `story`, `deployment_overview`, context fields, limitations | create new canonical truth |

If a change feels ambiguous, ask which stage owns the decision before editing.

## End-To-End Flow

```mermaid
flowchart TD
  A["Committed source facts"] --> B["DiscoverEvidence"]
  B --> C["EvidenceFact rows"]
  C --> D["Resolve"]
  D --> E["ResolvedRelationship rows"]
  E --> F["Reducer materialization"]
  F --> G["Canonical graph edges"]
  G --> H["Repository, workload, service, and deployment reads"]
  H --> I["story and deployment_overview"]

  J["Assertions"] --> D
  K["Graph readiness phases"] --> F
```

### Two invocation paths, two payload shapes

`DiscoverEvidence` runs from two places, and they hand it `parsed_file_data`
in different Go shapes:

- **Streaming**, per commit, for every repository scope. The collector embeds
  the parser's in-memory map into the fact envelope verbatim, with no JSON
  round trip, so bucket values arrive as `[]map[string]any`.
- **Deferred backfill**, which reloads facts from Postgres. JSONB decoding
  produces `[]any` of `map[string]any`.

An extractor that reads a bucket must accept both. Asserting one shape
silently yields zero evidence on the other path — and because the streaming
path is the always-on one, that failure is invisible: no error, no log, just
an empty result that looks like a repository with nothing to find.

This is not hypothetical. Eight IaC buckets (Terraform modules, Terragrunt
dependencies and configs, Helm charts and values, ArgoCD applications and
applicationsets, Flux git repositories) asserted `.([]any)` and therefore
produced nothing on the streaming path until #5445. Their evidence appeared
only after a backfill pass. Every test for them used `[]any` fixtures — the
backfill shape — so the suite stayed green throughout.

Read buckets through the typed `DecodeParsedFileData*` accessors in
`sdk/go/factschema`, which handle both shapes. When adding a test for an
extractor, drive it from real parser output rather than a hand-built map, so
the fixture cannot encode a shape production never emits.

## Canonical Relationship Types

The resolver-owned typed relationship enum lives in
`go/internal/relationships/models.go`:

| Type | Meaning |
| --- | --- |
| `DEPLOYS_FROM` | The source deploys artifacts from the target. |
| `DISCOVERS_CONFIG_IN` | The source discovers configuration in the target. |
| `RUNS_ON` | The source runs on the target platform. |
| `PROVISIONS_DEPENDENCY_FOR` | The source provisions infrastructure or configuration for the target. |
| `DEPENDS_ON` | Generic dependency when no more specific relationship is truthful. |
| `USES_MODULE` | The source consumes a target module repository. |
| `READS_CONFIG_FROM` | The source is granted read access to target configuration. |

Related graph edges such as `PROVISIONS_PLATFORM`, `DEFINES`, `INSTANCE_OF`,
`DEPLOYMENT_SOURCE`, and `MATCHES_STATE` are real runtime topology edges, but
they are not resolver-owned relationship types. They are written by
reducer/materializer paths and read by repository, workload, service, and
deployment trace surfaces. Deployment trace responses preserve
`DEPLOYMENT_SOURCE` separately from `DEPLOYS_FROM` and return each row's
canonical `source_id` and `target_id` so clients do not invent a
repository-to-repository edge from instance admission evidence.

## Traversal Rule

Use the direct repository-file edge for flat file lookup:

```text
Repository -[:REPO_CONTAINS]-> File -[:CONTAINS*]-> entity
```

Use `CONTAINS*` from a repository only when the query is genuinely about tree
ancestry or arbitrary descendants. Flat repo-file queries should not walk the
whole directory tree just to locate files.

## Canonical Versus Derived

Canonical truth:

- evidence rows
- resolved relationship rows
- workload/platform/dependency graph edges
- graph readiness rows such as `graph_projection_phase_state`

Derived read-side summaries:

- `deployment_artifacts`
- `delivery_paths`
- `delivery_workflows`
- `delivery_family_paths`
- `delivery_family_story`
- `shared_config_paths`
- `consumer_repositories`
- `relationship_overview`
- `deployment_overview`
- `story`

Derived summaries help answer questions. They must not be used as proof that a
new canonical relationship exists.

## Direction Matters

Write the edge in the direction of the behavior being explained:

- `gitops-control-plane -[:DISCOVERS_CONFIG_IN]-> platform-observability`
- `payments-api -[:DEPLOYS_FROM]-> deployment-charts`
- `terraform-stack-search -[:PROVISIONS_DEPENDENCY_FOR]-> search-api`

If the source is a control plane, keep the control-plane source on the left. If
the source is the deployed workload or service, keep that workload on the left.

## Flux Cross-Repository Deployment Binding

Flux contributes a cross-repository `DEPLOYS_FROM` relationship only when a
`FluxGitRepository.spec.url` is a supported remote URL whose
`repositoryidentity.NormalizeRemoteURL` value equals the catalog `RemoteURL` of
exactly one *other* indexed repository. This is strict remote-URL identity, not
an alias or token match; self, missing, and ambiguous matches emit no
cross-repository evidence.

A deployment trace may attribute target-repository resources to a Flux
Kustomization or HelmRelease only when its `sourceRef.kind` is `GitRepository`,
its effective `(namespace, name)` exactly matches the evidence binding, and
that binding identifies exactly one target repository. An explicit
`sourceRef.namespace` wins; otherwise the controller namespace supplies the
effective namespace. Missing identity, no target, multiple targets, or a
saturated bounded binding read fail closed: the trace does not expand the
controller into a target repository.

Target-resource matching remains path-bounded. The normalized controller root
must contain the resource's safe relative path; `.` and `./` mean the target
repository root and match safe relative paths. Empty, unsafe, or otherwise
unusable roots do not become repository-wide matches.

## Terraform State MATCHES_STATE Edge Existence

`MATCHES_STATE` links a config-declared `TerraformResource` to the
`TerraformStateResource` it matches by exact address equality (#5443). It
exists only while that config resource's Terraform backend resolves to a
single, unambiguous owning repository whose declared address matches the
state resource's address; it does not exist merely because a state resource
was observed.

Backend ownership resolution runs every materialization generation (not only
full reconciliation) and classifies into one of four outcomes, only one of
which preserves a prior edge unexamined:

- **Resolved** — a single repository owns the backend. The edge is written
  (or rewritten, if ownership moved to a different repository since the last
  generation) to that repository's matching config resource; any edge to a
  *different* prior repository is retracted the same cycle.
- **No owner** — no config repository currently declares this backend. Any
  existing edge is retracted; a state resource stays "applied-only" until a
  config declaration reappears.
- **Ambiguous owner** — more than one repository currently declares the same
  backend. Any existing edge is retracted, the same as no owner: ownership is
  not uniquely determined, so no edge should point at either candidate.
- **Transient resolution failure** — the ownership query itself did not
  complete this cycle (for example a Postgres timeout). Any existing edge is
  left untouched: a cycle that learned nothing about ownership must not act
  as though it learned something.

Because ambiguous-owner detection compares each candidate repository's
CURRENTLY-ACTIVE ingestion generation, and repositories ingest independently
and asynchronously, a genuine backend-ownership migration between two
repositories can transiently resolve as ambiguous purely because one
repository's ingestion has not yet caught up to the other's — not because
ownership is actually contested. During that window the edge (and anything
that reads it, including scoped-token infra authorization) can flap: retracted
while ambiguous, rewritten once both repositories' ingestion converges on a
single owner. This is bounded, self-healing, and fails toward absence, never
toward a wrong or extra edge — see `internal/storage/cypher/AGENTS-evidence-history.md`'s
`#5623 P1 follow-up` entry for the full mechanism and why the window is
tighter than the reconciliation interval it replaced.

### Bare Local-Backend Default-Path Resolution And The "unresolved" Finding

Before issue #5594, a bare `backend "local" {}` block with no explicit `path`
attribute — the ordinary way to write a local backend, since Terraform itself
defaults `path` to `"terraform.tfstate"` relative to the backend block's own
directory — produced no config-side candidate at all. Ownership resolution for
that backend was therefore always **No owner**, permanently, regardless of
whether a real owning repository existed: `MATCHES_STATE` could never form for
a bare local backend no matter what. `backendConfigCandidate`
(`go/internal/collector/terraformstate/backend_config.go`) now also derives a
`BackendLocal` candidate, applying Terraform's own default when the attribute
is absent, so these repos can reach **Resolved** and materialize
`MATCHES_STATE` for the first time. This changes which candidates are
*eligible* to compete for ownership; it does not change the
Resolved/No owner/Ambiguous owner/Transient-failure classification logic
above. A `path` present but unresolved (a dynamic expression) still yields no
candidate rather than a guessed locator, same as every other unresolved
backend attribute.

The **No owner** outcome (`tfstatebackend.ErrNoConfigRepoOwnsBackend`) was
log-only at the read edge until #5594: `POST
/api/v0/terraform/config-state-drift/findings` and
`list_terraform_config_state_drift_findings` returned an identical empty page
for "evaluated, no drift" and "ownership never resolved at all," with no way
for a caller to tell the two apart. `TerraformConfigStateDriftWrite` now
persists exactly one durable `outcome: "unresolved"` finding per
state-snapshot scope for this case
(`go/internal/reducer/terraform_config_state_drift_unresolved_owner.go`),
mirroring the pre-existing `"ambiguous"` write in every respect (same
upsert-by-stable-fact-id idempotency, same non-fatal write-failure handling).
This is the caller-visible complement to the graph-level edge retraction this
section already documents: the `MATCHES_STATE` edge silently disappears from
the graph on a **No owner** cycle, and the drift-findings read surface now
says why, distinguishable from a scope that resolved cleanly with zero drift.

## Safe Extension Checklist

Before adding a new mapping family or runtime interpretation:

1. Choose the semantic relationship first.
2. Choose the strongest explainable evidence source.
3. Keep parser output portable and provider-neutral where possible.
4. Emit evidence with stable kind, relationship type, confidence, rationale,
   and details.
5. Let `Resolve` apply assertions, rejections, and confidence filtering.
6. Add positive, negative, and ambiguous tests.
7. Prove graph truth and query/story truth agree.
8. Keep incomplete evidence explicit instead of hiding it behind confident
   prose.

Do not lower `DefaultConfidenceThreshold` or inflate confidence to force an
edge. If the signal is weak, keep it weak and let stronger evidence or an
explicit assertion admit it.

## Relationship Extractor Contribution Kit

Relationship extractors are evidence producers. They do not decide canonical
truth by themselves, and they do not write graph edges directly. A contribution
is ready only when extraction, resolution, graph materialization, and query or
story behavior agree.

Use this checklist:

1. Name the source evidence family and the intended canonical relationship type.
2. Add extractor tests with positive, negative, and ambiguous fixtures.
3. Keep provider-specific parsing in the extractor and relationship admission in
   `Resolve`.
4. Prove resolver behavior when confidence, assertions, rejections, or aliases
   affect the candidate.
5. Prove reducer/materializer behavior before claiming graph truth.
6. Prove the repository, workload, service, deployment, or story surface that
   will expose the relationship.
7. Update this page, the evidence/runtime/story subpage that owns the detail,
   and any affected language page when parser evidence feeds the relationship.
8. Run focused relationship tests, query or reducer tests for surfaced truth,
   `scripts/verify-parser-relationship-kit.sh`, the docs build, and
   `git diff --check`.

Fixture expectations:

| Fixture class | Required proof |
| --- | --- |
| Positive | Strong source evidence emits the intended `EvidenceFact`, survives resolution, and appears on the documented graph/query/story path. |
| Negative | Similar-looking source evidence that should not create a relationship stays absent or rejected with a clear reason. |
| Ambiguous | Multi-target, weak, duplicated, stale, or partial evidence remains unresolved, low confidence, or explicitly limited instead of inventing truth. |

Guardrails:

- Parse-only behavior is not supported query behavior. Parser evidence can feed
  relationships, but the relationship remains unsupported until resolver,
  materializer, and query/story proof exist.
- Dynamic imports, plugin loading, reflection, generated code, framework roots,
  and provider-specific conventions need exact fixtures before they can admit a
  relationship.
- Keep document parsing separate from evidence emission. Turning a source
  document into typed values and building an `EvidenceFact` from those values
  are different jobs, and once a provider family grows they belong in different
  files. Mixing them is how an extractor file ratchets past the 500-line cap a
  few lines at a time: each addition is individually small, and nothing says
  where the next function goes. A test that asserts no function on the parsing
  side returns evidence keeps the seam after the split.
- Do not collapse domain-specific relationships into `DEPENDS_ON` just because
  it is easier to materialize.
- Do not use derived read-side summaries such as `story` or
  `deployment_overview` as canonical relationship proof.
- Keep unsupported or partial source states visible in evidence details,
  limitations, status rows, logs, or query responses.

### Shared repository-catalog derivation

Relationship extractors that match candidates by repository alias resolve those
aliases against a catalog derived from `repository` facts.
`relationships.RepositoryCatalogEntry` (`relationships/catalog.go`) is the single
source of truth for that derivation: it turns a decoded repository fact payload
into a `CatalogEntry` (RepoID plus its aliases). The Postgres streaming commit
path and Ifá's offline derived catalog (#4394) both call it, so a generation's
committed repository identity is computed identically to any catalog derived
offline from the same facts. Do not re-implement repository-alias derivation in
an extractor; use this helper so alias-drift detection compares consistently
shaped aliases.

### Boundary-aware repository reference keys

Deferred relationship backfill loads candidate `content`, `file`, and
`gcp_cloud_relationship` facts by comparing their payload text against the
repository catalog. That comparison must be a superset of the in-memory
relationship matcher without collapsing overlapping repository names such as
`github.com/org/app` and `github.com/org/app-config`.

`relationships.CatalogReferenceKey` and
`relationships.CatalogReferenceTokenStream` provide that contract. They reuse the
same tokenization as the catalog matcher, then wrap tokens with `|` delimiters so
SQL can test whole-token containment instead of using unbounded substring
matches. The Postgres `relationship_reference_candidate_keys` side table stores
those token streams for accepted relationship-candidate facts; the deferred load
query uses the side table first and falls back to the legacy payload predicate
for unkeyed rows.

Any migration or repair path that backfills reference keys must preserve the
same token-boundary behavior: split first, trim `.git` and supported config-file
suffixes only at token boundaries, and keep source-repository self-exclusion in
lockstep with the runtime helper. Add positive and negative proof for prefix
overlap, `.github` repositories, file-suffixed references, and empty `repo_id`
fallbacks before changing this matcher.

## Interpreting Code Relationship Truth In API And MCP Responses

Code relationship reads (`POST /api/v0/code/relationships/story` and the
`analyze_code_relationships` MCP tool) expose uncertainty per edge and per
answer so an agent never has to guess how much to trust a relationship. The
labels are descriptive: they never change the answer-level `TruthEnvelope` and
never upgrade a heuristic or unsupported edge into canonical truth.

### Per-edge provenance

Every relationship row carries a `provenance` block:

| Field | Meaning |
| --- | --- |
| `source_family` | `code_edge` (resolved by the parser/reducer call resolver), `correlation_edge` (correlation evidence), or `unsupported` (no recorded basis). |
| `method` | The resolution mechanism: a code `resolution_method` (ADR #2222: `scip`, `import_binding`, `type_inferred`, `repo_unique_name`, …), a correlation `confidence_basis`, or `unsupported`. |
| `confidence` | Numeric confidence (omitted for a legacy edge with no recorded provenance). |
| `confidence_tier` | Named band derived from `confidence`: `high` (>= 0.9), `medium` (>= 0.7), `low` (otherwise), or `unsupported` (no confidence). A presentation derivation only. |
| `truth_state` | `derived` (canonical code edge with confidence), `heuristic` (correlation or semantic edge — evidence, not canonical), or `unsupported` (no confidence or no basis). |

A `heuristic` or `unsupported` edge is **never** promoted to `derived`. The
`confidence_tier` is a convenience over `confidence`; it does not raise or lower
`truth_state`.

### Per-answer missing-edge reason and truncation

The response `coverage` block explains why a result is empty or short instead of
leaving the caller to guess:

| `missing_edge_reason` | Meaning |
| --- | --- |
| `complete` | All matching relationships were returned. |
| `target_unresolved` | The target did not resolve to a known entity. |
| `no_relationships_found` | The target resolved but has no edges of the requested type and direction. |
| `all_below_confidence_floor` | Edges exist but all fell below the requested `min_confidence`. |
| `truncated_by_limit` | More edges exist than `limit`; raise `limit` or page with `offset`. |
| `truncated_by_token_budget` | Rows were trimmed to fit `token_budget`. |

`truncation_state` reports the capping cause (`none`, `count`, `token_budget`,
or `count_and_token_budget`), and `evidence_explanation` is a bounded
human-readable form of the reason. The MCP `analyze_code_relationships` text
summary surfaces the same derived/heuristic/unsupported counts and the
missing-edge reason; its `structuredContent` is the unchanged HTTP answer, so
API and MCP agree on every label.

## What To Read Next

- Need to change extractors or relationship types:
  [Relationship Evidence And Resolution](relationship-mapping-evidence.md)
- Need to change deployment stories or read-side summaries:
  [Relationship Runtime And Stories](relationship-mapping-runtime-stories.md)
- Need to prove a relationship change:
  [Relationship Mapping Observability](relationship-mapping-observability.md)
