# Perl Parser

## Purpose

This package owns the tree-sitter-backed Perl parser adapter used by the parent
parser engine. It extracts package declarations, use imports, subroutines,
variables, simple call evidence, and bounded dead-code root metadata.

## Ownership boundary

The package is responsible for Perl syntax-tree parsing and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, and parse telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse and PreScan.

## Dependencies

This package imports the Go standard library, the static Perl tree-sitter
binding, go-tree-sitter, and internal/parser/shared. It must not import the
parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Performance and observability evidence

No-Regression Evidence: baseline `origin/main` at
`bf7cb1216a677e94967cd429fca324c710cb8841` used the line/regex Perl adapter
and had no registered Perl tree-sitter runtime parser or Perl package import
resolver. After this change, Go 1.26.4 on darwin/arm64 with
`github.com/alexaandru/go-sitter-forest/perl v1.9.9` and
`github.com/tree-sitter/go-tree-sitter` parses the same legacy payload buckets
from syntax-tree nodes. The focused proof is
`go test ./internal/parser ./internal/parser/perl ./internal/reducer ./internal/resolutionparity -count=1`.
The input shape includes a two-file Perl repository fixture with
`App::Worker` importing `App::Util` and calling `App::Util::execute`, plus an
ambiguous duplicate package fixture. Terminal reducer output is one
`import_binding` code-call row for the imported callee, zero rows for the
decoy package, and zero rows for ambiguous duplicate package targets. This
change adds no queue workers, batches, leases, graph writes, Cypher, or storage
backend operations; it only enriches parser facts and in-memory resolver
selection before the existing repository fallback.

No-Observability-Change: this package still emits no telemetry directly.
Operators continue to diagnose parser throughput and failures through the
parent parser engine timing/error path, and code-call materialization continues
to use the existing reducer fact-load, extraction, shared-intent, and projection
status/log surfaces. No new runtime knob, queue state, metric cardinality, span,
or structured log field is introduced by the Perl parser or resolver slice.

## Gotchas / invariants

Package names are emitted as class rows using the final `::` segment to
preserve the legacy payload. Public packages carry `perl.package_namespace`.
Subroutines carry root metadata for script `main`, Exporter `@EXPORT` /
`@EXPORT_OK` declarations, package `new`, Perl special blocks, `AUTOLOAD`, and
`DESTROY`. Function calls are deduplicated by name, preserving the legacy
payload shape while deriving calls from syntax-tree call nodes. Function source
spans cover the full subroutine node when source indexing is enabled. PreScan
sorts names after collecting them from the parsed function and class buckets.

## Related docs

- docs/public/languages/support-maturity.md
