# AGENTS.md - internal/parser/dart

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`.
3. `parser_test.go` and parent Dart parser tests when wrapper behavior changes.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  engine orchestration, repository path handling, and parse telemetry.
- MUST preserve legacy Dart payload shape, deterministic bucket order, and
  sorted `PreScan` names from the same extraction path as `Parse`.
- MUST keep roots syntax-local: top-level `main`, constructors, `@override`,
  Flutter `build`/`createState`, and public `lib/` declarations outside
  `lib/src/`.
- MUST consume class annotations at the declaration boundary so they do not
  leak into member decorators.
- MUST keep constructor detection at class-member depth; constructor calls in
  method bodies remain call evidence.

## Change Scope

- Add Dart behavior with a failing `parser_test.go` or parent parser test first.
- Keep file reading through `internal/parser/shared.ReadSource`.
- Do not change extension ownership, bucket names, or root semantics without
  downstream shape and query review.
