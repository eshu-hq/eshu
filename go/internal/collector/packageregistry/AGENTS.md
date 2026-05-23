# AGENTS.md - internal/collector/packageregistry

Use `README.md` and `doc.go` for the package contract. This file keeps the
agent-only rules for package identity, metadata parsing, and fact envelopes.

## Read First

1. `README.md`, `doc.go`, `identity.go`, `runtime_config.go`, and
   `parser_registry.go`.
2. Metadata parser files before changing fixture parsing.
3. Envelope files before changing durable fact identity or payloads.
4. `packageruntime/README.md` before changing live metadata fetching.
5. `docs/public/guides/collector-authoring.md` and
   `docs/public/reference/component-package-manager.md`.

## Mandatory Invariants

- Registry metadata is reported evidence. Do not claim canonical package
  ownership, dependency truth, or graph truth in this package.
- Keep ecosystem identity rules separate; npm, PyPI, Go modules, Maven, NuGet,
  and generic feeds do not share one universal normalization rule.
- Use normalized identity for `StableFactKey` and scope/generation-specific
  identity for `FactID`.
- Strip URL credentials and sensitive token query parameters before payload or
  source-ref emission.
- Metadata parsers stay local and deterministic. Live HTTP, credentials, and
  workflow claims belong in `packageruntime`.
- Runtime config stays bounded by explicit provider, ecosystem, registry,
  scope, package limits, and version limits.
- New ecosystem parsing goes through explicit `MetadataParserRegistry`
  registration.
- Package names, private feed names, versions, URLs, artifact paths, and
  credentials stay out of metrics.

## Change Routing

- New ecosystem: extend `Ecosystem`, normalization, parser registration, and
  table tests together.
- New fact envelope: add the fact kind/schema in `internal/facts` first, then
  add envelope tests here.
- New fixture parser: map to existing observation structs without inventing
  graph truth, then register it explicitly.
- Live registry calls go in `packageruntime`, not identity helpers or envelope
  builders.

## Do Not Change Without Owner Approval And Proof

- Do not move ECR into package-registry support; ECR belongs to OCI registry
  evidence.
- Do not materialize graph nodes or relationships from this package.
- Do not flatten package-native dependency scopes into one generic dependency
  claim.

## Required Proof

- Run `cd go && go test ./internal/collector/packageregistry -count=1`.
- Run `./internal/collector/packageregistry/packageruntime` tests when runtime
  config or live fetching is affected.
- For docs-only edits, run `go run ./cmd/eshu docs verify ../go/internal/collector/packageregistry --fail-on contradicted,missing_evidence` from `go/`.
