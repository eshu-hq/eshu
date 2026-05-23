# AGENTS.md - internal/parser/php

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `calls.go`, `alias.go`, `returns.go`, and `support.go`.
3. Parent PHP parser tests and package-local tests when present.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  engine orchestration, repository path handling, and parse telemetry.
- MUST preserve PHP payload buckets for namespaces, classes, traits,
  interfaces, functions, imports, variables, calls, aliases, receiver evidence,
  and context metadata.
- MUST keep `Parse` and `PreScan` aligned through the same extraction path.
- MUST keep brace-depth scope tracking accurate for PSR-style declarations whose
  opening brace appears on a later line.
- MUST keep PHP roots bounded to entrypoints, constructors, known magic methods,
  same-file interface/trait methods, literal route evidence, Symfony route
  attributes, and WordPress hook callbacks.
- MUST NOT model Composer/autoload breadth, reflection, or dynamic dispatch as
  exact parser truth.

## Change Scope

- Add PHP evidence with a failing parent parser test first unless a child test
  already covers the package contract.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless explicitly requested.
- Do not change extension ownership, bucket names, or wire fields without
  downstream fixture/query impact review.
