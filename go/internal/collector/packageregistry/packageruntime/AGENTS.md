# packageruntime Agent Guidance

## Read First

1. `README.md` and `doc.go` for runtime flow and telemetry.
2. `source.go` for claim-to-fact behavior.
3. `http_provider.go` for explicit metadata URL fetch boundaries, timeouts,
   rate-limit classification, and source URI sanitization.
4. `../runtime_config.go` and `../parser_registry.go` for shared config and
   parser contracts.
5. `../README.md` for package-registry evidence boundaries.

## Local Rules

- Keep the runtime claim-driven. Do not add background enumeration or unbounded
  registry crawling.
- Fetch one explicit metadata document per target; do not infer additional feed
  URLs from provider names.
- Keep credentials in runtime config and request headers only. Do not copy
  package names, private feed URLs, versions, artifact paths, credential env
  names, or credential values into metric labels, logs, facts, docs, or PR text.
- Preserve `GenerationID`, `CollectorInstanceID`, and workflow fencing token in
  every emitted fact envelope.
- Keep advisory and registry-event observations within the same package and
  version bounds as package, dependency, artifact, and source-hint facts.
- Treat provider metadata as reported evidence only; reducers own later graph
  truth.
- Do not serialize workers to hide duplicate facts or claim conflicts. Fix
  idempotency, target partitioning, or claim fencing.

## Change Rules

- Add provider auth or request shaping behind `MetadataProvider`; keep parser
  behavior in `internal/collector/packageregistry`.
- Cover new claim identity, retry, parse, provider error, rate-limit, or
  redaction behavior with `ClaimedSource.NextClaimed` tests.
- Add telemetry through `internal/telemetry` before emitting it here, with
  bounded labels only.
- Do not bypass `collector.ClaimedService`, write directly to Postgres, add
  graph writes, or infer source repository ownership here.
