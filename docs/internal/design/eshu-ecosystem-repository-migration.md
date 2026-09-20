# Eshu ecosystem repository plan

Status: approved direction

Audience: Eshu maintainers and ecosystem repository owners

Scope: repository ownership, public contracts, migration order, cutover, and rollback

## Decision

Eshu is an ecosystem, not a permanently growing monorepo.

Every non-default collector will move to its own repository. Readiness controls
the order and timing of each move; it does not decide whether the move happens.

The `eshu` repository retains:

- the default Git collector;
- repository discovery and parsing required by that collector;
- the parser and reducer/resolution core; and
- the admission, projection, persistence, recovery, schema-bootstrap, and
  one-shot repair implementation required to operate that core.

Other deployed runtimes move to named ecosystem repositories. Public collector
and fact-schema contracts move to `eshu-sdk`. A package directory is an internal
ownership boundary; it is not automatically a repository or a service.

This decision supersedes the earlier conditional split. The migration is a
committed program with required verification at every cutover.

## Ownership rules

Each capability has exactly one implementation owner and each public contract
has exactly one publishing owner.

1. Collector repositories observe source truth and emit versioned facts.
2. Collector repositories do not receive Postgres, graph, queue, lease,
   reducer, API, or MCP implementation handles.
3. Eshu core owns fact admission, canonical identity, projection, retraction,
   persistence, recovery, and graph truth.
4. Read runtimes consume supported core contracts. They do not own canonical
   writes because they display the result.
5. Provider-specific collector clients, collection logic, and private helpers
   move with their collector. Provider-specific parsing, reduction, and
   canonical projection stay with the core owner. Shared public schemas move to
   `eshu-sdk`.
6. Directory nesting describes responsibility. The repository map below, not
   path depth, declares the future repository boundary.

## Repository map

The target names below are the canonical destinations. A repository can contain
more than one package or binary when they share one release and operational
owner.

| Repository | Ownership |
| --- | --- |
| `eshu` | Default Git collector; repository discovery; parser; reducer/resolution; canonical admission, projection, storage, recovery, bootstrap, and repair implementation. |
| `eshu-sdk` | Public collector protocol and public fact-schema contracts, published as independently versioned Go modules. |
| `eshu-ingester` | Intake orchestration that is not part of the default Git collector or parser implementation. |
| `eshu-api` | HTTP query, search, semantic retrieval, answer construction, read-model contracts, and supported admin or policy mutation endpoints. Canonical mutation persists through core-owned admission contracts. |
| `eshu-mcp` | MCP protocol handling, tool registration, dispatch, and adaptation to supported read contracts. |
| `eshu-cli` | User-facing CLI commands and their supported API/MCP clients. |
| `eshu-workflow` | Workflow planning, claim coordination, scheduling, and workflow-owned state. |
| `eshu-webhook` | Webhook admission, verification, normalization, and delivery to supported intake contracts. |
| `eshu-extension-host` | Component installation, trust checks, bounded execution, and collector result validation. |
| `eshu-scanner-worker` | Scanner execution and scanner-specific worker lifecycle. |
| `eshu-deploy` | Helm, Compose, GitOps examples, deployment defaults, and cross-repository compatibility matrices. |

The API, MCP, and CLI repositories remain separate even when they expose the
same capability. Shared response or request shapes belong to an explicit public
contract, not to whichever adapter happens to import them first.

Core-only bootstrap and repair commands remain in `eshu` because they directly
operate core-owned schema and recovery state. They are utilities of the retained
core, not independently released services.

## Collector repositories

Each non-default collector gets one repository and one release stream.
Repository names follow the existing executable vocabulary.

| Current collector runtime | Destination repository |
| --- | --- |
| `collector-aws-cloud` | `eshu-collector-aws-cloud` |
| `collector-azure-cloud` | `eshu-collector-azure-cloud` |
| `collector-cicd-run` | `eshu-collector-cicd-run` |
| `collector-confluence` | `eshu-collector-confluence` |
| `collector-gcp-cloud` | `eshu-collector-gcp-cloud` |
| `collector-grafana` | `eshu-collector-grafana` |
| `collector-jira` | `eshu-collector-jira` |
| `collector-kubernetes-live` | `eshu-collector-kubernetes-live` |
| `collector-loki` | `eshu-collector-loki` |
| `collector-oci-registry` | `eshu-collector-oci-registry` |
| `collector-package-registry` | `eshu-collector-package-registry` |
| `collector-pagerduty` | `eshu-collector-pagerduty` |
| `collector-prometheus-mimir` | `eshu-collector-prometheus-mimir` |
| `collector-sbom-attestation` | `eshu-collector-sbom-attestation` |
| `collector-security-alerts` | `eshu-collector-security-alerts` |
| `collector-tempo` | `eshu-collector-tempo` |
| `collector-terraform-state` | `eshu-collector-terraform-state` |
| `collector-vault-live` | `eshu-collector-vault-live` |
| `collector-vulnerability-intelligence` | `eshu-collector-vulnerability-intelligence` |

`collector-git` is intentionally absent from the table because it is
`core_by_design`. Component-extension hosting and scanner execution are runtime
owners, not collector source families, so they use the repositories in the
runtime map.

New non-default collectors start from the collector repository template. They
do not add a temporary implementation to `eshu` first.

## Supporting package ownership

Packages without a matching executable still need an explicit owner. Current
production callers and package contracts establish these destinations:

| Current package or responsibility | Destination |
| --- | --- |
| `collector/servicecatalog` | Retained `eshu` Git/parser core. It normalizes repository-hosted manifests selected by Git; a future hosted catalog API collector is a different producer. |
| `preflight/archive`, `preflight/diagram`, and `preflight/ooxml` | Retained `eshu` parser core because Git document parsing uses them. |
| `preflight/pdf` | Retained `eshu` parser core as a dormant format-safety classifier; it has no production caller today. |
| `documentationexport` and `exportmanifestpreflight` | `eshu-ingester` document-import capability. They have no production caller today. |
| `mediadoc` and `preflight/media` | `eshu-ingester` document-import capability. Hosted activation remains disabled until sandbox and runtime proof pass. |
| `ocrdoc` and `preflight/picture` | `eshu-ingester` document-import capability. Engine execution and limits move with that intake runtime. |
| `sbomdocument` | `eshu-collector-sbom-attestation`; the production SBOM runtime calls it. |
| `ospackagevulnerability` and `osruntime` | `eshu-scanner-worker`; scanner-worker image and root-filesystem analysis call them. |
| `secretsiam` public source-fact shapes | `eshu-sdk` fact-schema support. Provider acquisition moves with each collector; effective-access interpretation stays in Eshu core. |
| `collector/sdk` reusable HTTP, retry, TLS, and safe-error helpers | `eshu-sdk` collector helper module. |
| Generic `contracttest` fact checks | `eshu-sdk` conformance tooling after the AWS-specific helpers are separated. |
| AWS-specific `contracttest` helpers | `eshu-collector-aws-cloud` test support unless generalized first. |
| `parity` real claim-service harness | Retained core proof initially. Only neutral scenarios and expectations may move to `eshu-sdk`. |
| `entrypoints` reusable generator | `eshu-sdk` tooling after its templates stop importing core Postgres, workflow, and runtime implementation. |
| Provider entrypoint manifests and generated commands | Their owning collector repositories. |

The dormant document-import assignments name future ownership; they do not
claim those packages are deployed or production-ready. The `parity` harness
uses a simulated readback model, so it is not reducer/API production proof.

## Public contracts in `eshu-sdk`

`eshu-sdk` publishes two independently versioned Go modules:

- the collector SDK, including the bounded claim/config request, result,
  diagnostics, compatibility, and error contracts; and
- the fact-schema module, including envelopes, fact kinds, schema versions,
  stable-key rules, source scope, confidence, provenance, and compatibility
  metadata needed by producers.

Independent versioning prevents an SDK transport change from forcing a schema
release, or a new fact kind from forcing an unrelated collector runtime change.
The compatibility matrix still tests supported pairs.

Public modules must not import `go/internal/...`. Internal storage ports,
reducers, graph writers, queue records, and deployment configuration are not
SDK surfaces.

## First-party producer delegation

Extracted first-party collectors must be allowed to emit approved core-owned
fact kinds without copying or redefining those kinds.

The core issues a producer identity and an allowlist of fact kinds and schema
versions. Admission validates the producer, source scope, schema, stable key,
generation, payload bounds, and redaction policy before accepting a fact.

Delegation is explicit, versioned, observable, and revocable. It does not
transfer canonical ownership. A collector cannot mint a new canonical identity,
write graph edges, update queue rows, or access core databases directly.

Compatibility covers at least:

- an older collector against a newer supported core;
- a newer collector against the oldest supported core;
- duplicate delivery and retry;
- revoked or expired producer authority;
- unsupported fact kind or schema version;
- stale generation and out-of-order delivery; and
- partial failure without an ambiguous commit result.

## State and communication boundaries

Every repository move records the owner of durable state before code moves.

| State | Owner | Supported boundary |
| --- | --- | --- |
| Source credentials and cursor semantics | Collector repository/runtime | Bounded collector configuration, secret references, and a published checkpoint contract; durable checkpoint storage follows its declared state owner. |
| Producer identity and authorization | Eshu core | Authenticated claim/intake contract. |
| Facts, generations, admission, and recovery | Eshu core | Versioned fact submission and outcome. |
| Canonical graph and content projection | Eshu core | Supported read API; no direct collector access. |
| Query/read models and supported admin mutations | `eshu-api` with core data ownership preserved | Versioned read contracts and explicit mutation admission into core-owned persistence. |
| Workflow plans, claims, and scheduler state | `eshu-workflow` | Versioned workflow protocol; no shared-table shortcut after extraction. |
| Component trust and execution state | `eshu-extension-host` | Component manifest, trust, execution, and diagnostics contracts. |
| Deployment compatibility | `eshu-deploy` | Immutable artifact versions and a tested compatibility matrix. |

Sharing a physical Postgres cluster during migration does not make its tables a
public API. A runtime cannot cut over until its state owner and supported
communication boundary are explicit.

## Migration stages

The ecosystem roadmap uses five states:

| State | Meaning |
| --- | --- |
| `core_by_design` | The default Git collector or retained parser/reducer core. |
| `planned` | Destination and owner are assigned, but prerequisites remain. |
| `contract_blocked` | A named SDK, schema, authorization, state, or compatibility contract blocks cutover. |
| `ready_for_cutover` | Repository, release, parity, operations, and rollback proof pass. |
| `external` | Production uses the independently released repository and the old in-tree implementation is removed. |

An old source file moving into a cleaner directory does not advance this state.
Neither does a fixture-only SDK demonstration. State changes follow deployed
evidence and the cutover issue.

## Migration sequence

1. Publish this repository map and keep open issues aligned with it.
2. Publish the two `eshu-sdk` modules and their compatibility policy.
3. Add first-party producer delegation and fail-closed admission.
4. Publish the collector repository template and compatibility harness.
5. Inventory every collector and runtime, including state and public contracts.
6. Move one collector at a time, using PagerDuty boundary evidence as a pattern
   while preserving its stated production caveat.
7. Move the non-collector runtimes in dependency order: deployment contract,
   read API, MCP and CLI adapters, workflow/webhook/extension/scanner runtimes,
   and intake orchestration.
8. Remove superseded in-tree code only after production cutover proof passes.

Moves may proceed in parallel only when repositories do not share a contract
change, state migration, or production cutover window.

## Per-repository cutover gate

Every cutover issue must prove:

- source parity on representative and hostile fixtures;
- fact-schema, stable-key, scope, and generation compatibility;
- reducer, graph, API, and MCP truth for the emitted facts;
- bounded retries, idempotency, cancellation, duplicate delivery, and ordering;
- resource limits, health, readiness, metrics, traces, logs, and status;
- secret handling, redaction, artifact provenance, and vulnerability response;
- supported core/collector version pairs;
- independent build, release, deployment, and ownership; and
- a rehearsed rollback with no simultaneous producer authority or lost facts.

Credential-free conformance runs locally and in CI. Provider-backed and deployed
proof remains separate and must name the tested artifact, topology, and evidence
location.

## Rollback

Rollback is a producer-authority transition, not just a deployment reversal.

1. Stop new claims for the external producer.
2. Wait for or explicitly expire in-flight leases.
3. Revoke its producer authorization.
4. Restore the last supported producer version.
5. Replay from the last committed generation.
6. Verify exact fact, projection, graph, and read truth.

At no point may two producers hold authority for the same source scope and
generation unless the protocol explicitly defines a safe handoff.

## Naming during preparation

Package moves should make the current tree readable and expose dependencies
that cross a future repository boundary. They must not encode false architecture.

- Use plain-English nested directories.
- Do not repeat directory names in filenames or package-qualified symbols.
- Keep internal implementation private even when it will later move.
- Name contracts by responsibility, not by the first runtime that uses them.
- Do not create a network service merely because a package gained a directory.

## Tracking

- #6707 owns the ecosystem inventory and repository roadmap.
- #6708 publishes the SDK and fact-schema modules.
- #6709 owns first-party producer delegation.
- #6710 owns the collector repository template and compatibility harness.
- #4047 is the per-collector cutover gate.
- #6053, #6061, and #6692 prepare readable internal ownership boundaries.

No-Regression Evidence: this document changes repository direction only. It
does not move code, alter runtime behavior, change a wire contract, or claim a
collector has cut over.

No-Observability-Change: this document defines the signals required at future
cutovers but adds no metric, span, log, status field, or dashboard label.
