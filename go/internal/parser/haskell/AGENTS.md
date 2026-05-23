# AGENTS.md - internal/parser/haskell

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`.
3. `parser_test.go` and parent Haskell parser tests when wrapper behavior
   changes.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  engine orchestration, repository path handling, and parse telemetry.
- MUST preserve modules as module rows, data/class declarations as class rows,
  and where-block locals as variables instead of top-level functions.
- MUST keep function-call rows as bounded lexical evidence from definition
  bodies and continuation lines, not compiler-resolved name binding.
- MUST limit roots to explicit module exports, `main`, typeclass methods, and
  instance methods. Implicit-export modules do not make every declaration a
  public API root.
- MUST keep `Parse` and `PreScan` aligned through the same extraction path and
  deterministic sorting.

## Change Scope

- Add Haskell behavior with a failing `parser_test.go` or parent parser test
  first.
- Keep indentation-sensitive where-block coverage when changing variables.
- Do not change extension ownership, bucket names, or root semantics without
  downstream shape and query review.
