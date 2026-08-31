# Parser Quality Baseline and Test Convention

## Scope

This document records the corrected parser baseline and the parent-level test
convention for the `go/internal/parser/` package. It exists so the parser test
architecture is not re-filed as a zero-coverage gap by future tools that count
only `_test.go` files inside language subdirectories.

## Parser Inventory

The `go/internal/parser/` directory contains **38 immediate subdirectories** as
of 2026-06-26. These fall into three categories:

### Language parsers (19)

`c`, `cpp`, `csharp`, `dart`, `elixir`, `golang`, `groovy`, `haskell`, `java`,
`javascript`, `kotlin`, `perl`, `php`, `python`, `ruby`, `rust`, `scala`, `sql`,
`swift`

### Manifest and declarative-data parsers (11, permanent exceptions)

`cloudformation`, `dbtsql`, `dockerfile`, `gomod`, `gradle`, `hcl`, `json`,
`maven`, `nodelockfile`, `pythondep`, `yaml`

These are documented **permanent exceptions** in the parser taxonomy. They use
canonical format-specific decoders (`encoding/json`, `gopkg.in/yaml.v3`,
`hcl/v2`, `modfile`, `encoding/xml`), bounded text scanners, or regex lineage
extraction — not tree-sitter.

### Internal engine packages (8)

`cfg`, `dataflowemit`, `goldenaudit`, `interproc`, `shared`, `summary`, `taint`,
`valueflow`

These support the parser engine but are not parsers themselves. They are out of
scope for parser-depth audits.

## Parser Integration Test Convention

Issue #6062 moves language-specific black-box engine tests into the owning
language directory. These tests use the external `<language>_test` package so
they can import the parent parser, drive `parser.DefaultEngine`, and assert the
full discovery and parse path without reversing the production dependency from
the parent engine to its language adapter.

Rust is one migrated example. Its `engine_rust_lifetimes_test.go`,
`engine_rust_module_resolution_test.go`, `rust_route_entries_test.go`, and
`rust_cargo_dependency_test.go` files now live under `go/internal/parser/rust/`
as package `rust_test`. The in-package `rust` tests in the same directory still
cover adapter helpers and parsing details directly.

The parent package root (`go/internal/parser/`) continues to own shared and
cross-language engine tests. Rust coverage in `engine_systems_test.go` and the
Rust cases in `engine_cyclomatic_complexity_test.go` stays there because those
suites exercise common engine behavior. Language-specific tests that have not
yet moved also remain at the parent root until their migration lands.

Test location therefore records ownership, not depth of coverage. A language
may have external engine tests in its subdirectory, parent-root coverage in a
shared suite, package-internal unit tests, or a mix of those forms. Counting
only one directory still produces a false coverage gap.

## Corrected Baseline

The original P1 framing ("c, kotlin, php, scala, swift — 49 src files with zero
test coverage") counted only `_test.go` files inside each language subdirectory.
The current tree proves the opposite through both migrated subdirectory tests
and retained parent-root coverage:

| Parser | Parent-root `_test.go` files | Subdirectory `_test.go` files | Verdict |
|--------|------------------------------|-------------------------------|---------|
| kotlin | 16 | 3 | Deep |
| php | 17 | 5 | Deep |
| swift | 7 | 3 | Deep |
| c | 0 | 4 | Deep |
| csharp | 4 | 1 | Moderate |
| scala | 0 | 6 | Moderate |

Per-parser audit docs live at `docs/internal/parser-audit/<name>.md`. See the
[audit index](#audit-index) below.

## Audit Index

Detailed per-parser audit docs record claimed constructs, verified-by-test
constructs, edge-case coverage, verdicts, and recommended actions:

| Parser | Audit Doc | Verdict |
|--------|-----------|---------|
| c | `parser-audit/c.md` | deep |
| cloudformation | `parser-audit/cloudformation.md` | moderate |
| cpp | `parser-audit/cpp.md` | moderate |
| csharp | `parser-audit/csharp.md` | moderate |
| dart | `parser-audit/dart.md` | shallow |
| dbtsql | `parser-audit/dbtsql.md` | moderate |
| dockerfile | `parser-audit/dockerfile.md` | deep |
| elixir | `parser-audit/elixir.md` | deep |
| golang | `parser-audit/golang.md` | deep |
| gomod | `parser-audit/gomod.md` | deep |
| gradle | `parser-audit/gradle.md` | deep |
| groovy | `parser-audit/groovy.md` | moderate |
| haskell | `parser-audit/haskell.md` | moderate |
| hcl | `parser-audit/hcl.md` | deep |
| java | `parser-audit/java.md` | deep |
| javascript | `parser-audit/javascript.md` | deep |
| json | `parser-audit/json.md` | deep |
| kotlin | `parser-audit/kotlin.md` | deep |
| maven | `parser-audit/maven.md` | deep |
| nodelockfile | `parser-audit/nodelockfile.md` | deep |
| perl | `parser-audit/perl.md` | moderate |
| php | `parser-audit/php.md` | deep |
| python | `parser-audit/python.md` | deep |
| pythondep | `parser-audit/pythondep.md` | deep |
| ruby | `parser-audit/ruby.md` | moderate |
| rust | `parser-audit/rust.md` | deep |
| scala | `parser-audit/scala.md` | moderate |
| sql | `parser-audit/sql.md` | moderate |
| swift | `parser-audit/swift.md` | deep |
| yaml | `parser-audit/yaml.md` | deep |
