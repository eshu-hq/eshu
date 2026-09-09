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
reducer runtime drives `PackageSourceCorrelationHandler` through the default
domain catalog; `go/cmd/reducer` wires the Postgres writer. Query surfaces
read the durable facts; they never import this package.

## Files

| File | Covers |
| --- | --- |
| `source.go` | Source-hint classification, repository-ID narrowing, outcome derivation |
| `source_handler.go` | `PackageSourceCorrelationHandler`, fact loading, refresh/retract edges, narrow loader interfaces |
| `source_admission_decisions.go` | Ownership/consumption/publication admission writers |
| `consumption.go` | Manifest-dependency extraction, consumption keys, consumption builder |
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
- Builders/writers: `BuildPackageSourceCorrelationDecisions`,
  `BuildPackageConsumptionDecisions`, `BuildPackagePublicationDecisions`,
  `PostgresPackageCorrelationWriter`, `PackageProvenanceEdgeWriter`,
  `PackageConsumptionRepoDependencyInput`,
  `BuildPackageConsumptionRepoDependencyIntents`.
- Durable kinds: `PackageOwnershipCorrelationFactKind`,
  `PackageConsumptionCorrelationFactKind`,
  `PackagePublicationCorrelationFactKind`.
- Narrow loaders: `ActiveRepositoryFactLoader`,
  `ActivePackageManifestDependencyFactLoader`,
  `HasPackageSourceRepositoryFact`.

## Dependency rule

One-way imports only: this package reads the shared tier (`contract`,
`factload`, `factwrite`, `crossrepo`, `sharedintent`, `payloadcore`,
`admissiondecision`, `packagesourcecore`), `facts`, `packageidentity`,
`telemetry`, and `securityalert`'s exported types. It never imports the
parent reducer package. The parent references it only through
`correlation`-qualified names.
