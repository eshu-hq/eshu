# Collector Authoring Guide

Use this guide when adding or expanding a collector family. Collectors observe a
source and emit typed facts. They do not write canonical graph truth directly.

For deployed lanes and readiness gaps, see
[Collector And Reducer Readiness](../reference/collector-reducer-readiness.md).

## Contract To Lock First

Before writing code, define:

| Decision | Required answer |
| --- | --- |
| Source truth | Git, cloud API, registry, state file, documentation source, or another source. |
| Scope | The durable shard: repo, account, region, cluster, registry target, space, dataset, or equivalent. |
| Generation | What replaces the previous authoritative snapshot for that scope. |
| Facts | Which fact kinds are emitted before projection begins. |
| Confidence | Whether each claim is observed, reported, inferred, derived, or unknown. |
| Failure model | Retryable, terminal, rate-limited, auth-related, or source-missing. |
| Operations | Health, backlog, duration, throttle, retry, pool, and status signals. |

If any row is fuzzy, the collector design is not ready.

## Ownership Boundary

The collector owns source observation, scope identity, generation identity, and
fact emission. Source-local projection belongs to projector code. Cross-source
correlation, graph promotion, retries, dead letters, and read-model truth belong
to reducers and shared storage contracts.

New facts must carry `collector_kind` and `source_confidence`. The confidence
vocabulary lives in `go/internal/facts/source_confidence.go` and the public
envelope contract lives in
[Fact Envelope Reference](../reference/fact-envelope-reference.md).

Documentation collectors are evidence about what a document says. They do not
prove that the documented operational claim is true until reducer-owned
findings admit it.

## Runtime Shape

Hosted collectors use the shared Go service shape: one binary, shared health and
metrics wiring, structured logs, bounded runtime knobs, and status surfaces.
Claim-driven collectors use `collector.ClaimedService` and durable workflow
claims with heartbeats and fencing.

Do not add a Helm value for a design-only collector. A chart option is an
operator promise that the binary, fact contract, configuration, status path, and
runtime proof exist.

## Implementation Order

1. Update architecture or runtime docs when ownership or deployment rules
   change.
2. Define scope and generation identity.
3. Define fact payloads, validation, and confidence.
4. Implement source observation and normalization.
5. Commit facts through the durable store.
6. Reuse projector and reducer contracts for downstream work.
7. Add telemetry, logs, traces, status, and claim handling where relevant.
8. Add replay, fixture, local, or cloud validation gates.
9. Update package and public docs before calling the slice complete.

Avoid answer shaping, direct graph mutation, post-commit repair hooks, or one-off
API fixes in collector code. Those belong downstream.

## AWS Scanner Registration

AWS service scanners under `go/internal/collector/awscloud/services/<svc>/`
self-register with the runtime through a sibling `runtimebind/` sub-package.
A new scanner adds:

- `services/<svc>/runtimebind/bind.go` calling `awsruntime.Register` from
  `init()`.
- `services/<svc>/runtimebind/` package docs (`doc.go`, `README.md`,
  `AGENTS.md`) and a `bind_test.go` that asserts the binding resolves via
  `awsruntime.LookupBuilder`.
- One underscore-import line appended to
  `go/internal/collector/awscloud/awsruntime/bindings/bindings.go`. That file
  is marked `merge=union` in `.gitattributes` so parallel scanner PRs do not
  collide.
- One new entry in the want-list inside
  `awsruntime/registry_supported_services_test.go` so a missing binding
  surfaces as a test failure.

No file in `awsruntime/` itself changes for a new scanner. The runtime
already has zero compile-time dependency on individual service packages.

## Verification

Use [Local Testing](../reference/local-testing.md) for the full gate map. Common
collector gates are:

```bash
scripts/verify-package-docs.sh
scripts/verify-performance-evidence.sh
scripts/verify-collector-authoring-gate.sh
```

For in-progress branches, pair those with the focused Go tests for the touched
collector, fact, projector, reducer, and runtime packages.

## Related Docs

- [System Architecture](../architecture.md)
- [Source Layout](../reference/source-layout.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [Local Testing](../reference/local-testing.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
