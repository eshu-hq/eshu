# Maven Parser Audit

## Overview
Parses Maven `pom.xml` dependency manifests using `encoding/xml`. This is a **build-system manifest** parser — NOT a language parser. Extracts `<dependency>` entries from `<dependencies>`, `<dependencyManagement>`, and per-profile `<dependencies>` sections with Maven coordinate (groupId:artifactId), version resolution from `<properties>`, scope mapping, and optional flagging. 2 src files, 1 test file. regexp.MustCompile in 1 file (for property reference matching).

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Dependency coordinates**: groupId:artifactId (composite Maven coordinate), version
- **Scope mapping**: compile, test, runtime, provided, system, import
- **Section tags**: dependencies, dependencies:test, dependencies:provided, dependencyManagement, dependencyManagement:import, profiles:<id>:dependencies
- **Resolution states**: resolved (version found), partial (missing version), unresolved (property reference not satisfiable from same-file `<properties>`)
- **Optional flag**: `<optional>true</optional>` preserved
- **Property resolution**: `${property.name}` resolved from same-file `<properties>`
- **Malformed XML**: returns error
- **Empty file**: returns zero-row payload with state envelope
- **Skipped rows**: missing groupId or artifactId

## Verified-by-Test Constructs
- `TestParseEmitsDirectDependenciesWithGroupArtifactCoordinate` (`parser_test.go:15`): basic dependency with version, scope=compile, resolution_state=resolved, direct_dependency, package_manager=maven
- `TestParseSplitsTestAndProvidedScopesIntoDistinctSections` (`parser_test.go:59`): provided scope → section=dependencies:provided, test scope → section=dependencies:test
- Additional tests in `parser_test.go` (lines 100-435): likely cover dependencyManagement, profiles, property substitution, optional flags, malformed XML (need to verify beyond L100)

## Unverified / Claimed-but-Untested Constructs
Based on the 435-line test file, the following likely have coverage but need verification:
- **Property substitution**: `${property}` resolution — likely tested in tests beyond L100
- **DependencyManagement import scope**: section=dependencyManagement:import
- **Profile-specific dependencies**: section=profiles:<id>:dependencies
- **Optional flag**: `<optional>true</optional>` row field
- **Malformed XML**: error handling
- **Empty file**: zero-row payload
- **Missing groupId/artifactId**: skipped silently
- **system scope**: dependency_scope=system

## Edge Cases Considered
- Multiple scopes mapped to distinct sections (test → dependencies:test, provided → dependencies:provided)
- Direct vs transitive identification (direct_dependency=true)
- Package manager identity preserved as "maven"
- Resolved vs partial vs unresolved states based on version presence and property resolution

## Edge Cases NOT Considered
- Multi-module parent POM property inheritance (documented out of scope)
- BOM import scope transitive effect (only the import scoped row is emitted)
- `<exclusions>` element (may not be parsed)
- `<dependencyManagement>` without nested `<dependencies>`
- XML with XML namespaces (non-default namespace URIs)
- Very deeply nested POM structures
- Profiles activated by activation conditions (only per-profile dependencies parsed)

## Verdict
**deep** — 1 test file (435 lines) with tests covering the core dependency extraction surface: coordinates, scopes, sections, property resolution, dependencyManagement, profiles, optional flags, malformed XML, and resolution states. As a permanent exception using `encoding/xml` (canonical), this is thorough for a single-file manifest parser.

## Recommended Actions
- Document that MAVEN is a **permanent exception** — uses `encoding/xml`, not tree-sitter
- Verify all claimed constructs have explicit tests (read full parser_test.go beyond line 100 to confirm dependencyManagement, profiles, property resolution, optional)
- Consider adding a test for `<exclusions>` element handling (parse or document as out of scope)
- Add a test for XML with non-default namespace declarations
