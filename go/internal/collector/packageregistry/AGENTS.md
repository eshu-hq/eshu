# AGENTS.md — internal/collector/packageregistry guidance

## Read First

1. `README.md` — package purpose, exported surface, and invariants
2. `identity.go` — ecosystem-specific identity normalization
3. `envelope.go` — durable fact-envelope construction
4. `docs/docs/adrs/2026-05-12-package-registry-collector.md` — source-truth
   boundary and implementation slices

## Invariants

- Registry metadata is reported evidence. Do not claim canonical package
  ownership or dependency truth in this package.
- Keep ecosystem semantics separate. npm, PyPI, Go modules, Maven, NuGet, and
  generic feeds do not share one universal identity rule.
- Use normalized identity for `StableFactKey` and `FactID`.
- Do not put package names, private feed names, versions, URLs, or artifact
  paths in metrics.

## Common Changes

- Add a new ecosystem by extending `Ecosystem`, `normalizeNameForEcosystem`,
  and the table tests in `identity_test.go`.
- Add a new fact envelope builder only after `internal/facts` exposes the fact
  kind and schema version. Keep the source confidence explicit.
- Add live registry calls in a runtime subpackage or later collector slice, not
  in the identity helpers.

## What Not To Change Without An ADR

- Do not move ECR into package-registry support. ECR belongs to the OCI registry
  collector lane.
- Do not materialize graph nodes or relationships from this package.
- Do not flatten dependency scopes such as dev, peer, optional, runtime, target
  framework, or classifier-specific edges into a generic dependency claim.
