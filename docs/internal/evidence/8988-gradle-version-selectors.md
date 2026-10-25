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
  `638dab42...`. The parser change alone moves the collector output.

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
