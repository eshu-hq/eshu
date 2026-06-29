# Perl Parser

This page describes the current Go parser and query contract for Perl.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `perl` |
| Parser | `DefaultEngine (perl)` |
| Entrypoint | `go/internal/parser/perl_haskell_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/perl_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_long_tail_test.go`, `go/internal/parser/perl/parser_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Subroutines, packages, imports, method calls, plain or ambiguous calls, and scalar/array/hash variables. |
| Graph surface | Parsed functions, classes, imports, calls, and variables use the shared graph/content entity model. |
| Dead-code roots | Parser metadata marks known live Perl entrypoints and package surfaces as roots. |
| Exact web route entries | Literal Mojolicious::Lite and Dancer/Dancer2 verb routes with named code-reference handlers emit exact route entries. |

## Dead-Code Support

Perl dead-code support is `derived`. Modeled roots include script entrypoints,
package namespaces, `Exporter` declarations, constructors, special blocks,
`AUTOLOAD`, and `DESTROY`.

It is not cleanup-safe exact truth. Symbolic references, `AUTOLOAD` target
resolution, `@ISA`, Moose/Moo metadata, import side effects, runtime `eval`, and
broad public API surfaces remain blockers.

## Framework And Library Support

Supported today:

- Literal Mojolicious::Lite and Dancer/Dancer2 route declarations emit
  `framework_semantics.{mojolicious,dancer}.route_entries` when the file has one
  active DSL owner, the HTTP verb and route path are literal, and the handler is
  a named code reference such as `\&health`.
- Exporter declarations, package namespaces, constructors, special blocks,
  `AUTOLOAD`, `DESTROY`, and script entrypoints are modeled as derived roots.
- `HANDLES_ROUTE` is projected only when the reducer resolves the emitted
  handler name to exactly one Function entity.

Not claimed today:

- Moose/Moo metadata, symbolic references, `AUTOLOAD` target resolution,
  import side effects, runtime `eval`, and broad public API surfaces remain
  exactness blockers.
- Catalyst dispatcher conventions, Mojolicious `controller#action` strings,
  Dancer `any`, inline subroutines, generated route tables, dynamic paths,
  dynamic methods, wrappers, files importing both route DSL families, and other
  runtime framework conventions remain unsupported or partial rather than
  guessed.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Feature Matrix](feature-matrix.md)
- [Parser Support Matrix](support-maturity.md)
