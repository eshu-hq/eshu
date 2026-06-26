# Gradle Parser Audit

## Overview
Parses Gradle build script dependencies from `build.gradle` (Groovy DSL) and `build.gradle.kts` (Kotlin DSL) files using bounded string/text scanning — NOT by executing Gradle. Extracts Maven coordinate dependencies with their configuration names, version resolution states, and BOM/platform wrappers. Skips `project()`, `files()`, `fileTree()`, and version-catalog aliases. 6 src files, 1 test file. regexp.MustCompile in 3 files (scanner, blocks, statement splitter).

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **String-form dependencies**: `implementation 'groupId:artifactId:version'` (both Groovy single-quote and Kotlin parenthesized)
- **Map-form declarations**: `implementation group: 'g', name: 'a', version: 'v'`
- **Configuration names**: implementation, api, runtimeOnly, compileOnly, testImplementation, testRuntimeOnly, etc.
- **Platform/enforcedPlatform BOM wrappers**: `implementation platform('g:a:v')`
- **Buildscript classpath**: `buildscript { dependencies { classpath 'g:a:v' } }`
- **Version interpolation**: `def var = 'v'`, `ext { var = 'v' }`, `val var = "v"` resolved
- **Resolution states**: resolved (version found), partial (no version), unresolved (unknown interpolation)
- **Skip patterns**: `project(':x')`, `files(...)`, `fileTree(...)`, `libs.*`, `sourceSets.*`
- **Malformed DSL tolerance**: unbalanced braces/quotes in one statement do not affect other statements

## Verified-by-Test Constructs
- `TestParseGroovyStringFormDeclarations` (`parser_test.go:15`): 6 configuration keywords, Groovy single/double-quote forms
- `TestParseKotlinDSLStringFormDeclarations` (`parser_test.go:59`): parenthesized Kotlin forms
- `TestParseMarksPlatformBomWithDistinctSection` (`parser_test.go:97`): platform() and enforcedPlatform() sections and scope
- `TestParseGroovyMapFormDeclarations` (`parser_test.go:130`): Groovy map form (group: 'g', name: 'a', version: 'v')
- `TestParsePreservesUnresolvedVersionVariables` (`parser_test.go:157`): def/val/ext resolution, unresolved fallback
- `TestParseSkipsProjectDependenciesAndFileCollections` (`parser_test.go:196`): project(), files(), fileTree() skipped
- `TestParseSkipsGradleSourceSetAndVersionCatalogAliases` (`parser_test.go:219`): libs.*, sourceSets.* skipped
- `TestParseHandlesBuildscriptDependenciesWithDistinctSection` (`parser_test.go:241`): buildscript nested block with classpath
- `TestParseToleratesMalformedDSLWithoutSmugglingPartialRows` (`parser_test.go:276`): unbalanced quotes do not produce valid coordination
- `TestParseHandlesParenthesizedKotlinDSLForms` (`parser_test.go:300`): version-less parenthesized forms, platform(), kotlin()

## Unverified / Claimed-but-Untested Constructs
- **Kotlin DSL map form**: `implementation(group = "...", name = "...", version = "...")` — only Groovy map form tested
- **Annotation processor configurations** (`annotationProcessor`, `kapt`)
- **Gradle version catalogs** (documented as out of scope, but no test proving they are skipped)
- **Kotlin val interpolation**: `val springVersion = "5.3.20"` in .kts file — only def/ext tested

## Edge Cases Considered
- Both string literal forms: single-quoted and double-quoted
- Both DSL syntaxes: Groovy (build.gradle) and Kotlin (build.gradle.kts)
- BOM/platform with enforcedPlatform scope normalization
- Parenthesized Kotlin form with trailing lambda block `{ version { strictly("5.3.20") } }`
- Buildscript nested block not double-processed
- Unbalanced quotes in one statement don't contaminate others
- Unresolved interpolations preserved as raw text

## Edge Cases NOT Considered
- Empty dependencies block
- Gradle properties file interpolation
- `allprojects {}` / `subprojects {}` nesting
- Kotlin DSL `by` delegation patterns
- Multiple `dependencies {}` blocks in one file
- `configurations.all { resolutionStrategy }` influence
- Multi-module flag resolution (documented out of scope)
- The `constraints { }` block inside dependencies
- `api files(...)` or `api fileTree(...)` (only top-level project/file/fileTree covered)

## Verdict
**deep** — 1 test file (380 lines) with 10 named tests covering Groovy and Kotlin DSLs, string/map/parenthesized forms, platform BOMs, version interpolation from def/val/ext, buildscript classpath, skip lists (project/files/fileTree/libs/sourceSets), and malformed DSL tolerance. As a permanent exception that uses bounded text scanning over build scripts without executing Gradle, this is thorough.

## Recommended Actions
- Document that GRADLE is a **permanent exception** — bounded scanner over Groovy/Kotlin DSL, no tree-sitter, no Gradle execution
- Add a test for Kotlin DSL map-form declarations
- Add a test for `kapt` or `annotationProcessor` configurations (or document as explicitly unsupported)
- Add a test for completely empty/missing dependencies block
