# AGENTS.md — internal/collector/packageregistry/packageruntime guidance

## Read First

1. `README.md` — runtime purpose, flow, telemetry, and invariants
2. `source.go` — claim-to-fact runtime path
3. `http_provider.go` — explicit metadata URL fetch boundary
4. `../runtime_config.go`, `../parser_registry.go` — shared config and parser
   contracts
5. `docs/docs/adrs/2026-05-12-package-registry-collector.md`

## Invariants

- Keep the runtime claim-driven. Do not add unbounded registry crawling or
  background enumeration from this package.
- Do not serialize concurrent collector workers as a fix for duplicate facts or
  claim conflicts. Fix idempotency, target partitioning, or claim fencing.
- Do not put package names, private feed URLs, versions, artifact paths, or
  credential env names in metric labels.
- Always copy the workflow `GenerationID` and fencing token into emitted fact
  observations.
- Keep advisory and registry-event observations inside the same package and
  version bounds as dependency, artifact, and source-hint observations.
- Treat metadata as reported evidence only. Reducers own any later graph truth.

## Common Changes

- Add provider-specific auth or request shaping behind `MetadataProvider`; keep
  parser behavior in `internal/collector/packageregistry`.
- Add telemetry in `internal/telemetry` before emitting it here.
- Add a regression test that exercises `ClaimedSource.NextClaimed` for new
  claim identity, retry, parse, or provider error behavior.

## What Not To Change Without An ADR

- Do not infer source repository ownership from package metadata in this
  package.
- Do not bypass `collector.ClaimedService` or write directly to Postgres.
- Do not add graph writes, reducer correlation, or query-surface behavior here.
