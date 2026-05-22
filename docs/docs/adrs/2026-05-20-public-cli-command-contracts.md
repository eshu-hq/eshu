# ADR: Public CLI Command Contracts

**Date:** 2026-05-20
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- Issue #475: align public-site claims and examples with shipped surfaces
- Issue #476: implement `eshu scan`
- Issue #477: implement `eshu trace service`
- Issue #478: implement `eshu map --from`
- Issue #479: implement `eshu docs verify`
- Issue #480: define public CLI contracts for site-advertised commands
- `../reference/truth-label-protocol.md`
- `2026-05-09-documentation-truth-collectors-and-actuators.md`
- `2026-05-14-service-story-dossier-contract.md`
- `2026-05-14-mcp-tool-contract-performance-audit.md`

---

## Context

The public site needs a simple command story that matches how people talk
about Eshu:

```bash
eshu scan
eshu trace service checkout
eshu map --from terraform/aws_lb.main
eshu docs verify
```

Those commands are good product language, but they are not allowed to become
demo-only aliases. Eshu is a code-to-cloud truth platform. Public commands must
preserve the same accuracy, performance, and reliability rules as the API and
MCP surfaces.

The current CLI already has strong lower-level surfaces:

- `eshu index`, `eshu workspace index`, `eshu watch`, and admin reindex flows
  drive repository ingestion.
- `eshu find`, `eshu analyze`, API routes, and MCP tools read code, content,
  relationship, and impact data.
- `trace_deployment_chain`, `trace_resource_to_code`, `get_service_story`,
  and `investigate_service` already model parts of the code-to-runtime story.
- Documentation findings and evidence packets are designed in the query layer
  and the documentation-truth ADR, but a public CLI verification workflow is
  not yet complete.

This ADR turns the site-facing command names into contracts that future PRs can
implement without creating a second truth path.

---

## Decision

Eshu will treat the four site-facing commands as first-class public CLI
contracts. They must be implemented over the same canonical data-plane,
query-layer, API, and MCP contracts that already power the platform.

All four commands must provide:

- human-readable output for operators;
- `--json` output with stable fields for automation;
- explicit truth metadata: truth level, profile, freshness, evidence IDs,
  warnings, and partial or unsupported state;
- deterministic exit codes for success, partial, ambiguous, stale,
  unsupported, and failed outcomes;
- bounded execution: scope first, default limits, explicit timeouts,
  deterministic ordering, and truncation or continuation metadata;
- evidence-first wording that distinguishes exact, derived, ambiguous, stale,
  unsupported, and incomplete results;
- performance evidence before site or docs examples present the command as
  generally available.

CLI commands remain consumers or orchestrators. They must not introduce a
separate source of truth from the API, MCP, graph, content store, fact store,
or runtime status surfaces.

---

## Shared Output Contract

Human output should lead with the operational answer, then evidence and
warnings. JSON output must follow the existing canonical envelope from the
[Truth Label Protocol](../reference/truth-label-protocol.md): top-level
`data`, `truth`, and `error` only. Command status, scope, warnings, evidence
handles, and truncation metadata belong inside `data`.

```json
{
  "data": {
    "status": "success",
    "command": "trace_service",
    "scope": {},
    "result": {},
    "evidence": {
      "ids": [],
      "packets": [],
      "truncated": false
    },
    "warnings": []
  },
  "truth": {
    "level": "exact",
    "profile": "production",
    "freshness": {"state": "fresh"},
    "capability": "platform_impact.deployment_chain"
  },
  "error": null
}
```

The exact fields can be refined by implementation, but every command must keep
these concepts visible inside the canonical envelope. This ADR does not
supersede the truth-label protocol. A command must not return a confident empty
result when the real state is ambiguous, stale, unsupported, incomplete, or
unreadable.

## Exit Codes

Implementation PRs should keep exit codes stable across the four commands:

| Code | Meaning |
| --- | --- |
| `0` | Complete success for the requested scope. |
| `1` | Runtime or unexpected command failure. |
| `2` | Invalid input, missing required scope, or unsupported flag combination. |
| `3` | Ambiguous input; caller must provide a narrower selector. |
| `4` | Stale, building, or incomplete index state prevents a definitive answer. |
| `5` | Partial result was produced but requested `--fail-on` policy rejects it. |
| `6` | Capability unsupported in the active profile or runtime. |

Commands may add more specific internal error codes in JSON, but shell scripts
should be able to rely on the broad exit-code classes above.

---

## Command Contracts

### `eshu scan`

`eshu scan [path]` means: make the requested source queryable, then prove it is
queryable.

The command must:

- detect a repository, workspace, or configured source root;
- preflight graph backend, Postgres/content store, schema readiness, runtime
  owner state, and discovery root;
- run the existing ingestion/indexing path rather than a new scanner;
- wait for collection complete, source-local projection complete, reducer and
  shared projection queue drain, and zero failed or dead-lettered work;
- print collector-complete, projection-complete, and queue-zero timings as
  separate values;
- surface backend, profile, schema/bootstrap state, retrying work, failed work,
  and dead letters;
- exit non-zero on partial or failed states unless the caller explicitly allows
  partial output.

Accuracy requirement: collection completion is not query readiness. The command
may not report success until the graph and content query surfaces are ready for
the requested scope.

Performance requirement: implementation must capture small, medium, and
representative large-run timing. Full-corpus proof must report collector,
projection, and queue-zero timing separately.

Implementation note for issue #476: the first `eshu scan` PR implements the
correct readiness gate over the existing `bootstrap-index` runtime and
`/api/v0/status/pipeline`, and it probes `/api/v0/repositories?limit=1` before
and after the run so a status-only API cannot be mistaken for a queryable
source. It reports bootstrap completion and queue-zero timings.
Collector-complete and source-local projection-complete timings are rendered as
explicit `null` values with warnings until the bootstrap/runtime status
contract exposes those milestones as structured parent-process fields. This is
intentional: the CLI must not fabricate precision from child logs.

Performance Evidence: focused implementation proof is command-level only:
`go test ./cmd/eshu -run 'TestRunScan' -count=1` covers preflight, bootstrap
dispatch, readiness waiting, dead-letter failure, and JSON output without
changing worker counts, batches, queue shape, graph writes, or reducer logic.
Representative small/medium/large runtime timings remain required before
public examples present `eshu scan` as a measured throughput claim.

No-Observability-Change: `eshu scan` reuses existing operator signals from
`/api/v0/status/pipeline`: `health.state`, `health.reasons`, queue outstanding,
pending, in-flight, retrying, failed, dead-letter counts, generation history,
stage summaries, and domain backlogs. The command adds no new runtime worker,
collector, graph query, metric, span, or log path.

Continuation note for issue #502: `eshu scan [path]` now owns the child
bootstrap source-mode contract for the requested local path. The command
resolves the source root, sets `ESHU_REPO_SOURCE_MODE=filesystem`,
`ESHU_FILESYSTEM_ROOT=<resolved root>`, `ESHU_FILESYSTEM_DIRECT=true`, and
`ESHU_REPOS_DIR=<workspace cache>/repos` for `eshu-bootstrap-index`, then leaves
readiness proof on the existing `/api/v0/status/pipeline` and query-probe
checks. This prevents a local scan from falling back to GitHub App auth when the
caller did not manually export collector internals.

No-Regression Evidence: focused TDD first reproduced the missing child
bootstrap filesystem env in
`go test ./cmd/eshu -run TestRunScanRunsBootstrapAndWaitsForHealthyPipeline -count=1`,
then passed after the CLI supplied filesystem mode, root, direct mode, and a
cache-backed repos dir while preserving the existing discovery-report handoff.
The change does not alter worker counts, queue claim behavior, graph writes,
projector execution, or readiness semantics.

No-Observability-Change: the change only corrects bootstrap child environment
selection. Operators still diagnose scan readiness through
`/api/v0/status/pipeline`, the pre/post `/api/v0/repositories?limit=1` query
probe, bootstrap logs, queue counts, generation history, and the existing
collector/projector structured logs.

### `eshu trace service <name>`

`eshu trace service <name>` means: explain how a service gets from code to
deployed runtime.

The command must:

- resolve `<name>` across service, workload, repository, deployable-unit, and
  runtime identifiers;
- return disambiguation candidates when more than one entity matches;
- require selectors such as `--repo`, `--env`, or `--service-id` when the name
  alone is not enough;
- use a bounded service trace or service dossier API contract;
- return owning repo, code entry points where known, CI/CD path, image or
  package path, deployment config, runtime workload/resource, cloud
  dependencies, and evidence citations;
- mark missing, stale, derived, and ambiguous links explicitly.

Accuracy requirement: runtime truth cannot be inferred from repository name
alone. Deployment evidence, config evidence, and dependency evidence must stay
separate unless a reducer-owned relationship proves the connection.

Performance requirement: implementation must resolve the smallest service or
workload scope before graph traversal, cap section sizes, expose truncation, and
benchmark against representative whole-organization data.

Implementation note for issue #477: the first `eshu trace service <name>` PR
adds the public CLI consumer over the existing bounded service-story dossier
route. The command calls `GET /api/v0/services/{service_name}/story` with
`Accept: application/eshu.envelope+json`, renders an operator summary from the
service identity, repository, deployment lanes, runtime instances,
upstream/downstream counts, investigation coverage, and limitations, and passes
the canonical envelope through unchanged with `--json`. It also makes
service-story not-found responses honor the canonical envelope when requested.

No-Regression Evidence: focused command and query tests cover trace command
registration, envelope request headers, unsupported environment selector
guarding, human output, JSON passthrough, null `truth` on synthetic transport
errors, unsupported-capability exit classification, and canonical not-found
service-story errors:
`go test ./cmd/eshu -run 'TestTraceService|TestFetchTraceService' -count=1`
and
`go test ./internal/query -run 'TestGetServiceStoryReturnsEnvelope' -count=1`.
This change does not introduce a new graph traversal, worker, queue,
collector, batch, or write path; it reuses the service-story route's scoped
query contract.

No-Observability-Change: the command is a read-only CLI wrapper over
service-story. Operators still diagnose slow or stale traces through the
existing `service_query.stage_started` / `service_query.stage_completed`
structured logs for `operation=service_story`, plus existing `neo4j.query` and
`postgres.query` spans for graph and content reads. The CLI adds no runtime
metric, span, log, or status surface.

Continuation note for issue #477: the resolver now performs a bounded candidate
pass before story enrichment. The service-story API accepts `service_id`,
`repo`, and `environment` query parameters, resolves repository selectors to
canonical repository ids, and returns `409` with
`error.code=ambiguous` plus `error.details.candidates` when a display name
still matches multiple workloads. `eshu trace service` exposes the same
selectors as `--service-id`, `--repo`, and `--env`; ambiguous names print the
candidate service ids and exit `3` instead of tracing whichever workload the
backend returns first.

No-Regression Evidence: focused resolver tests cover duplicate-name ambiguity,
repository selector narrowing, environment selector narrowing, exact
`service_id` selection, CLI query-param forwarding, and human ambiguity output:
`go test ./cmd/eshu -run 'TestTraceService|TestFetchTraceService' -count=1`
and `go test ./internal/query -run 'TestGetServiceStory' -count=1`. The
candidate pass uses bounded exact-property `Workload` / `WorkloadInstance`
lookups with `LIMIT 11` before the existing exact workload-id enrichment path;
it avoids cross-property `OR` and does not add a worker, queue, write path, or
unbounded traversal.

Observability Evidence: the resolver still runs inside the existing
`service_story` request and emits the existing
`service_query.stage_started` / `service_query.stage_completed` structured logs.
This continuation adds a `service_candidate_lookup` stage before enrichment, so
operators can see whether a trace stopped at ambiguity or continued through the
exact workload-id path. Existing `neo4j.query` spans still expose the backing
candidate lookup, workload lookup, repository lookup, instance lookup, graph
reads, Postgres reads, and response assembly cost.

Continuation note for issue #477: the service-story dossier now includes
`code_to_runtime_trace`, a bounded synthesis over the already-scoped dossier
fields. It reports service identity, code/API entrypoint, CI/CD, image/package,
deployment-config, runtime, and cloud-dependency segments with explicit
`exact`, `derived`, or `missing_evidence` status; the CLI human renderer prints
the same segment status before the count summary.

No-Regression Evidence: focused TDD covers the query contract, OpenAPI field,
and CLI human output:
`go test ./internal/query -run 'TestBuildServiceStoryResponseIncludesCodeToRuntimeTrace|TestOpenAPISpecServiceStoryExposesDossierFields' -count=1`
and
`go test ./cmd/eshu -run TestRunTraceServiceRendersOperationalSummary -count=1`.
This continuation adds no new graph read, Cypher shape, queue, worker, or write
path; it only synthesizes the response from existing scoped service-story data.

No-Observability-Change: the synthesis runs after existing service-story
enrichment. Operators still diagnose slowness or staleness through
`service_query.stage_started` / `service_query.stage_completed`, `neo4j.query`,
`postgres.query`, truth envelope metadata, and the response-level segment
statuses plus `missing_segments`.

### `eshu map --from <thing>`

`eshu map --from <thing>` means: start from one known entity and show its
bounded code/cloud neighborhood with evidence.

The command must:

- accept supported handles such as Terraform addresses, ARNs, Kubernetes object
  references, image refs, package refs, repo IDs, file paths, workload IDs,
  service IDs, and graph entity handles;
- normalize the input to one canonical entity handle or return
  disambiguation choices;
- prefer typed relationship, resource, and impact routes before any generic
  graph traversal;
- group output into sections such as `defined_by`, `deployed_by`, `runs_as`,
  `depends_on`, `consumed_by`, and `evidence`;
- support `--depth`, `--limit`, `--relationship`, `--env`, `--repo`, and
  `--json`;
- include truncation and freshness metadata.

Accuracy requirement: a string match is not a resolved entity. Cloud-only,
config-only, derived, candidate, and exact evidence must be labeled.

Performance requirement: input resolution must happen before relationship
expansion. Default traversal depth and result sizes must be bounded.

Implementation note for issue #478: the first `eshu map --from <thing>` PR adds
the public CLI consumer over `POST /api/v0/impact/entity-map`. The route
normalizes supported prefixes such as `terraform/<address>`, resolves the input
with exact typed label/property probes, returns ambiguity candidates before
traversal, and only expands outgoing/incoming relationships after one typed
start anchor is selected. The default graph expansion is depth 1 with cap 4 and
limit 25 with cap 100. The first supported handle families are workloads and
service names, workload instances, repositories, cloud resources, Terraform
resources/data sources/modules, Kubernetes resources, and graph file paths.
Image refs, package refs, and cloud-only runtime handles remain future handle
families unless they already materialize into one of those typed graph nodes.
Entity-map traversal uses relationship-family query shapes instead of raw
whole-neighborhood expansion. The default `depth=1` path uses direct typed
adjacency reads and repository anchors skip structural `CONTAINS` /
`REPO_CONTAINS`, outgoing repository, and code-edge `CALLS` / `IMPORTS` fanout
unless callers pass an exact `relationship` filter.

No-Regression Evidence: focused API and CLI tests cover command registration,
canonical envelope POST behavior, JSON passthrough, ambiguous-input exit code,
stale-freshness exit code, Terraform address normalization, no whole-graph
resolver scan, typed Workload and TerraformResource traversal anchors, bounded
depth/limit query shape, relationship-family repository traversal, explicit
relationship filters, grouped output sections, and traversal suppression on
ambiguity:
`go test ./cmd/eshu -run 'TestMapFrom|TestFetchEntityMap|TestRunMapFrom' -count=1`
and `go test ./internal/query -run 'TestEntityMap' -count=1`. This PR adds a
bounded read route and CLI wrapper only; it does not add graph writes, queues,
workers, collectors, or batch-processing paths.

Observability Evidence: the API route wraps requests in the new
`query.entity_map` span with `http.route=/api/v0/impact/entity-map` and
`eshu.capability=platform_impact.entity_map`. Existing graph query spans expose
the resolver and outgoing/incoming traversal cost. The response includes
`resolution.status`, selected anchor metadata, `coverage.query_shape`,
`coverage.depth`, `coverage.limit`, evidence relationship counts, relationship
filter, and truncation so an operator can distinguish empty, ambiguous, slow,
or truncated maps without guessing from CLI text.

### `eshu docs verify`

`eshu docs verify [path]` means: extract documentation claims, compare them
against code/API/deployment/cloud truth, and produce durable findings with
evidence packets.

The command must:

- inventory supported documentation sources such as Markdown, docs site
  content, READMEs, runbooks, OpenAPI docs, and later external documentation
  collectors;
- extract checkable claims, including CLI commands, environment variables,
  service names, endpoints, deployment paths, Terraform/Kubernetes/cloud
  references, package/image names, ownership claims, and runbook instructions;
- normalize those claims into durable documentation facts;
- compare claims against canonical truth from the CLI command tree, OpenAPI and
  query routes, graph relationships, collector/runtime facts, and content
  store;
- produce findings such as `valid`, `stale`, `missing_evidence`,
  `contradicted`, `ambiguous`, `unsupported_claim_type`, and
  `inaccessible_evidence`;
- write evidence packets so CLI, API, and MCP consumers read the same truth;
- support `--fail-on`, `--scope`, `--repo`, `--limit`, and `--json`.

Accuracy requirement: a document is not valid merely because it was parsed. A
specific claim is valid only when checked against a matching truth source.
Unsupported claim types must be labeled unsupported.

Performance requirement: claim extraction must be bounded by scope, file count,
content size, fingerprints, and batching. Large documentation sets need
progress/status and stop thresholds.

Implementation evidence for issue #479 first slice: `eshu docs verify [path]`
now scans local Markdown-family documentation with `--limit` and
`--max-bytes`, extracts explicit Eshu CLI command claims, HTTP endpoint claims,
`ESHU_*` environment variable claims, and known unsupported shell-command
claims, then emits documentation finding and evidence-packet fact envelopes in
memory. This slice validates CLI claims against the current Cobra command tree,
HTTP endpoint claims against the generated OpenAPI path inventory, and
environment claims against the documented Eshu env-var allowlist. It does not
yet verify cloud, Kubernetes, Terraform, package/image ownership, or external
documentation collector claims; those remain unsupported rather than passed.

Implementation evidence for issue #479 persistence slice: `eshu docs verify
--persist` now writes generated `documentation_finding` and
`documentation_evidence_packet` envelopes through the Postgres fact ingestion
boundary under a `documentation_source` scope. The command computes a bounded
document fingerprint from the inventoried Markdown-family file revisions and
checks the newest pending or active persisted generation before re-running
claim comparison. If the fingerprint is unchanged, it loads the stored finding
and evidence-packet facts and applies `--fail-on` to those persisted findings
instead of silently passing the command.

Implementation evidence for issue #479 read-surface slice: the existing
documentation findings API and MCP tool now accept `scope_id`, `generation_id`,
and `repo` filters so callers can target one persisted `eshu docs verify
--persist` run instead of scanning every documentation finding. The Postgres
read model enriches each listed finding with its persisted `scope_id`,
`generation_id`, and repository metadata from `ingestion_scopes.metadata.repo`
when present. This keeps the CLI writer, API route, and MCP tool on the same
durable fact identity without adding graph reads or a second truth path.

Implementation evidence for issue #479 local-path truth slice: `eshu docs
verify [path]` now extracts explicit backticked local repository paths and
Markdown local-link targets for Terraform, Kubernetes/YAML, Helm, Docker
Compose, HCL, JSON, and TOML-shaped files. The CLI supplies a bounded
filesystem truth resolver rooted at the nearest Git worktree root, or the
current working directory when no Git root is available. Existing paths produce
`valid` documentation findings and missing paths produce `contradicted`
findings; remote links and broad prose remain out of scope. This checks local
code/deployment path claims without opening Postgres, graph, cloud, or
Kubernetes clients.

Implementation evidence for issue #479 local image-reference truth slice:
`eshu docs verify [path]` now extracts explicit tagged or digested container
image refs from backticked claims and `image:` documentation snippets. The CLI
supplies a bounded local deployment-manifest resolver rooted at the nearest Git
worktree root. It scans Dockerfile, YAML, JSON, and TOML manifest-shaped files
while skipping generated/vendor trees. Image refs found in local manifests
produce `valid` documentation findings, absent refs produce `contradicted`
findings, and incomplete local scans produce `missing_evidence` rather than a
false pass. This slice checks local code/deployment evidence only; registry
ownership, cloud runtime presence, and OCI digest freshness remain future
API/collector truth work.

Implementation evidence for issue #479 container-image API truth slice:
`eshu docs verify [path] --image-truth api` now checks explicit container image
refs against the reducer-owned container image identity read model at
`GET /api/v0/supply-chain/container-images/identities?image_ref=<ref>&limit=1`.
`--image-truth auto` keeps the local-manifest truth source unless
`--service-url`, `--api-key`, `--profile`, `ESHU_SERVICE_URL`, or
`ESHU_API_KEY` explicitly selects remote API context. The command resolves
`auto` to the effective `local` or `api` truth source before persistence is
prepared. API hits produce `valid` findings only when the active read model
returns at least one identity row for the exact image ref; empty API pages
produce `contradicted`, and API transport or backend errors produce
`missing_evidence`. The docs-verification freshness fingerprint includes that
effective image truth mode so persisted local-image findings are not reused for
API-image verification runs.

No-Regression Evidence: focused gates for the first slice are
`go test ./internal/doctruth -run 'TestVerifier' -count=1`,
`go test ./cmd/eshu -run 'TestDocsVerify' -count=1`, and
`go test ./internal/mcp -run 'TestDocumentationToolsAreRegisteredAndRouted|TestReadOnlyTools|TestDocumentationTools' -count=1`.
The persistence slice adds
`go test ./cmd/eshu -run 'TestRunDocsVerifyPersist' -count=1` and
`go test ./internal/storage/postgres -run 'TestIngestionStoreCurrentScopeGeneration' -count=1`.
The read-surface slice adds
`go test ./internal/query -run 'TestDocumentationHandlerPassesPersistedScopeFilters|TestBuildDocumentationFindingsSQLFiltersPersistedScopeIdentity|TestDocumentationHandlerListsScopeMetadataInFindings' -count=1`
and
`go test ./internal/mcp -run 'TestListDocumentationFindings' -count=1`.
The local-path truth slice adds
`go test ./internal/doctruth -run 'TestVerifierComparesLocalPathClaims' -count=1`
and
`go test ./cmd/eshu -run 'TestRunDocsVerifyChecksLocalPathClaims' -count=1`.
The local image-reference truth slice adds
`go test ./internal/doctruth -run 'TestVerifierComparesContainerImageClaims|TestVerifierMarksContainerImageUnsupportedWithoutResolver|TestContainerImageRefsFromTextIsConservative' -count=1`
and
`go test ./cmd/eshu -run 'TestRunDocsVerifyChecksContainerImageClaims' -count=1`.
The container-image API truth slice adds
`go test ./cmd/eshu -run 'TestRunDocsVerifyChecksContainerImageClaimsAgainstAPITruth|TestRunDocsVerifyMarksAPIImageTruthErrorsMissingEvidence|TestDocsVerifyFreshnessIncludesEffectiveImageTruthMode' -count=1`.
The covered input shape is local Markdown documents with explicit command,
endpoint, env-var, local repo path, local container image ref, and unsupported
shell-command snippets.
Bounds are file-count and per-file byte limits plus one exact `stat` probe per
extracted local path candidate, one scope-generation freshness lookup, and one
kind-filtered fact read on unchanged persisted input; read-surface lookups
remain bounded by explicit filters, `limit`, and cursor offset. Local
image-reference truth adds a bounded manifest scan of at most 2000
manifest-shaped files and 512 KiB per manifest file before claim comparison.
API image truth adds at most one `limit=1` container-image identity API lookup
per unique extracted image ref in the bounded documentation input, with per-run
cache reuse. Graph writes, backend Cypher, cloud clients, Kubernetes clients,
registry clients, and runtime worker counts are unchanged.

Observability Evidence: this slice is a local CLI/documentation-fact generator,
so no new long-running runtime metric is required. Operator diagnosis uses the
CLI summary counters (`documents_scanned`, `bytes_scanned`, `claims_checked`,
`valid`, `contradicted`, `missing_evidence`, `unsupported_claim_type`,
`truncated`), generated finding statuses, the JSON `persistence` block
(`enabled`, `persisted`, `skipped`, `scope_id`, `generation_id`, and
`freshness_hint`), Postgres ingestion commit stage logs for persisted writes,
and the existing documentation read route spans
`query.documentation_findings`, `query.documentation_evidence_packet`, and
`query.documentation_packet_freshness` when API or MCP reads the stored facts.
The read-surface filter slice reuses those same route spans plus the existing
Postgres `db.operation=list_documentation_findings` span; no new metric label
or runtime worker signal is required because the change only narrows a bounded
fact-record read.
The API image truth slice additionally reuses the container image identity
route span `query.container_image_identities`, the HTTP response envelope
truth metadata, and the docs verifier finding counters; no new metric label is
needed because the route already exposes the reducer-owned image identity read
model with explicit `limit`, `count`, and `truncated` fields.

---

## Implementation Order

1. Land this ADR and update the public issue tracker.
2. Implement `eshu scan` as the readiness contract for issue #476.
3. Implement `eshu trace service` on top of the service story/investigation
   contract for issue #477.
4. Implement `eshu map --from` using typed entity resolution and bounded
   relationship routes for issue #478.
5. Implement `eshu docs verify` as a documentation-truth pipeline for issue
   #479.
6. Update `geteshu.com` examples only after the matching command exists and
   passes focused plus representative runtime proof.

The command PRs may be separate, but they should inherit this ADR's output,
exit-code, truth, freshness, evidence, performance, and observability rules.

---

## Rejected Options

### Ship Cosmetic Aliases First

Rejected. A command that looks polished but does not prove readiness or truth
would make Eshu less trustworthy.

### Put Product Logic In CLI Only

Rejected. CLI-only orchestration would drift from HTTP, MCP, and Console. The
query layer owns read contracts. The data plane owns ingestion and projection.

### Let `docs verify` Read Existing Findings Only

Rejected as the final command contract. Reading existing findings is useful for
drill-downs, but `docs verify` promises active verification. It must extract or
refresh claims, compare them against truth, and persist findings/evidence.

### Allow Whole-Graph Defaults For Convenience

Rejected. If a command needs a service, repo, resource, environment, or entity
scope, it must resolve or request that scope before running expensive reads.

---

## Bounds And Observability

This ADR is design-only. It does not add new runtime paths, graph queries, or
collectors.

Implementation PRs that add graph-backed reads, documentation verification
stages, queue-backed work, or runtime orchestration must include a performance
impact declaration and tracked evidence note. At minimum, that note must name:

- affected stage;
- expected cardinality;
- scope and limit;
- backend/profile;
- timeout;
- result count and truncation behavior;
- small/medium/large proof ladder;
- stop threshold;
- metrics, spans, logs, status fields, or pprof output used for diagnosis.

No-Observability-Change is allowed only when an implementation PR names the
existing signals that already diagnose that path.

---

## Acceptance Criteria

This ADR is accepted when:

- issue #480 references this ADR as the shared command contract;
- the CLI reference marks the four commands as planned, not shipped;
- each implementation issue references this ADR;
- future command PRs follow the shared output, exit-code, truth, freshness,
  performance, and observability contract;
- public-site examples are not updated until the matching command is real,
  tested, and measured.
