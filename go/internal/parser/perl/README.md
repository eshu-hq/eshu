# Perl Parser

## Purpose

This package owns the tree-sitter-backed Perl parser adapter used by the parent
parser engine. It extracts package declarations, use imports, subroutines,
variables, simple call evidence, bounded dead-code root metadata, and exact
Perl web route entries for narrow literal DSL forms.

## Ownership boundary

The package is responsible for Perl syntax-tree parsing and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, and parse telemetry. External `perl_test`
files exercise that public Engine boundary without joining the production
dependency graph.

## Exported surface

The godoc contract is in [doc.go](doc.go). Current exports are Parse,
ParseWithParser, and PreScan.

## Dependencies

This package imports the Go standard library, the static Perl tree-sitter
binding, go-tree-sitter, and internal/parser/shared. Production package `perl`
and same-package tests must not import the parent `internal/parser` package.
External `perl_test` files may import it only to verify public Engine behavior.
Those tests use `internal/parser/parsertest` for shared fixtures and payload
assertions.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Package names are emitted as class rows using the final `::` segment to
preserve the legacy payload. Public packages carry `perl.package_namespace`.
Subroutines carry root metadata for script `main`, Exporter `@EXPORT` /
`@EXPORT_OK` declarations, package `new`, Perl special blocks, `AUTOLOAD`, and
`DESTROY`. Function calls are deduplicated by name, preserving the legacy
payload shape while deriving calls from syntax-tree call nodes. Function source
spans cover the full subroutine node when source indexing is enabled. PreScan
sorts names after collecting them from the parsed function and class buckets.
Route entries are exact-only: one active Mojolicious::Lite or Dancer/Dancer2
import family per file, literal paths, concrete HTTP verbs, and a named code
reference handler. Catalyst dispatcher conventions, Mojolicious controller
strings, Dancer `any`, inline handlers, generated route tables, and dynamic
symbol lookup stay outside this package boundary. Keep adapter-internal tests
in package `perl`; keep parent Engine dispatch and payload checks in external
package `perl_test` so the production import direction remains explicit.

## Related docs

- docs/public/languages/support-maturity.md
