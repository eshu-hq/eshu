# Extraction PREP — package-registry collector dependency census (AC2 input)

Scope: [#8862](https://github.com/eshu-hq/eshu/issues/8862). In-tree implementation
only; no reducer, graph, deploy, or `sdk/go` change. Destination repository does
not exist yet (`eshu-hq/eshu-collector-package-registry`, inventory main
`50e556470fcddc99c953c7662ed4b0bc1995674e`).

## Moves to the destination (in scope)

- `go/cmd/collector-package-registry/`: `main.go` (claimed-live + cassette
  launch modes, hosted runtime), `config.go` (instance selection, target JSON,
  credential env resolution into `runtime.TargetConfig`), `service.go`
  (`buildClaimedService` via `runtime.ClaimedSource`, cassette service),
  `doc.go`, tests, `README.md`.
- `go/internal/collector/registry/package/`: parsers (`metadata_parsers.go`,
  `gomod_parser.go`, `maven_parser.go`, `nuget_parser.go`,
  `metadata_parser_helpers.go`), registry wrapper (`artifactory_parser.go`),
  envelopes (`envelope.go`, `version.go`, `dependency.go`, `artifact.go`,
  `source_hint.go`, `vulnerability_hint.go`, `registry_event.go`,
  `repository_hosting.go`, `warning.go`, `envelope_helpers.go`), identity
  (`identity.go`), bounds config (`config.go`, `types.go`),
  `parser_registry.go`.
- `go/internal/collector/registry/package/runtime/`: claim source (`source.go`,
  `source_warnings.go`, `source_missing_evidence.go`, `derived_targets.go`),
  fetch (`http_provider.go`, 20MiB cap), bounds (`bounds.go`),
  `freshness.go`, `source_uri.go`, plus tests and `README.md` files.

## Stays in core, consumed under explicit grant (never copied)

| Core surface | Used for |
|---|---|
| `go/internal/collector` | `ClaimedService`, `ClaimedSource`, `CollectedGeneration`, `FactsFromSlice`, registry failure constructors |
| `go/internal/collector/sdk` | Default HTTP client, retry-after parsing |
| `go/internal/workflow` | `WorkItem`, `DesiredCollectorInstance`, control store, claim TTL defaults |
| `go/internal/scope` | `CollectorPackageRegistry`, `IngestionScope`, `ScopeGeneration`, pending/snapshot status |
| `go/internal/facts` | `Envelope`, `StableID`, `SourceConfidenceReported` |
| `go/internal/facts/supply/chain` | 9 package-registry fact kinds, `1.0.0` schema versions, family declaration (already names `PlannedRepository: eshu-hq/eshu-collector-package-registry`) |
| `sdk/go/factschema` + `packageregistry/v1` | Versioned payload contract encoders |
| `go/internal/packageidentity` | Ecosystem, raw/normalized identity behind `NormalizePackageIdentity` |
| `go/internal/storage/postgres` | Ingestion committer, workflow control store, status store |
| `go/internal/urlredact` | Credential detection behind `sanitizeURL` fail-closed path |
| `go/internal/{app,buildinfo,runtime,telemetry,replay/cassette}` | Binary hosting, version flag, pprof, Postgres open, OTEL, cassette mode |

## Fact kinds, schema versions, consumers

All nine kinds are at schema `1.0.0`. Canonical projection consumes package,
package_version, package_dependency, package_artifact, registry_event,
source_hint. `repository_hosting` and `vulnerability_hint` are declared
unconsumed/join-key-only; `warning` is read-only. Reducer projection,
canonical graph, HTTP API, and MCP truth stay core-owned (AC4, NOT_CHECKED).

## Durable-state ownership

Core keeps admission, canonical identity, projection, persistence, recovery,
bootstrap, and repair. The extracted producer owns observation parsing,
envelope construction, claim-driven fetch, and warning evidence. No
shared-table shortcuts: the producer commits only through the ingestion
committer and the workflow control plane.

## Protocols and artifacts

- HTTPS metadata fetch per ecosystem with ecosystem Accept headers (npm
  packument, PyPI JSON, Go proxy bundle, Maven XML, NuGet XML, generic JSON,
  Artifactory package wrapper). No full-registry crawling; every target is
  explicit provider + ecosystem + registry + scope + package/version limits.
- Claim/lease/commit through `collector.ClaimedService` against Postgres.
- Artifact: `eshu-collector-package-registry` binary (`-mode
  claimed-live|cassette`); image and deployment manifest are destination work.

## Prerequisites (tracked, not in this worktree)

#6708 SDK publication, #6709 producer delegation, #6710 standalone template,
#4047 gate implementation, #6707 SDK relocation. Production acceptance,
deployment, and rollback rehearsal remain NOT_CHECKED (AC8-10).
