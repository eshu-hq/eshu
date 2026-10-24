# Gradle Version Selector Rule

## Purpose

`Exact(text)` decides whether Gradle version text names one exact version.
Gradle reads much version text as a selector instead:

| Form | Example | Gradle meaning |
| --- | --- | --- |
| Prefix | `1.+`, `1.0.+`, `+` | highest version with that prefix |
| Status | `latest.release`, `latest.integration` | highest version with that status |
| Range | `[1.0,2.0)`, `]1.0,2.0[`, `(,2.0]`, `[1.0,)` | any version in the range |
| Strict | `1.0!!`, `[1.0,2.0)!!1.5` | strictly that version or range |
| Artifact only | `1.0@jar` | that version, artifact type `jar` only |

None of those texts is an installed version. Nor is text with a `:`, the
string-notation separator: a version never holds one, so a `:` means a
classifier or other notation text leaked into the version. A row that marks one resolved
lets the reducer use `1.+` as the observed version for package matching and
security-alert comparison (issue #8988). `Exact` rejects all of them and any
text outside a printable-ASCII floor, so a reader marks the row unresolved.

Sources: the Gradle user manual pages "Declaring Versions and Ranges" and
"Declaring Rich Versions" (prefix, `latest.<status>`, Maven-style ranges, the
`!!` short-hand), and the artifact-only notation in "Declaring Dependencies".

## Shared rule

`testdata/versions.txt` is the one table. The build-script parser
(`go/internal/parser/gradle`) runs every row through Groovy string, Groovy map,
Kotlin string, Kotlin named-argument, `kotlin()` helper, `platform()`, and
same-file interpolation notations. The version-catalog parser adopts the same
call and table. A reader that disagrees with the table is the bug.

Gradle itself treats only a trailing `+` as a prefix selector. `Exact`
rejects every `+`, so `1.0.0+build.1` also stays unresolved. That costs an
observed version for a rare Maven version shape and never produces a wrong
one.

## Telemetry

None. `Exact` is a pure, allocation-free function on the parse path.
Parse timing stays with the parent parser engine.

## Verification

`go test ./internal/parser/gradle/... -count=1`.
