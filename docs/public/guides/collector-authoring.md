# Collector Authoring Guide

Use this guide when adding or expanding a collector family. Collectors observe a
source and emit typed facts. They do not write canonical graph truth directly.

For deployed lanes and readiness gaps, see
[Collector And Reducer Readiness](../reference/collector-reducer-readiness.md).
For GCP and Azure cloud collector design, see
[Multi-Cloud Runtime Collector Contract](../reference/multi-cloud-collector-contract.md).
For moving an existing collector out of tree, see
[Collector Extraction Policy](../reference/collector-extraction-policy.md).

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

## Generated Claimed Entrypoints

Claim-driven hosted collectors with the standard runtime shape should keep
shared command boilerplate in
`go/internal/collector/entrypoints/collector_entrypoints.yaml` and regenerate
with:

```bash
scripts/generate-collector-entrypoints.sh
scripts/verify-collector-entrypoints-generated.sh
```

The manifest owns the collector name, binary name, scope kind, auth mode, target
identity fields, target auth fields, environment variables, source constructor,
and claim-service options. Generated `main.go`, `service.go`, and generic
`config.go` files wire telemetry bootstrap, pprof, Postgres instrumentation,
hosted status routes, claim selection, lease timing, owner fallback, source
construction, runtime-signal attachment, and bounded startup failures.

Keep provider target decoding in handwritten `source_config.go` files. That is
where token env references, JQL env references, target limits, source-specific
duration fields, redaction-sensitive validation, and provider config error
messages belong.

The first generated parity slice covers `collector-pagerduty` and
`collector-jira`. PagerDuty now generates 326 shared entrypoint lines from about
37 manifest lines; Jira generates 324 shared entrypoint lines from about 37
manifest lines. New standard claimed collectors therefore avoid roughly 287
lines of handwritten shared boilerplate before provider-specific target config
is added.

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

There is no want-list to edit. The supported-service guard is derived: the
guard tests enumerate the `services/<svc>/runtimebind/` directories on disk and
the runtimebind blank imports parsed from `bindings.go`, then assert the two
sets and the registered scanner count agree. A new
`services/<svc>/runtimebind/` directory without a matching `bindings.go` import
fails the guard automatically, so adding a scanner touches zero want-lists.

No file in `awsruntime/` itself changes for a new scanner. The runtime
already has zero compile-time dependency on individual service packages.

### Redaction-key requirement

A scanner that redacts sensitive metadata declares the requirement in its own
`runtimebind/bind.go`, not in the command. Set `RequiresRedactionKey: true` in
the `awsruntime.Register` call and keep the builder's `d.RedactionKey.IsZero()`
guard. The command derives the `ESHU_AWS_REDACTION_KEY` pre-flight requirement
and the missing-key error message from
`awsruntime.ServiceKindsRequiringRedactionKey()`, so adding a redaction scanner
touches zero shared lines in `go/cmd/collector-aws-cloud/config.go`. Scanners
that need no key leave the flag unset.

The reference table at
`docs/public/services/collector-aws-cloud-scanners.md` is marked `merge=union`
in `.gitattributes`, so parallel scanner PRs append rows without colliding; the
strict docs build catches any duplicate row.

### Relationship graph-join guard

Every relationship a scanner emits must carry a `target_type` that names a
resource family Eshu can resolve, or the edge dangles and never joins its
target node. The dominant historical scanner defect was an empty `target_type`,
a `target_type` that is not a real resource family, or an ARN-keyed target keyed
by a bare name. The `internal/collector/awscloud/internal/relguard` test-support
package mechanizes the contract so it is no longer a per-PR review burden:

- A repo-level static guard (`TestLiveScannerTreeHasNoGraphJoinDefects`) AST-walks
  the scanner tree and fails when any statically resolvable `target_type` literal
  is empty or is neither a declared `awscloud.ResourceType*` constant value nor a
  documented `relguard.KnownTargetTypeAllowlist` entry.
- A runtime helper, `relguard.AssertObservations(t, observations...)`, that a
  scanner test calls to enforce the same contract on the data-dependent
  `target_type` values a helper or field read produces, plus ARN shape and
  ARN-vs-name join-mode consistency.

A new scanner whose target type is a real resource family needs no guard change.
A target that is deliberately a forward reference (not scanned yet) or a
synthetic/non-AWS anchor goes in `KnownTargetTypeAllowlist` with a rationale. A
target whose value should match an existing scanner's published `resource_id`
type must be fixed, not allowlisted. See the package README for the full list of
what the guard does and does not catch.

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
- [Multi-Cloud Runtime Collector Contract](../reference/multi-cloud-collector-contract.md)
- [Collector Extraction Policy](../reference/collector-extraction-policy.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [Local Testing](../reference/local-testing.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
