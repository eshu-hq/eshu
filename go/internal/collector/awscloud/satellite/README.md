# AWS Satellite Component Port

## Purpose

`satellite` is the typed component port for the extracted AWS cloud
satellite (`eshu-hq/eshu-collector-aws-cloud`, cutover from eshu-hq/eshu
issue #8852). Host-side admission (`install`, `verify`, `conform`) and
activation planning judge the manifest this package builds, so the judged
document always matches the satellite's declared surface. This package
never scans AWS APIs, emits facts, claims work, or writes graph truth.

## Ownership boundary

This package owns the satellite component ID and version, the fifteen
emitted core-owned fact kinds with their schema versions, the reducer-phase
contract derived from the fact registry, and the required producer-grant set
(`ManifestFor`, `RequiredGrants`). It never touches Postgres, the graph,
the reducer, or the API/MCP surfaces, and it invents no protocol:
manifests speak the v1alpha1 shape the satellite collector emits.

It does not own claim planning (coordinator), grant minting (the
`eshu component grant` activation flow defines lifetimes), or the satellite
image publish (the image ref rides the satellite release; this port only
checks its digest-pinned shape).

## Exported surface

- `ComponentID`, `Version` — satellite identity and manifest version.
- `ManifestFor` — typed manifest builder; the image ref is publish-owned
  and must be digest-pinned.
- `RequiredGrants` — one live grant per declared family, scoped to `aws`.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/component` — manifest, grant, and artifact types the port
  is expressed in.
- `go/internal/facts`, `go/internal/facts/cloud` — fact-kind, schema
  version, and reducer-domain constants for the emitted families.
- `go/internal/collector/awscloud` — the shared `aws` collector-kind
  constant only; the port stays a light leaf otherwise.
- `github.com/eshu-hq/eshu-sdk/factschema/fixturepack` — payload-schema
  presence for the kind-keyed schema refs.

## Telemetry

This package emits no metrics, spans, or logs of its own. The manifest
carries the `eshu_collector_aws_cloud_` metrics prefix the satellite uses.

## Gotchas / invariants

- The manifest declares every kind the satellite can emit, including the
  `aws_ssm_host` pair the pinned SDK fixture pack has no schemas for yet.
  Fixture conformance reports exactly that pack gap; install, verify, and
  activation admission cover all fifteen kinds through the grants.
- `PayloadSchemaRef` rides fixture-pack coverage, not the registry: kinds
  without a shipped schema declare no ref so component validation stays
  green.
- Reducer phases derive from the fact registry at build time, so the
  manifest and the grant-derived trusted phases stay equal by construction.

## Related docs

- `docs/public/reference/component-package-manager.md`
- `docs/public/reference/component-producer-grants.md`
