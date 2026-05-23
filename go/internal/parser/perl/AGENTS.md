# AGENTS.md - internal/parser/perl

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`.
3. `parser_test.go` and parent Perl parser tests when behavior crosses the
   parent wrapper.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch and
  repository path handling.
- MUST preserve package declarations as class rows and keep `Parse`/`PreScan`
  aligned through the same extraction path.
- MUST keep output deterministic through shared payload helpers and sorted
  pre-scan names.
- MUST mark only bounded Perl roots: public packages, Exporter declarations,
  script `main`, constructors, special blocks, `AUTOLOAD`, and `DESTROY`.
- MUST keep special blocks as derived roots, not ordinary callable subroutines.
- MUST NOT add repository-specific Perl conventions without fixture evidence.

## Change Scope

- Add Perl behavior with a failing `parser_test.go` or parent parser test first.
- Do not change extension ownership, package-row shape, bucket names, or root
  semantics without downstream shape and query review.
