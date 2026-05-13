# AGENTS.md — internal/collector/packageregistry guidance

## Read First

1. `README.md` — package purpose, exported surface, and invariants
2. `identity.go` — ecosystem-specific identity normalization
3. `runtime_config.go`, `parser_registry.go` — bounded target config and
   ecosystem parser registration
4. `metadata_parsers.go`, `gomod_parser.go`, `maven_parser.go`,
   `nuget_parser.go`, `metadata_parser_helpers.go` — local fixture parsing
   into observation structs
5. `envelope.go`, `version.go`, `dependency.go`, `artifact.go`,
   `source_hint.go`, `repository_hosting.go`, `warning.go` — durable
   fact-envelope construction
6. `docs/docs/adrs/2026-05-12-package-registry-collector.md` — source-truth
   boundary and implementation slices
7. `packageruntime/README.md` — claim-driven metadata fetch and commit flow

## Invariants

- Registry metadata is reported evidence. Do not claim canonical package
  ownership or dependency truth in this package.
- Keep ecosystem semantics separate. npm, PyPI, Go modules, Maven, NuGet, and
  generic feeds do not share one universal identity rule.
- Use normalized identity for `StableFactKey` and `FactID`.
- Use stable source-native keys for artifact, hosting, source-hint, and warning
  envelopes.
- Strip URL credentials and sensitive token query parameters before adding URLs
  to payloads or source refs.
- Keep metadata parsers local and deterministic. Live HTTP clients and workflow
  claims belong in `packageruntime`; graph writes and ownership decisions do not
  belong in this package tree.
- Keep runtime config bounded by explicit provider, ecosystem, registry, scope,
  package limits, and version limits. Do not let config imply full registry
  crawling.
- Add new ecosystem parsing through `MetadataParserRegistry` registration
  instead of a runtime source switch that hides package-native behavior.
- Do not put package names, private feed names, versions, URLs, or artifact
  paths in metrics.

## Common Changes

- Add a new ecosystem by extending `Ecosystem`, `normalizeNameForEcosystem`,
  and the table tests in `identity_test.go`.
- Add a new fact envelope builder only after `internal/facts` exposes the fact
  kind and schema version. Keep the source confidence explicit.
- Add package-native fixture parsing in this package only when it maps to
  existing observation structs without inventing graph truth, then register the
  parser explicitly with `MetadataParserRegistry`.
- Add live registry calls in `packageruntime`, not in identity helpers or
  envelope builders.

## What Not To Change Without An ADR

- Do not move ECR into package-registry support. ECR belongs to the OCI registry
  collector lane.
- Do not materialize graph nodes or relationships from this package.
- Do not flatten dependency scopes such as dev, peer, optional, runtime, target
  framework, or classifier-specific edges into a generic dependency claim.
