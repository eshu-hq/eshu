# Component Producer Grants

Core owns the meaning, schemas, stable identities, and join keys of
core-owned fact kinds, but production extraction needs a way to authorize a
trusted first-party producer to emit them. A producer grant is that
authorization: it binds a producer identity and manifest version to one
core-owned fact kind, the covered schema versions, and the source scope it
is valid for, with a bounded lifetime. Anything outside the grant fails
closed at install, activation, and every emission. Grants are part of #6709;
the production cutover gate is #4047.

## Commands

```bash
eshu component grant dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --version 0.1.0 \
  --kind aws_resource \
  --schema-version 1.0.0 \
  --scope aws \
  --expires-in 720h
eshu component grants --component-home ~/.eshu/components
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.aws
eshu component revoke-grant dev.eshu.collector.aws \
  --component-home ~/.eshu/components \
  --version 0.1.0 \
  --kind aws_resource \
  --scope aws
```

`component grant` records the grant and prints
`granted <producer>@<version> kind <kind> scope <scope> expires <RFC3339>`.
`component grants` lists durable grants in record order for audit, one per
line with scope, covered schemas, expiry, and revocation state, or
`no producer grants` when none exist. `component revoke-grant` marks the
stored grant revoked so the next emission fails closed.

## Issuance rules

- The producer version must be a semantic version, matching the manifest
  `metadata.version` rule.
- The kind must be core-owned. Namespaced kinds need no grant, so issuing
  one is rejected as invalid input rather than stored as a no-op.
- At least one `--schema-version` is required; repeat the flag for multiple
  covered versions. Declared manifest versions must be a subset of the
  covered versions for a core-owned kind to pass admission.
- `--scope` names the source scope the grant is valid for and must match a
  manifest-declared collector kind at admission time.
- `--expires-in` must be positive; the stored expiry is the issuance
  instant plus the lifetime. There are no non-expiring grants.
- Re-issuing the same producer, version, kind, and scope replaces the
  stored grant, so rotation and revocation converge on one record per key.
- The grant may be recorded before the producer is installed
  (grant-first-then-install), which is the order cutover needs: authorize
  the replacement before it activates.
- Revoking an absent grant fails with `grant_not_found` instead of
  silently succeeding, so a typo cannot masquerade as a completed
  revocation.
- Inputs canonicalize before storage and lookup: producer, version, kind,
  scope, and schema entries are trimmed, and versions compare in
  normalized form at match, storage-key, and lookup time, so `v0.1.0` and
  `0.1.0` are the same grant identity instead of twin records or a missed
  revocation.

## Enforcement points

Grants are read from durable registry state, never from the component
package itself:

- Install, enable, and readback resolve the registry's stored grants, so a
  granted core-kind manifest passes admission while an ungranted one keeps
  the fail-closed core-owned rejection. Install reads the target home's
  grants before its own pre-validation, which is what makes
  grant-first-then-install work through the operator CLI.
- `component verify` stays policy-alone on purpose: it answers whether the
  manifest passes trust policy without registry context, so a granted
  core-kind manifest still fails verification there. Verify-then-install
  is not the grant flow; grant-then-install is.
- Collector startup loads the grant snapshot for activation and re-reads
  the registry on every emission: a grant revoked or expired during
  execution fails the next result terminal with an `InvalidResult` failure
  instead of emitting under a dead authorization. A registry read failure
  denies core-owned kinds rather than emitting under unknown authorization.

Like install, enable, disable, and uninstall, grant and revoke-grant write
the registry file atomically.

## JSON output

Every subcommand accepts `--json` under
`schema_version: eshu.component.cli.v1`. `grant` and `revoke-grant` report
the `grant` block; `grants` reports the `grants` list:

```json
{
  "schema_version": "eshu.component.cli.v1",
  "command": "grants",
  "status": "listed",
  "grants": [
    {
      "producer_id": "dev.eshu.collector.aws",
      "version": "0.1.0",
      "kind": "aws_resource",
      "schema_versions": ["1.0.0"],
      "scope": "aws",
      "expires_at": "2026-10-17T01:57:53Z",
      "revoked": false
    }
  ]
}
```

`expires_at` is RFC 3339 with full timestamp precision; whole-second values
render without a fraction. Failed commands carry the `error` block with the
stable code operator scripts can branch on (`invalid_input`,
`grant_not_found`).

## End-to-end scope

The operator-level end-to-end path is CLI-proven: `grant`, then `install`
of the granted core-kind manifest, then `grants`/`list` readback, then
`revoke-grant` failing the next admission closed. The live Compose proof
covers the namespaced external path (source-evidence parity with the
in-tree PagerDuty contract), not the grant path: no core-emitting
first-party producer exists yet outside cutover, so there is no granted
core-kind manifest for the Compose harness to run. The first granted core
emission will arrive with the #4047 production cutover.

## Performance and observability evidence

No-Regression Evidence (#6709): the grant checks add one registry file
read plus one linear scan over the live grant set per emission result, at
collector schedule cadence (minutes), beside a process-adapter run plus
workflow mutations and fact writes.

- Baseline (main): the checked-in PagerDuty Compose driver asserts a
  completed `pagerduty-reference` work item with 6/6 parity facts
  committed (extension signature equals the in-tree reference signature).
- After (this branch): the same driver on the nornicdb v1.3.3 backend
  reports the same terminal state — work item completed, 6/6 facts
  committed, `fixture_parity: passed` — with the admission and
  per-emission recheck code in the running image. Emission-path sources
  are unchanged since that run (later commits touch CLI/docs only).
- Input shape: `pagerduty-reference` instance, `pagerduty_account`
  scope, complete-fixture input; crowded-registry microbenchmarks use 50
  stored grants with the match last.
- Microbenchmarks (Apple M4 Pro, darwin/arm64, 100k iterations):
  `BenchmarkAuthorizesEmission` 1099 ns/op with 4 allocs/op;
  `BenchmarkProducerGrantsRead` 39127 ns/op with 115 allocs/op.
  Per emission result that is ~39µs of re-read plus ~1µs per fact,
  orders of magnitude below collection cost.
- Telemetry/log/status evidence: queue terminal state `completed` from
  `workflow_work_items`; driver provenance pins the image commit.
- Why safe: the checks run pre-commit on already-loaded results, change
  no Cypher, worker, queue, lease, or batching behavior, and every
  failure mode denies (missing, expired, revoked, or unreadable grants),
  so the only observable behavior change is a terminal rejection where
  emission was previously unconditional for ungranted core kinds — which
  is the feature, proven by the revocation/expiry tests.

## Grant-decision telemetry

Full reference: [Producer-Grant Decisions](telemetry/producer-grant-decisions.md).

Every grant decision for a core-owned fact kind leaves an operator signal
(#6726), so an operator can tell a grant allow from a grant deny from
telemetry alone. Kinds that are not core-owned need no grant and report
nothing.

| Signal | Name | Carries |
| --- | --- | --- |
| Counter | `eshu_dp_component_producer_grant_decisions_total` | `decision` (`allow`/`deny`), `stage`, `reason`, `fact_kind` |
| Span event | `component.producer_grant.decision` on the active span | the four labels plus `eshu.producer_grant.producer_id` and `eshu.producer_grant.version` |
| Log line | `producer grant denied` (WARN) / `producer grant allowed` (INFO) | `producer_grant.*` keys |

`stage` is where the decision was made:

| Stage | Decision site | Emitted by a shipped binary today |
| --- | --- | --- |
| `install` | `eshu component install` | No (the CLI has no observer) |
| `readback` | Registry readback, including the worker's activation selection | Worker only |
| `activation` | `eshu component enable` and extension-host construction | Extension host only; `enable` is not emitted |
| `emission` | The recheck on every extension result | Worker |

An `allow` means the grant covers the kind, not that the install or result was
accepted; a later check can still fail it.

`reason` is `granted` for an allow and exactly one closed deny reason:

| Reason | Meaning |
| --- | --- |
| `no_matching_grant` | No grant names this producer, version, and kind. |
| `revoked` | Every matching grant is revoked. |
| `expired` | Every unrevoked matching grant is past its expiry. |
| `scope_mismatch` | A live grant exists but its scope is not a collector kind the manifest declares. |
| `schema_not_covered` | A live in-scope grant exists but does not cover the emitted schema version. |
| `grants_unreadable` | The grant set could not be read; the recheck denied fail-closed. |

Cardinality: `decision`, `stage`, and `reason` are closed sets and `fact_kind`
is bounded by the core fact-kind registry (any other value folds to `other`).
The producer id is operator-configured and unbounded, so it appears on the span
event and log line only, never as a metric label. No signal carries a
credential, grant scope, config value, or fact payload.

A denial still fails closed exactly as before (terminal `InvalidResult`, zero
facts); the signal is additive. Emission decisions are reported once per
distinct core kind per result, so an allowed result with many facts of one kind
counts once. The worker (`collector-component-extension`) wires the readback,
activation, and emission stages. The CLI, coordinator, and API construct
registries without an observer today, so their install/enable/readback
decisions surface through command output and exit status rather than this
counter.

Performance: the recheck adds one nil-check when no observer is configured and
one pre-built-attribute counter add (one 16-byte SDK allocation) per distinct
core kind per result when it is.
