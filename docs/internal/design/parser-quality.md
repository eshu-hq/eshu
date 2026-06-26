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

## Parent-Level Test Convention

The parser package places engine-level integration tests at the **package root**
(`go/internal/parser/`), not inside each language subdirectory. This is by
design, not a coverage gap:

1. **Engine tests** (`engine_<lang>_*_test.go`) exercise the full parse path
   through parser discovery, initialisation, and result verification. They live
   at the package root because they import and exercise the shared engine
   dispatch, not a single language adapter.

2. **Language subdirectory tests** (`go/internal/parser/<lang>/*_test.go`) cover
   package-internal helpers, edge cases, and unit-level parsing logic. Not every
   language needs subdirectory tests when the parent engine tests provide
   sufficient coverage.

3. **Dead-code root tests** (`<lang>_dead_code_roots_test.go`) live at the
   package root and verify framework-specific entry-point detection per language.

This convention means that a language parser can have **zero subdirectory test
files** and still be thoroughly tested at the engine level. Subdirectory test
counts are not a coverage signal.

## Corrected Baseline

The original P1 framing ("c, kotlin, php, scala, swift — 49 src files with zero
test coverage") counted only `_test.go` files inside each language subdirectory.
Parent-level tests prove the opposite:

| Parser | Parent-level test files | Subdirectory tests | Verdict |
|--------|------------------------|-------------------|---------|
| kotlin | 15 (`engine_kotlin_*`, `kotlin_dead_code_roots`) | 0 | Deep |
| php | 15 (`php_language_*`, `php_dead_code_roots`) | 0 | Deep |
| swift | 4 (`engine_swift_*`, `swift_dead_code_roots`) | 0 | Deep |
| c | 1 (`c_dead_code_roots`) | 0 | Deep |
| csharp | 2 | 0 | Moderate |
| scala | 1 (`scala_dead_code_roots`) + 3 incidental | 0 | Moderate |

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
