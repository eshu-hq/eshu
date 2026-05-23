# packageregistry Agent Guidance

## Read First

1. `README.md` and `doc.go` for the package contract.
2. `identity.go`, `runtime_config.go`, and `parser_registry.go` for target and
   ecosystem identity rules.
3. Metadata parser files before changing fixture parsing.
4. Envelope files before changing durable fact identity or payloads.
5. `packageruntime/README.md` before changing live metadata fetching.
6. `docs/public/guides/collector-authoring.md` and
   `docs/public/reference/component-package-manager.md` for public contracts.

## Local Rules

- Treat registry metadata as reported evidence only. Do not claim canonical
  package ownership, dependency truth, graph truth, or query truth here.
- Keep ecosystem identity rules separate; npm, PyPI, Go modules, Maven, NuGet,
  Generic, and Artifactory wrapper metadata do not share one universal
  normalization rule.
- Use normalized identity for `StableFactKey` and scope/generation-specific
  identity for `FactID`.
- Strip URL credentials and sensitive token query parameters before payload or
  source-ref emission.
- Keep parsers local and deterministic. Live HTTP, credentials, rate limits,
  and workflow claims belong in `packageruntime`.
- Keep runtime config bounded by explicit provider, ecosystem, registry, scope,
  package limit, and version limit.
- Keep package names, private feed names, versions, URLs, artifact paths, and
  credentials out of metrics.

## Change Rules

- Add ecosystems by extending `Ecosystem`, normalization, parser registration,
  and table tests together.
- Add fact envelopes only after `internal/facts` exposes the fact kind and
  schema; keep source confidence explicit.
- Map new fixture parsers to observation structs without inventing graph truth,
  then register them explicitly.
- Do not move ECR here, materialize graph nodes or relationships, or flatten
  package-native dependency scopes into one generic dependency claim.
