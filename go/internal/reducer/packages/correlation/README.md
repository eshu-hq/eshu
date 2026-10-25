# correlation

The package correlation family: package-source (ownership), package-consumption,
and package-publication correlation (issue #6061, step 1 of
`docs/internal/design/reducer-target-tree.md`).

## Ownership

This package owns the join from package-registry identities to owning
repositories and consumer edges: it reads package-registry package/version
facts, source-hint facts, repository facts, and manifest/lockfile dependency
evidence, and writes reducer-derived ownership/consumption/publication
correlation facts plus repo-to-repo `DEPENDS_ON` projection intents. The
reducer runtime drives `PackageSourceHandler` through the default
domain catalog; `go/cmd/reducer` wires the Postgres writer. Query surfaces
read the durable facts; they never import this package.

## Files

| File | Covers |
| --- | --- |
| `source.go` | Source-hint classification, repository-ID narrowing, outcome derivation |
| `source_handler.go` | `PackageSourceHandler`, fact loading, refresh/retract edges, narrow loader interfaces |
| `source_admission_decisions.go` | Ownership/consumption/publication admission writers |
| `consumption.go` | Manifest-dependency extraction (central version pins such as a NuGet `PackageVersion` with `central_version_declaration` are excluded, #8824; the flag is ASCII-trimmed like the Postgres sidecar and readiness SQL, so all three readers agree), consumption keys, consumption builder |
| `consumption_manifest_metadata.go` | Manifest metadata join and payload shaping |
| `consumption_repo_edge.go` | Owner resolution, consumer-edge intents, repo-edge writer |
| `consumption_rubygems.go` | RubyGems composite-version join |
| `payloads.go` | Typed payload construction for the three builders |
| `writer.go` | Batched versioned Postgres writer for correlation facts |
| `provenance_edges.go` | PUBLISHES provenance projection |
| `publication.go` | Publication builder |
| `security_alert_manifest_dependency_match.go` | Manifest bridge the securityalert family matches alerts through |
| `provenance_root_compat_exports.go` | Test seam for the reducer root's own test files (containerimage precedent) |

## Seams other families use

- Manifest evidence: `ExtractPackageManifestDependencies`,
  `PackageConsumptionKeys`, `PackageConsumptionNameCandidates`,
  `ExtractPackageRegistryIdentities`, `PackageManifestDependency`,
  `PackageRegistryIdentity`, `PackageManifestDependencyFactFilter`.
- Owner resolution: `ResolvePackageOwners`, `PackageOwnerResolution`,
  `ExtractSecurityAlertManifestConsumptions`,
  `SecurityAlertPackageNameMatches(Dependency)`.
- Builders/writers: `BuildPackageSourceDecisions`,
  `BuildPackageConsumptionDecisions`, `BuildPackagePublicationDecisions`,
  `PostgresPackageWriter`, `PackageProvenanceEdgeWriter`,
  `PackageConsumptionRepoDependencyInput`,
  `BuildPackageConsumptionRepoDependencyIntents`.
- Durable kinds: `PackageOwnershipFactKind`,
  `PackageConsumptionFactKind`,
  `PackagePublicationFactKind`.
- Narrow loaders: `ActiveRepositoryFactLoader`,
  `ActivePackageManifestDependencyFactLoader`,
  `HasPackageSourceRepositoryFact`.

## Dependency rule

One-way imports only: this package reads the shared tier (`contract`,
`factload`, `factwrite`, `crossrepo`, `sharedintent`, `payloadcore`,
`admissiondecision`, `packages/source`), `facts`, `packageidentity`,
`telemetry`, and `securityalert`'s exported types. It never imports the
parent reducer package. The parent references it only through
`correlation`-qualified names.

## Evidence

No-Regression Evidence: the unresolved-row observed-version fix (issue
#9101) adds one string equality check on `dependency_resolution_state`
before the existing syntactic gate in `packageManifestObservedVersion` —
constant time, no allocations, no new branches downstream. Baseline is the
suite green before the change; after measurement is the same suites green
after it, plus the two new red-first regression tests:

```bash
go test -count=1 ./internal/reducer/packages/correlation/ ./internal/reducer/ \
  ./internal/reducer/payloadcore/ ./internal/reducer/securityalert/
```

Input shape is manifest payload maps carrying `dependency_resolution_state`
`unresolved` with exact-looking literals (`1.0!`, `1.0.0+build.1`); resolved
and key-absent rows take the byte-identical old path, proven by the
unchanged suites. No backend, storage, queue, or graph write is involved —
the predicate runs in memory during projection.

No-Observability-Change: no metric, span, log line, or status field changes;
the existing decision fields already surface the row state.
