# Collector Extension SDK Contracts (#1821)

Status: design gate for issue #1821. No Go code, schema migration, runtime
default, Compose profile, Helm value, API route, MCP tool, or reducer change is
part of this document.

Related: #1817 community-extension epic, #1818 component package manager, #1821
SDK design issue, [System Architecture](../../public/architecture.md),
[Extend Eshu](../../public/extend/index.md), [Collector Authoring](../../public/guides/collector-authoring.md),
[Fact Envelope Reference](../../public/reference/fact-envelope-reference.md),
[Fact Schema Versioning](../../public/reference/fact-schema-versioning.md),
[Component Package Manager](../../public/reference/component-package-manager.md),
and [Plugin Trust Model](../../public/reference/plugin-trust-model.md).

This is a maintainer-only internal design doc. It is not part of the public
MkDocs site (`docs_dir: public`), so the public docs build does not validate
these links.

## 1. Decision

Eshu should publish a minimal public collector SDK as source in this repository,
not as a generated-only artifact and not as a separate repository at first.

The first implementation target is a separate Go module under
`sdk/go/collector` with module path
`github.com/eshu-hq/eshu/sdk/go/collector`. It must not import
`github.com/eshu-hq/eshu/go/internal/...`. Release automation should tag the
submodule independently, for example `sdk/go/collector/v0.1.0`, while core
releases continue to enforce `spec.compatibleCore` in component manifests.

Generated JSON Schema, golden fixtures, and a conformance CLI are artifacts of
the SDK source. They are not the source of truth. Moving the SDK to a separate
repository is a future release-engineering decision, not the first contract.

The runtime shape should be a process/OCI adapter protocol, not an in-process Go
plugin. Core launches or supervises an extension process or container, passes a
bounded claim/config document, receives fact/status/result records, validates
them, and commits through existing Eshu collector stores. Extensions never link
against Eshu internals and never receive direct database, graph, queue, reducer,
or query handles.

## 2. Pipeline Placement

The extension SDK plugs into the existing facts-first pipeline:

```text
component manifest + trust policy
  -> core extension host
  -> out-of-tree collector process
  -> SDK fact/status/result records
  -> core validation and internal facts.Envelope mapping
  -> Postgres fact commit and workflow claim completion
  -> projector/reducer queues
  -> reducer-owned graph/read-model truth
  -> API/MCP/CLI reads
```

This preserves the public architecture rule: extensions emit facts; reducers
own graph truth. The SDK is a compatibility facade over the intake boundary,
not a second graph writer or query engine.

## 3. Minimal Public API Surface

The SDK should expose data contracts first. Helper interfaces may wrap them, but
the wire format must remain inspectable and language-portable.

| Public type | Required contract |
| --- | --- |
| `Claim` | Core-provided work item identity: component id, instance id, collector kind, source system, scope id, scope kind, source run id, generation id, work item id, fencing token, attempt count, deadline, and config handle. |
| `Scope` | Bounded source shard such as repository, account/region/service, registry repository, site, dataset, or document source. Extensions may validate it but may not change a claimed scope. |
| `Generation` | One source observation for the scope. Carries generation id, observed time, optional freshness hint, and result state: complete, unchanged, partial, retryable failure, or terminal failure. |
| `Fact` | Public mirror of the durable fact envelope: fact kind, schema version, stable fact key, source confidence, observed time, source ref, payload, tombstone flag. Core derives the internal fact id and validates scope/generation/fence agreement. |
| `SourceRef` | Source-local provenance: source system, scope id, generation id, fact key, source URI, and source record id. It must not contain host paths, credentials, private URLs, or raw provider responses. |
| `Emitter` | Validates declared fact kinds, schema versions, confidence values, stable keys, payload JSON shape, duplicate keys, and source refs before writing records. |
| `Status` | Bounded progress and failure records: status class, failure class, retry-after hint, partial flag, warning counts, fact counts, and source latency. |
| `Telemetry` | Helpers for bounded OTEL/log fields. Metric labels must use closed enums or safe fingerprints; high-cardinality source values stay in logs or spans. |

The first SDK does not need parser, relationship, reducer, API, MCP, or graph
types. It may include payload-validation helpers for public fact schemas, but
fact family payload structs should only be promoted into the SDK after the
owning consumer contract is stable.

## 4. Ownership Boundary

| Responsibility | Core-owned | Extension-owned |
| --- | --- | --- |
| Component manifest validation | Yes: identity, trust, compatible core, declared fact families, runtime protocol. | Declares package metadata and emitted facts. |
| Trust and activation | Yes: disabled, allowlist, strict provenance, revocation, enabled and claim-capable state. | Ships digest-pinned artifact and publisher metadata. |
| Workflow planning | Yes: run creation, work items, fairness, claim leases, fencing, attempt budget, expired-claim reaping. | None. It receives one claimed unit at a time. |
| Source observation | Provides claim/config/deadline. | Reads external source within configured scope and limits. |
| Scope and generation identity | Provides claimed identity for claim-driven work; validates returned generation. | May compute freshness hints and source-local source refs. |
| Fact envelope mapping | Validates public facts and maps them to internal `facts.Envelope`. | Emits declared public fact records with stable keys and safe payloads. |
| Retry and status | Owns claim mutation, heartbeat, retry visibility, terminal failure, dead letter, and `/admin/status`. | Returns retryable/terminal/partial/unchanged result with bounded failure class. |
| Telemetry substrate | Owns service resource identity, scrape surface, shared metric names, and status API. | Emits component-owned metrics under declared prefix and bounded span/log attributes. |
| Reducer and graph truth | Owns projector queues, reducer phases, DDL, graph writes, read models, API/MCP truth labels. | None. It may declare required reducer contracts but cannot execute them. |

## 5. Fact And Schema Contract

Extensions must declare every emitted fact family in the component manifest.
For optional components, fact kinds must be namespaced, for example
`dev.example.scorecard.result`, unless a core-owned fact family explicitly
admits third-party producers.

Each emitted fact must satisfy:

- `fact_kind` is declared by the manifest and supported by the active core.
- `schema_version` is semantic and declared by the manifest.
- `source_confidence` is one of `observed`, `reported`, `inferred`, or
  `derived`; `unknown` is forbidden for new component output.
- `stable_fact_key` is deterministic for the same source observation.
- `source_ref` points to a source-local record without credentials or
  machine-specific paths.
- `payload` is valid JSON, bounded, redacted, and compatible with the declared
  schema.
- duplicate facts with the same stable key in one generation are exact
  duplicates or validation failures.

Core must fail closed on undeclared fact kinds, unsupported major versions,
newer minors without declared support, source-confidence mismatch, namespace
collision, malformed payloads, or source refs outside the claimed scope.

Reducers remain the only place where facts become canonical graph or read-model
truth. A component fact family without a reducer or query consumer is stored as
source evidence only and must not be advertised as platform truth.

## 6. Claim, Retry, Status, And Telemetry Contracts

Claim-driven extensions should run through a core extension host that uses the
existing `ClaimedService` pattern. The host owns `ClaimNextEligible`,
`HeartbeatClaim`, `CompleteClaim`, `ReleaseClaim`, `FailClaimRetryable`, and
`FailClaimTerminal`. The extension receives the claim context but cannot mutate
workflow rows directly.

Result states:

| Result | Core action |
| --- | --- |
| `complete` | Validate facts, commit generation under the active fence, complete claim. |
| `unchanged` | Complete claim without a fact commit when freshness proves no new generation is needed. |
| `partial` | Commit reachable facts and warning facts, mark status partial, complete claim unless the extension marks the missing source as retryable. |
| `retryable` | Fail claim retryable with bounded failure class and visibility delay. |
| `terminal` | Fail claim terminal with bounded failure class. |

Telemetry requirements:

- Core records claim, commit, fact count, duration, retry, dead-letter, status,
  and admin-surface signals.
- Extension metrics use the declared manifest prefix and bounded labels such as
  provider, operation, result, status class, failure class, fact kind, and
  warning kind.
- Raw repository names, package names, account ids, URLs, object ids, token env
  names, credentials, provider response bodies, and file paths do not become
  metric labels.
- Extension spans and logs may include safe scope handles, fingerprints,
  generation ids, work item ids, page counts, limit counts, retry-after values,
  and redaction counters.

## 7. Edge Cases

| Case | Required behavior |
| --- | --- |
| Invalid manifest | Component verification fails before install or activation. Strict mode fails closed until provenance support exists. |
| Invalid operator config | Enabled instance is not claim-capable; if already claimed, the claim fails terminal with `invalid_config`. |
| Invalid claim input | Core extension host rejects the launch before running the extension and releases or terminal-fails according to whether the row is corrupt. |
| Source auth failure | Retryable for transient credential/provider errors; terminal for malformed or missing required credential references. Secret values never enter facts, logs, or labels. |
| Rate limit or network outage | Retryable with bounded `retry_after` and failure class. Partial facts may commit only when the source contract says reachable evidence is still useful. |
| Stale fence | Core rejects the commit in the same transaction and records retryable or terminal failure according to existing claim classification. |
| Scope mismatch | Core rejects the fact batch and terminal-fails with `identity_mismatch`; an extension may not redirect a claim to another scope. |
| Generation mismatch | Core rejects unless the collector family has an explicit resolved-generation contract. The first SDK should not expose resolved-generation mutation. |
| Duplicate delivery | Exact duplicate records converge through stable keys. Conflicting duplicates in one generation fail validation. |
| Retry after partial emit | At-least-once is assumed; the extension must reuse stable keys so retry does not create divergent facts. |
| Tombstone/retraction | Tombstones are allowed only for fact families whose schema documents deletion semantics. Reducers decide graph retraction. |
| Rollback | Core can disable activation, revoke package identity, re-run previous core-compatible collectors, or replay stored facts. Extensions cannot run DDL or in-place migrations. |
| Unsupported reducer consumer | Facts remain source evidence or are rejected during activation if the manifest requires an unavailable reducer phase. |

## 8. SDK Compatibility And Versioning

There are three independent version lines:

1. SDK module semver, for example `sdk/go/collector/v0.1.0`.
2. Wire protocol version, for example `collector-sdk/v1alpha1`.
3. Fact payload schema versions, per fact kind.

Component manifests must declare the SDK protocol version and compatible core
range. Core rejects unsupported SDK protocols before activation. Minor protocol
additions must be backward compatible and optional for older extensions. Major
or alpha-to-beta breaking changes require an explicit migration window and
conformance fixtures for both old and new shapes.

The SDK may add helpers without changing the wire protocol. The wire protocol
changes only when a host and extension would serialize different records.

## 9. What Out-Of-Tree Extensions Cannot Do

Out-of-tree collector extensions cannot:

- write canonical graph nodes or edges;
- run Postgres, graph, queue, or status DDL;
- mutate workflow runs, claims, leases, retries, dead letters, or coordinator
  state directly;
- call private Eshu Go packages under `go/internal`;
- register API, MCP, CLI, query, or reducer behavior at runtime;
- bypass component trust policy, allowlists, compatible-core checks, or
  revocation;
- emit undeclared fact kinds or `source_confidence=unknown`;
- persist raw secrets, private provider responses, token-bearing URLs, absolute
  host paths, or customer-specific machine paths;
- make a fact family authoritative without a core-owned consumer contract;
- rely on worker-count reduction, serialized queues, or batch size one as a
  correctness fix.

## 10. Candidate Example Extension

The first example should be an OpenSSF Scorecard JSON collector.

Why this example:

- It is useful but not required for core Eshu operation.
- It can run from a local fixture or a bounded HTTPS source.
- It emits reported source evidence, not graph truth.
- It exercises manifest-declared fact families, source confidence, warning
  facts, redaction, partial failure, retryable provider errors, and conformance
  fixtures without needing provider credentials in public docs.

Candidate fact families:

- `dev.eshu.examples.scorecard.snapshot`
- `dev.eshu.examples.scorecard.check`
- `dev.eshu.examples.scorecard.warning`

Initial consumer contract: source evidence only, no required reducer phase. A
later reducer issue may decide whether selected scorecard checks influence
repository risk read models.

## 11. Proof Plan

Implementation PRs should land in this order:

1. SDK module with public types, validators, JSON Schema generation, golden
   fixtures, and no dependency on Eshu internals.
2. Component manifest/runtime-protocol validation for collector SDK packages.
3. Core extension host adapter with fake-extension tests proving validation,
   claim fencing, retry, partial, unchanged, duplicate, and conflict behavior.
4. Example Scorecard extension with fixtures and conformance tests.
5. Local or remote Compose proof that a digest-pinned out-of-tree component can
   emit facts, commit through core, appear in status, and remain non-graph
   truth until a reducer consumer exists.

Minimum gates for implementation:

- `go test` for the SDK module.
- Core Go tests for component, collector, workflow, and Postgres commit paths.
- `scripts/verify-collector-authoring-gate.sh`.
- `scripts/verify-package-docs.sh` if Go package docs change.
- `scripts/verify-performance-evidence.sh` when host runtime, claims, queues,
  graph writes, or telemetry paths change.
- Strict MkDocs and `git diff --check` for docs and repo hygiene.

Remote Docker Compose proof is required before an EKS rollout or public
operator claim that out-of-tree collectors are deployable.

## 12. Follow-Up Issue Map

The design issue has real follow-up issues rather than relying on PR text:

| Issue | Purpose |
| --- | --- |
| #1920 | Implement the public collector SDK module, validators, JSON Schema, and conformance fixtures. |
| #1921 | Add collector SDK protocol validation to component package manifests. |
| #1922 | Build the core-owned collector extension host adapter and claim/fact validation path. |
| #1823 | Own the broader extension conformance harness for manifests, facts, reducers, and query truth. |
| #1828 | Own the reference open-source collector extension package; the Scorecard example proposed here is a candidate shape for that issue. |
| #1923 | Prove the out-of-tree collector SDK path in remote Docker Compose before hosted rollout claims. |
