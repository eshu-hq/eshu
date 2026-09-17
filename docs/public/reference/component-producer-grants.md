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
  the fail-closed core-owned rejection.
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
