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

No-Observability-Change: this change adds no metric, span, or log key.
Grant denials surface through the existing extension-failure
`InvalidResult` terminal channel with the producer, version, and kind in
the message; approvals leave no dedicated trace. A dedicated
grant-decision signal is tracked separately in #6726.
