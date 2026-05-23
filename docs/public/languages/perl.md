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

## Dead-Code Support

Perl dead-code support is `derived`. Modeled roots include script entrypoints,
package namespaces, `Exporter` declarations, constructors, special blocks,
`AUTOLOAD`, and `DESTROY`.

It is not cleanup-safe exact truth. Symbolic references, `AUTOLOAD` target
resolution, `@ISA`, Moose/Moo metadata, import side effects, runtime `eval`, and
broad public API surfaces remain blockers.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Feature Matrix](feature-matrix.md)
- [Parser Support Matrix](support-maturity.md)
