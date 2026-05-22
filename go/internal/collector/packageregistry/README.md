# Package Registry Collector Contracts

## Purpose

`packageregistry` normalizes package-registry identity, validates bounded target
configuration, registers metadata parsers, and builds reported-confidence facts
for the `package_registry` collector family.

## Ownership boundary

This package belongs to the collector boundary. It does not fetch live metadata,
claim workflow work, write graph state, or decide package ownership or
dependency truth. Runtime fetching lives in `packageruntime`; reducers promote
only corroborated evidence.

## Exported surface

Use `doc.go` and `go doc ./internal/collector/packageregistry` for the full
contract. The important surfaces are target validation, ecosystem identity
normalization, parser registration, metadata parsing, observation models, and
fact-envelope builders.

## Dependencies

`packageregistry` depends on `internal/facts` for durable envelopes. Runtime
collection depends on this package through `packageruntime`, not the other way
around.

## Telemetry

This package emits no metrics or spans directly. Hosted package-registry
runtimes record request, parse, fact-emission, rate-limit, lag, and status
signals around calls into this package.

## Gotchas / invariants

- Target config must stay bounded before any registry connection opens.
- Stable package identity uses ecosystem-native normalization, not display
  names.
- `FactID` is scope- and generation-specific; `StableFactKey` stays
  source-stable.
- Envelope payloads carry correlation anchors so reducers do not re-parse
  source-specific payload fields.
- Source hints, warning payloads, and source refs must strip URL credentials and
  sensitive query parameters.
- Artifactory package wrappers are hosting evidence plus package-native
  metadata. They do not prove source ownership or package consumption.
- ECR belongs to OCI registry evidence. JFrog can emit OCI or package-registry
  evidence depending on repository type.

## Related docs

- `go/internal/collector/packageregistry/packageruntime/README.md`
- `go/cmd/collector-package-registry/README.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/guides/collector-authoring.md`
