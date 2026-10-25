# Gradle Version Selectors Stay Unresolved (#8988)

## Change

`build.gradle` and `build.gradle.kts` rows with a Gradle selector as the
version were marked `resolved`: `1.+`, `latest.release`, `[1.0,2.0)`, `1.0!!`,
`1.0@jar`. The reducer then took the selector text as the observed version.
The parser now calls `selector.Exact` (`go/internal/parser/gradle/selector`)
after same-file interpolation. A selector row is `unresolved` and keeps the
text as written. The reducer backstop `payloadcore.ExactManifestDependencyVersion`
rejects a trailing `+`, a `latest.` prefix, `!!`, `@`, and any `:`
(a colon means a classifier or other notation text leaked into the version).
Security-alert
reconciliation forwards to that function. The git collector content version
moves to `git-collector.v12`, so each stored
repository re-collects once and its stale `resolved` rows are replaced.

The same string-notation reader stored a classifier as part of the version:
`g:a:1.0:jdk8` had `value` `1.0:jdk8`, and `g:a@jar` had name `g:a@jar`.
`parseCoordinateLiteral` now reads `group:name[:version[:classifier]][@ext]`.
The classifier never joins the version, and `selector.Exact` rejects `:`.
The reducer backstop rejects `:` through `GradleVersionSelector`: a version never holds one, so a `:` means a classifier or other notation text leaked into the version. No parser in the tree emits Debian epochs, and the rejection is pinned by `TestExactManifestDependencyVersionRejectsGradleSelectors` and `TestPackageManifestObservedVersionRejectsColonBearingGradleValue`.

The exact-version check in package correlation
(`packageManifestObservedVersion`) read `resolved_version` or `value` without
the row's `dependency_resolution_state`. A Gradle map-form `1.0:jdk8`, a `1.0!`
or a `1.0+build.1` that the Gradle rule left `unresolved`, and an sbt version
identifier the parser could not resolve, still passed the cross-ecosystem check
and became `ObservedVersion`. An `unresolved` row now observes no version. The
manifest security-alert bridge carries the state on `SecurityAlertConsumption`
and skips the text fallback for an unresolved row. A `partial` row keeps its
answer: an sbt row whose scope is a placeholder is `partial` with an exact
literal. The persisted `reducer_package_consumption_correlation` fact now
carries the state in an additive optional `dependency_resolution_state` field
(`reducerderived/v1`, a minor change). The consumption writer sets it when the
parser gave one. Both readers of the persisted fact, security-alert
reconciliation and supply-chain impact, pass it to their
`exactConsumptionDependencyVersion`, which refuses an `unresolved` row, so the
observed-version rule is one rule on every path. A fact written before the
field has no state and keeps the text fallback, so an old fact that already
carries `1.0:jdk8` stays wrong until the repository is re-materialized. No
migration. The golden snapshot holds no `reducer_package_consumption_correlation`
fact, so it does not change.

State-gate proof (a throwaway generated corpus, deleted after the run, compared
the old reader with a gate on `unresolved` and with a gate on every
non-`resolved` state): gradle build scripts 5,829 rows, version catalogs 11,439,
Maven poms 6,000, sbt builds 7,210, and nine ecosystems with no state
(npm, pypi, nuget, cargo, go, rubygems, composer, maven-lock, pub; 64 rows each, lockfile
and not). The `unresolved` gate changed only Gradle `1.0!` (237 rows),
`1.0+build.1` (282), `1.0:jdk8` (103) and sbt identifiers (390); it changed no
Maven, catalog or no-state row. A gate on every non-`resolved` state would also
have dropped 1,793 sbt `partial` rows that carry a version text, which
is why `partial` is not gated.

## Accuracy proof

- The shared table `go/internal/parser/gradle/selector/testdata/versions.txt`
  has 13 exact rows and 28 not-exact rows. Each row runs through eight
  notations: Groovy string, Groovy map, `platform()`, Groovy interpolation,
  Kotlin string, Kotlin named arguments, `kotlin()`, and Kotlin interpolation.
- The seeded oracle is `TestParseGeneratedVersionsMatchTheGradleOracle`, seed
  8988. Its generator is written from the Gradle user manual ("Declaring
  Versions and Ranges", "Declaring Rich Versions", and the artifact-only
  notation), and the family label of each version is the oracle. The default
  run is 3,000 scripts. One run of 30,000
  (`ESHU_GRADLE_SELECTOR_FUZZ_ROUNDS=30000`) passed in 33.8 s.
- The generator also writes string notation with a classifier, with and
  without `@ext`. The 30,000-script run passed again after that change, in
  31.7 s.
- 16 overlay mutants were all killed by assertion failures, not by build
  errors. They removed each reject class in `exact.go` (`:` included), the
  case fold, the character floor, the `buildRow` guard, the assignment that
  keeps the literal, the reducer's trailing-`+` and `@` rejects, and each part
  of the classifier and `@ext` split.
- Content-version pin: on the same fixture, base `statement.go` and
  `coordinate.go` give digest `5afd66e7...`, and head gives the pinned
  `638dab42...`. The parser change alone moves the collector output. The version-catalog change leaves the digest unchanged.

## No-Regression Evidence:

The benchmark is `BenchmarkParseConfig/gradle` in `go/internal/parser`. It
parses a 10,001-line `build.gradle` through `DefaultEngine().ParsePath`. Each
arm ran six times, interleaved, on the same arm64 host. The base arm overlaid
`statement.go` and `coordinate.go` from `origin/main`.

| Arm | ns/op mean (min-max) | B/op mean | allocs/op range |
| --- | --- | --- | --- |
| base (origin/main) | 55,779,893 (54.9-56.4 ms) | 12,755,064 | 119,739-119,761 |
| head | 56,565,498 (55.4-57.4 ms) | 12,942,978 | 119,748-119,760 |

Head is 1.4% slower, and the two ranges overlap. Allocations per op are
unchanged. Bytes per op rise 1.5%: the string-notation split now keeps four
parts, so its slice moves from the 48-byte to the 64-byte size class, about
16 bytes per dependency row. `selector.Exact` allocates nothing
(`TestExactAllocatesNothing`). The reducer change adds four constant-time
string checks per call. It touches no query, lock, queue or batch. The
content-version bump costs one extra generation per repository on its next
sync, the cost the gate makes explicit (#7978).

## No-Observability-Change:

This change adds no metric, span or log. A selector row is visible through
the existing `dependency_resolution_state=unresolved` value. On the alert
path it is visible through the existing `installed package version missing`
evidence string. Parse timing stays with the parent parser engine.

## Known limits

- Rich-version closures (`version { strictly(...) }`) are not read and stay
  `partial`.

## Observed-version state gate (follow-up to #9023)

The package-correlation reader gates an `unresolved` row (#9101). This change
carries the same rule to the two persisted-fact readers, security-alert
reconciliation and supply-chain impact, through an optional
`dependency_resolution_state` on the persisted consumption fact. The gate
changes only rows a parser left unresolved (Gradle `1.0!`, `1.0+build.1`,
map-form `1.0:jdk8`, sbt version identifiers); `partial`, `resolved` and
no-state rows keep today's answer.

No-Regression Evidence: the added work is one trimmed, case-folded string
compare per consumption row in `exactConsumptionDependencyVersion`, on a
reducer path that runs once per reconciliation, not per query. Baseline
`origin/main` against this change, the same throwaway benchmark over four
representative versions, 3,000,000 iterations, three runs each on one shared
dev host: 448 to 450 ns per four rows and 2 allocations before, 462 to 463 ns
and 2 allocations after, about 4 ns per row and no new allocation. The figure
is a single-host measurement, not a throughput claim, and the benchmark was
removed after the run. Backend: none (pure in-memory decision; no SQL or
Cypher changed).

No-Observability-Change: the gate adds no metric, span or log key. An operator
sees the effect as an empty `ObservedVersion` on the affected consumption row,
which the existing `missing_evidence` field on the reconciliation row already
reports for a row with no observed version.
