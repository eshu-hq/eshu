# Perl Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `perl`
- Family: `language`
- Parser: `DefaultEngine (perl)`
- Entrypoint: `go/internal/parser/perl_haskell_language.go`
- Fixture repo: `tests/fixtures/ecosystems/perl_comprehensive/`
- Unit test suite: `go/internal/parser/engine_long_tail_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Subroutines | `subroutines` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlFixtures` | Compose-backed fixture verification | - |
| Packages | `packages` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Use statements | `use-statements` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlFixtures` | Compose-backed fixture verification | - |
| Method calls | `method-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Ambiguous function calls | `ambiguous-function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Plain function calls | `plain-function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Scalar variables (`my $x`) | `scalar-variables-my-x` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Array variables (`my @x`) | `array-variables-my-x` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |
| Hash variables (`my %x`) | `hash-variables-my-x` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathPerlCallsAndVariables` | Compose-backed fixture verification | - |

## Dead-Code Support

Maturity: `derived`.

Perl parser metadata currently marks these cleanup-protecting roots:

- `perl.script_entrypoint` for `sub main` in `.pl` and `.t` files
- `perl.package_namespace` for public package declarations
- `perl.exported_subroutine` for functions listed in `@EXPORT` or `@EXPORT_OK`
  through bounded `qw(...)` Exporter declarations
- `perl.constructor` for `sub new` inside a package
- `perl.special_block` for `BEGIN`, `UNITCHECK`, `CHECK`, `INIT`, and `END`
- `perl.autoload_subroutine` for `AUTOLOAD`
- `perl.destroy_subroutine` for `DESTROY`

Checked fixtures:

- Parser metadata coverage:
  `go/internal/parser/perl/parser_test.go::TestParseMarksPerlDeadCodeRoots`
- Query suppression coverage:
  `go/internal/query/code_dead_code_perl_roots_test.go`
- Dead-code fixture intent:
  `tests/fixtures/deadcode/perl/`

Perl remains non-exact until symbolic reference dispatch, AUTOLOAD target
resolution, `@ISA` inheritance, Moose/Moo metadata, import side effects,
runtime `eval`, and broad public API surfaces are modeled or scoped out.

Real-repo dogfood for Issue #103 used isolated Compose projects:

- `mojolicious/mojo` at `ef9a681c0d2d235e9cc3bbd855f14ae32bd5574f`,
  `401` checked-out files and `274` `.pl` / `.pm` / `.t` files. Discovery
  indexed `187` files, parsed `185`, materialized `3,582` content entities,
  emitted `4,122` facts, and completed bootstrap in `5.54s` wall time. The
  scoped dead-code API returned `truth.level=derived`,
  `dead_code_language_maturity.perl=derived`, `25` results, and all seven
  modeled Perl root kinds.
- `metacpan/metacpan-web` at
  `d0e81c3cd33d490ce3b14ed53004cb7c267c0e8b`, `659` checked-out files and
  `121` `.pl` / `.pm` / `.t` files. Discovery indexed and parsed `115` files,
  materialized `1,318` content entities, emitted `1,556` facts, and completed
  bootstrap in `5.09s` wall time. The final shared projection queue had
  `pending=0`. The scoped dead-code API returned `truth.level=derived`,
  `dead_code_language_maturity.perl=derived`, `25` results, and all seven
  modeled Perl root kinds.
- `Perl/perl5` at `16357f7bef073667e5bd2316408a6a004055174b`, `6,850`
  checked-out files and `4,321` `.pl` / `.pm` / `.t` files. Collection parsed
  `1,549` files and materialized `67,927` entities, but Neo4j canonical
  projection failed on the default Compose transaction memory cap. That is
  recorded as backend-capacity evidence, not Perl parser failure evidence.

## Known Limitations
- Anonymous subroutines assigned to variables are not captured as named functions
- `AUTOLOAD` dispatch targets are not resolved
- Special blocks are emitted as derived roots, not ordinary callable
  subroutines
