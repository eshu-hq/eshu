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
  `PackageManifestDependency.ConsumptionKeys` (the one place a dependency row
  becomes registry keys; an sbt row joins only the Maven artifact it publishes,
  #8961),
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

## Evidence (#8961)

Performance Evidence: `go test -run '^$' -bench BenchmarkManifestConsumptionKeys
-benchmem -count=3 ./internal/packageidentity` (linux, 16 CPUs, load average
6 to 13) measured the helper every manifest site now calls. A non-sbt row costs
the same as the direct `ConsumptionKeys` call it replaced: 669 to 811 ns/op,
408 B/op, 11 allocs/op direct against 671 to 685 ns/op, 408 B/op, 11 allocs/op
through the helper (two runs, the spread is host load). An sbt `%%` row with a
known artifact costs 906 to 924 ns/op, 680 B/op, 16 allocs/op. The Postgres read this change widens is measured
in `go/internal/storage/postgres/facts_active_package_manifest_dependency.go`
(throwaway Postgres 18, 1.3M dependency rows, 13,000 sbt rows, EXPLAIN
(ANALYZE, BUFFERS)).
The one-time sidecar repair for stored sbt rows
(`234_package_manifest_consumption_keys_sbt_repair.sql`) marked 40 scopes from
13,000 sbt rows in 52 ms on the same table with a cold cache (one index scan on
the `package_manager = 'sbt'` prefix, no sequential scan); re-running it
changes nothing but the marks.

No-Regression Evidence: `go test -race -count=1 ./internal/packageidentity
./internal/reducer/... ./internal/storage/postgres ./cmd/reducer` passed; the
non-sbt key sets are asserted byte-identical to `ConsumptionKeys` in
`TestManifestConsumptionKeysSBTJoinRules`, and the non-Maven read keeps the
original query text and arguments
(`TestListActivePackageManifestDependencyFactsKeepsNonMavenQueryUnchanged`).

No-Observability-Change: the join is a pure function inside existing reducer
handlers; the reducer run span, execution counters, and the package
consumption counts they already emit cover it. A `%%` row with no known Scala
binary version joins nothing on purpose, and the operator reads why from the
row's `sbt_state` and `sbt_cross_version` on the content entity.
