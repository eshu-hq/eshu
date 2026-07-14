# AGENTS.md - internal/parser/gradle guidance

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for the Gradle manifest parser.
3. `parser.go` - `Parse`, block detection, statement splitting, coordinate
   extraction, and version interpolation resolution.
4. `parser_test.go` - behavior coverage for Groovy and Kotlin DSL string,
   map, and parenthesized forms, platform/enforcedPlatform BOMs,
   buildscript classpath dependencies, malformed DSL tolerance, and
   `project()`/`files()`/`fileTree()` skipping.
5. `../json/dependency_coverage.go` - matrix entries this package backs.

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import
  this package, but this package must not import `internal/parser`.
- Never execute Gradle, evaluate Groovy or Kotlin, run source-set
  resolution, or read sibling files. Each build script is parsed in
  isolation from file bytes only.
- `dependency_resolution_state` must be one of `resolved`, `partial`, or
  `unresolved`; partial/unresolved entries keep `value` as the literal text
  found in the file so callers can see exactly what was declared.
- Statements that name `project()`, `files()`, `fileTree()`, `gradleApi()`,
  or `localGroovy()` MUST be skipped. They are not Maven coordinates and
  cannot become package consumption decisions.
- Map-form declarations require both `group` and `name`; rows with only one
  of the two are skipped.

## Common changes and how to scope them

- Add a new failing test in `parser_test.go` before changing `parser.go`.
- New configuration keywords belong in `configurationKeywords` only when
  they are documented Gradle defaults or framework conventions that map
  cleanly to a Maven scope.
- Version catalog (`libs.versions.toml`) support belongs in a separate
  TOML parser package, not here.
- Telemetry, registry dispatch, and engine wrappers belong in the parent
  parser package.

## Failure modes and how to debug

- Missing dependency rows usually mean the `dependencies { }` block was not
  detected at parse time. Re-check `blockHeaderAtCursor` and
  `collectBlocks` in `blocks.go`.
- Spurious `unresolved` states often mean the interpolation references a
  Gradle-managed property (e.g. `project.version`) the parser cannot prove
  from the file alone. Leave it unresolved rather than guessing.
- Statement splitter regressions usually come from unbalanced parens inside
  configuration closures; check `splitDependencyStatements` brace and
  paren tracking.

## Anti-patterns specific to this package

- Calling out to the Gradle daemon or evaluating Groovy/Kotlin to resolve
  versions.
- Treating `project(":x")` as a Maven coordinate.
- Inferring a version from `latest.release`, `+`, or catalog aliases.

## What NOT to change without an ADR

- Cross-file resolution (settings.gradle, subprojects, version catalogs).
- The `dependency_resolution_state` vocabulary; reducer and docs depend on
  the three-value set.
- The package-manager identifier (`gradle`); changing it would break
  ecosystem normalization in the reducer.

## Evidence notes

### Map-form regex compile hoist (issue #4874)

`extractMapEntry` compiled two `regexp.MustCompile` patterns per call for
whatever key it was given (`parseMapCoordinate` always calls it with `group`,
`name`, and `version`). The per-call compile is now a package-level
`mapEntryPatternCache` (`sync.Map`, keyed by key) that compiles once per
distinct key and reuses the `*regexp.Regexp` afterward; `extractMapEntry`
keeps its original any-key contract, it just no longer recompiles the same
key's patterns on every call. `TestExtractMapEntryMatchesColonAndEqualsForms`
and `TestParseMapCoordinateCombinesGroupNameVersion` pin the exact matched
values for both the Groovy colon form and the Kotlin DSL equals form before
and after the hoist. The identical hoist-to-package-level-cache technique is
benchmarked directly on the SCIP and dbtsql sibling sites in this issue (see
`../AGENTS.md#evidence-notes` and `../dbtsql/AGENTS.md`); this package relies
on those measurements plus its own regression tests rather than a separate
gradle-specific benchmark, since the win is the same "stop recompiling an
identical pattern" mechanism against Go's `regexp` package.

No-Observability-Change: this package still emits no metrics, spans, or logs.
Parser timing remains owned by the ingester and collector runtime paths that
call the parent Engine.
