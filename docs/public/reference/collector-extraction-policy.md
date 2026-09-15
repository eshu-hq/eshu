# Collector Repository Migration Policy

This page defines how Eshu moves non-default collectors into independently
owned and released repositories.

The destination is settled: every non-default collector moves. The gate below
controls when a collector cuts over and when its old in-tree implementation can
be removed.

Collectors observe source truth and emit versioned facts. Eshu core owns fact
admission, canonical identity, projection, persistence, recovery, graph truth,
and retraction.

## Target Boundary

Each collector repository contains one source collector, its private source
client, fixtures, conformance tests, build, release, security response, and
operator documentation.

Collector repositories depend only on published contracts. They never receive
Postgres, graph, queue, lease, reducer, API, MCP, or workflow implementation
handles. They submit a bounded result containing validated facts and an explicit
outcome.

The default Git collector, repository discovery, parser, and reducer core remain
in `eshu`. Public collector and fact-schema contracts move to independently
versioned modules in `eshu-sdk`.

The internal design
`docs/internal/design/eshu-ecosystem-repository-migration.md` records the
complete repository map.

## One Collector, One Repository

Each non-default collector has its own destination repository. Provider source
clients, collection logic, and private helpers move with that collector.
Provider-specific parsing, reduction, and canonical projection remain with the
core owner. Shared public fact shapes belong to the fact-schema module.

New non-default collectors start in their destination repository. They are
never staged in `eshu` as temporary implementations.

Package nesting inside the current tree is preparation only. A directory level
does not create a repository, service, public API, or permission to share a
database.

## Extraction Criteria

A collector can enter `ready_for_cutover` only when every applicable row is
proven.

| Criterion | Required evidence |
| --- | --- |
| Source boundary | The collector imports only external source dependencies and published Eshu contracts. |
| Fact contract | Every emitted kind, schema, stable key, confidence, provenance field, redaction rule, and downstream consumer is documented. |
| Producer authority | The first-party producer identity and allowed fact kinds are explicit, versioned, revocable, and rejected when invalid. |
| Scope and generation | Durable source scope and generation support retry, replay, stale input, idempotency, and out-of-order delivery. |
| Runtime safety | Claims are bounded; credentials are read-only where possible; retries, cancellation, dead letters, and partial failures are deterministic. |
| Trust and release | Artifact identity, publisher, compatible core range, immutable digest, revocation, vulnerability response, and ownership are recorded. |
| Truth proof | Fixtures agree with reducer, graph, API, and MCP truth before support is claimed. |
| Operations | Health, readiness, metrics, traces, structured logs, status, resource bounds, and operator procedures are proven. |
| Rollback | Authority can return to the prior supported producer without duplicate authority, lost facts, or an ambiguous generation. |

Passing manifest validation or fixture conformance is necessary but not
sufficient. It proves a package and result shape, not production graph truth or
operational safety.

## Migration States

The target roadmap vocabulary is:

| State | Meaning |
| --- | --- |
| `core_by_design` | The default Git collector. It remains in `eshu`. |
| `planned` | Destination and owner are assigned. |
| `contract_blocked` | A named SDK, schema, authorization, state, or compatibility contract blocks cutover. |
| `ready_for_cutover` | Repository, release, parity, operational, and rollback proof pass. |
| `external` | Production uses the external repository and the old implementation has been removed. |

The current `eshu component extraction-readiness` command still reports its
legacy advisory classifications: `keep_in_tree`, `extraction_candidate`,
`blocked`, and `external_ready`. Until its wire contract is deliberately
updated, read them as diagnostics rather than the ecosystem migration state.
Do not claim the target state names are live CLI output.

The migration mapping is:

| Current diagnostic | Roadmap interpretation |
| --- | --- |
| `keep_in_tree` | Historical policy result; Git maps to `core_by_design`, while every other collector remains scheduled to move. |
| `extraction_candidate` | Usually `planned`; proof may satisfy some cutover rows. |
| `blocked` | `contract_blocked` when the diagnostic names the missing contract. |
| `external_ready` | At most `ready_for_cutover` until deployed cutover and in-tree removal prove `external`. |

Changing the diagnostic vocabulary requires a separate CLI/API contract change
with compatibility tests and documentation updates.

## First-Party Producer Delegation

An extracted first-party collector can emit a core-owned fact kind only through
explicit producer delegation.

Admission validates:

- producer identity and source scope;
- allowed fact kinds and schema versions;
- stable key, generation, and payload bounds;
- confidence, citation, and provenance requirements; and
- redaction and secret-handling policy.

Delegation does not transfer canonical ownership. Collectors cannot write graph
edges, choose incompatible canonical identities, mutate queues, or access core
storage. Authorization must fail closed when it is missing, expired, revoked,
or does not cover the submitted kind.

## Claim and Result Contract

The public collector SDK boundary is a bounded exchange:

1. The host supplies a claim identifier, source scope, configuration, supported
   contract versions, deadline, and permitted work.
2. The collector reads the external source and returns facts plus a typed
   outcome.
3. The host authenticates the producer and validates size, schema, identity,
   generation, redaction, and compatibility.
4. Eshu core owns admission, persistence, reducer queue transitions, and
   projection. Workflow claim transitions remain owned by `eshu-workflow`.

The contract must distinguish successful empty collection, retryable source
failure, permanent rejection, cancellation, partial source visibility, and an
ambiguous transport result. Duplicate delivery must be safe.

## Compatibility

Every collector repository tests a declared matrix of collector SDK,
fact-schema, and Eshu core versions. At minimum it covers the newest collector
against the oldest supported core and the oldest supported collector against
the newest core.

A schema release cannot silently change stable keys, source scope, generation
meaning, redaction, or downstream materialization. Breaking changes require a
new version and an overlap or migration plan.

## Cutover and Rollback

Production cutover follows #4047.

Before cutover:

- the external artifact and exact configuration are recorded;
- in-tree and external outputs are compared on the same representative inputs;
- core graph and read truth agree;
- operator dashboards and alerts identify the producer version; and
- rollback has been rehearsed.

During cutover, only one producer may hold authority for a source scope and
generation. Stop new claims, drain or expire in-flight leases, switch producer
authority, and verify accepted facts before removing the old implementation.

Rollback reverses the authority transition, restores the last supported
producer, replays from the last committed generation, and verifies exact fact,
projection, graph, and read truth.

## Extraction Readiness Diagnostics

The existing component diagnostic is advisory. It never moves code, changes
authority, disables an in-tree collector, or changes runtime behavior.

Read it with:

```bash
eshu component extraction-readiness
eshu component extraction-readiness pagerduty
eshu component extraction-readiness jira --verbose --json
```

Its result is evidence for the migration issue, not the cutover itself. Missing
criteria fail closed and are reported as blockers.

## PagerDuty Reference Path

<!-- capability-state: id=component_extensions.diagnostics state=ga issue=2700 -->
<!-- capability-state: id=component_extensions.inventory state=ga issue=2700 -->

PagerDuty is the completed reference proof for the out-of-tree execution
boundary. A trusted component package uses `collector-sdk/v1alpha1`, receives no
core handles, claims bounded work through the component-extension host, and
returns validated facts through the normal claimed-service boundary.

The proof covers packaging, trust, claim execution, result validation, fixture
parity, Compose execution, redaction, and operator evidence.

| Stage | State | Proof |
| --- | --- | --- |
| Reference package | Complete | `examples/collector-extensions/pagerduty/manifest.yaml` |
| Trust boundary | Complete | `go/internal/runtime` Helm contract tests and [Plugin Trust Model](plugin-trust-model.md) |
| Claim-capable execution | Complete | `go/cmd/collector-component-extension` and `go/internal/collector/extensionhost` |
| Fact-shape parity | Complete for the example contract | `go test ./internal/collector/pagerduty -run ReferenceComponent` |
| Remote Compose proof | Complete | `docs/public/run-locally/docker-compose.component-extension-pagerduty.yaml` and `scripts/verify-remote-e2e-pagerduty-component-extension.sh` |
| Private-data proof | Complete | Remote proof redaction canary and reference component redaction test |
| Operator evidence | Complete | `docs/internal/remote-validation/prod-component-extension-inventory.md` and `docs/internal/remote-validation/prod-component-extension-diagnostics.md`; health, readiness, metrics, logs, and status endpoints. |
| Production reducer/read truth | In-tree only | `go/internal/reducer/incident/incident_routing_evidence_rows.go`, `go/internal/storage/cypher/incident_routing_evidence_writer.go`, and `go/internal/query/incident_context_routing.go`. |

The two capability-state markers above are backed by the named production
validation artifacts. The diagnostics artifact records a live authenticated
`GET /api/v0/component-extensions/{id}/diagnostics` response with trust,
policy, scheduler, read-model, and conformance state. The inventory artifact
records the matching deployed registry readback.

The reference package emits namespaced example facts such as
`dev.eshu.examples.pagerduty.*`. Those facts are not interchangeable with the
core `incident_routing.*` and `incident.record` kinds. The reducer, graph
writer, API, and MCP paths do not consume the example kinds.

Therefore the boundary proof does not mean PagerDuty has cut over. Disabling the
in-tree collector today would commit evidence that the incident-routing read
path skips. First-party producer delegation and production-kind compatibility
must land before that cutover.

The Helm component-extension path is also default-off. Enabling it remains an
explicit operator action. Broader live PagerDuty configuration coverage and
alert-route comparison remain follow-up work.

See [PagerDuty Evidence Contract](pagerduty-evidence.md) for the detailed proof.

## Verification Gates

Use the smallest gate that proves the touched boundary.

| Change | Required gate |
| --- | --- |
| Policy or docs only | Strict MkDocs build, collector-authoring gate when guidance changes, package-doc gate, `git diff --check`, and sensitive-string scan. |
| SDK or schema contract | SDK tests, schema compatibility, component inspect/verify/conform tests, and package-doc gate. |
| Producer delegation | Admission tests for identity, allowlists, revocation, schema, scope, retries, duplicates, and stale generations. |
| Extension host or worker | Focused host, workflow, retry, cancellation, identity-mismatch, status, and resource-bound tests. |
| Collector cutover | Fixture conformance, deployed proof, reducer/graph/API/MCP truth, performance evidence, operations evidence, and rollback rehearsal. |

No-Regression Evidence: this policy change documents the approved destination
and cutover requirements. It does not change the SDK, schema, collector runtime,
workflow claim, graph, reducer, query, Helm, Compose, or release behavior.

No-Observability-Change: this policy names required future signals but adds no
metric, span, log, status field, queue domain, pprof output, or dashboard label.

## Related Docs

- `docs/internal/design/eshu-ecosystem-repository-migration.md`
- [Community Extension Authoring](../extend/community-extension-authoring.md)
- [Collector Authoring](../guides/collector-authoring.md)
- [Component Package Manager](component-package-manager.md)
- [PagerDuty Evidence Contract](pagerduty-evidence.md)
- [Plugin Trust Model](plugin-trust-model.md)
- [Local Testing](local-testing.md)
