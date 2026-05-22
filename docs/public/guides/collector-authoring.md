# Collector Authoring Guide

Use this guide when adding or expanding a collector family that feeds Eshu's
shared data plane. Collectors observe source truth and emit typed facts. They
do not own canonical graph correlation, API repair behavior, or cross-source
truth.

For the current deployed lanes and readiness gaps, see
[Collector And Reducer Readiness](../reference/collector-reducer-readiness.md).

## Collector Contract

Every collector must define one bounded work unit before implementation starts.
Lock these decisions first:

| Decision | Required answer |
| --- | --- |
| Source truth | Which source is authoritative: Git, cloud API, registry, state file, documentation source, or another source. |
| Scope model | The durable ingestion shard: repository, account, region, cluster, registry target, space, dataset, or equivalent. |
| Generation model | What replaces the previous authoritative snapshot for that scope. |
| Fact model | Which typed facts are emitted before downstream projection begins. |
| Confidence | Whether each fact is observed, reported, inferred, derived, or legacy unknown. |
| Failure model | Which errors are retryable, terminal, rate-limited, auth-related, or source-missing. |
| Operator model | Which health, backlog, duration, throttle, retry, pool, and status signals prove progress. |

The collector owns source observation, scope identity, generation identity, and
fact emission. Source-local projection belongs to projector code. Cross-source
correlation, graph promotion, read-model truth, retries, and dead-letter
handling belong to reducers and shared storage contracts.

If any row is still fuzzy, the collector is not ready for code.

## Fact Confidence

Every emitted fact must carry `collector_kind` and `source_confidence`.
`collector_kind` identifies the producing family. `source_confidence` tells
reducers how much trust to place in the claim when sources disagree.

Use the vocabulary in `go/internal/facts/source_confidence.go`:

| Value | Meaning |
| --- | --- |
| `observed` | Eshu read the source artifact directly, such as Git contents or Terraform state. |
| `reported` | An external API reported the value, such as AWS or registry metadata. |
| `inferred` | Eshu concluded the claim by comparing or correlating other facts. |
| `derived` | Eshu materialized the value from existing Eshu facts. |
| `unknown` | Legacy or system fallback. New collector work should not depend on it. |

Documentation sources are observed evidence about what a document says. They do
not prove that the documented claim is operationally true. Documentation facts
must feed reducer-owned findings before they affect graph, deployment, runtime,
source-code, or infrastructure truth.

## Runtime Shape

Hosted collectors use the shared Go service shape: one CLI entrypoint, shared
health/readiness/metrics/status wiring when the runtime mounts HTTP admin,
structured logs with trace and correlation fields, source-stage metrics, and
bounded runtime knobs for pools, workers, queues, API budgets, and limits.

Claim-driven collectors must run through `collector.ClaimedService` and durable
workflow claims. The workflow coordinator plans bounded work rows. Collector
runtimes claim the work, heartbeat the claim, emit facts through the normal
commit boundary, and complete, release, or fail the claim with fencing.

Do not add a Helm or deployment knob for a design-only collector. A chart value
is an operator promise that the binary, fact contract, configuration, status
path, and runtime proof exist.

## Implementation Order

Follow this order so the collector lands on stable ownership boundaries:

1. Update published architecture, workflow, or runtime docs when the source
   changes ownership or deployment rules.
2. Define scope and generation identity.
3. Define fact payloads, validation, and confidence.
4. Implement source observation and normalization.
5. Emit facts into the durable store.
6. Reuse projector and reducer contracts for downstream work.
7. Add telemetry, logs, traces, status, and claim handling where relevant.
8. Add replay, fixture, local, or cloud validation gates.
9. Update package and public docs before calling the slice complete.

Do not start with answer shaping, direct graph mutations, post-commit repair
hooks, or one-off API fixes. Those are downstream ownership problems, not
collector contracts.

For documentation collectors, start with source-neutral facts:
`documentation_source`, `documentation_document`, `documentation_section`,
`documentation_link`, `documentation_entity_mention`, and
`documentation_claim_candidate`. Keep source-specific fields in metadata until
the shape is stable across at least two documentation source families.

## Evidence Gates

Use [Local Testing](../reference/local-testing.md) for the full gate map. The
collector-specific checks are:

| Gate | What it enforces |
| --- | --- |
| `scripts/verify-package-docs.sh` | Changed Go packages under `go/internal` or `go/cmd` have `doc.go`, `README.md`, and scoped `AGENTS.md`. |
| `scripts/verify-performance-evidence.sh` | Hot-path collector changes with graph writes, workers, leases, batching, goroutines, channels, queues, or runtime stages carry tracked performance and observability evidence. |
| `scripts/verify-collector-authoring-gate.sh` | Changed collector source packages have package docs, tests, collector evidence, observability evidence, and a deployment or ServiceMonitor decision note. |

Use these tracked markers in a changed reference doc or package README when the
change affects that surface:

```text
Collector Performance Evidence: <baseline, after measurement, input shape,
fact count, wall time, remote/API budget, backend where relevant, and result>

Collector Observability Evidence: <source-stage metrics, spans, logs, status
fields, pprof, or queue/domain counters that let an operator diagnose this
collector>

Collector Deployment Evidence: <health, readiness, metrics, ServiceMonitor,
and admin/status proof, or a clear no-hosted-runtime decision>
```

For correctness-only work, use `No-Regression Evidence:`. If existing telemetry
already covers the path, use `No-Observability-Change:` and name the exact
metric, span, log event, status field, or pprof path.

## Anti-Patterns

Avoid these patterns:

- writing canonical graph edges directly from collectors
- hiding source gaps behind optimistic status output
- encoding source-specific meaning in generic fallback fields
- using full re-indexing as the normal freshness path
- adding a second admin, metrics, or logging shape for one collector
- adding compatibility shims or alternate production runtimes outside the
  shared Go service contracts

Those choices make the next collector harder to add and move truth out of the
layer that owns it.

## Related Docs

- [System Architecture](../architecture.md)
- [Source Layout](../reference/source-layout.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [Local Testing](../reference/local-testing.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
