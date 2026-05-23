# AGENTS.md - internal/parser/ruby

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `calls.go`, and `dead_code_roots.go`.
3. `parser_test.go` and parent Ruby parser tests.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  engine orchestration, repository path handling, and parse telemetry.
- MUST preserve legacy Ruby payload buckets, context metadata, `end_line`
  metadata, method suffixes, and deterministic pre-scan output.
- MUST keep constants in the legacy `variables` bucket unless downstream shape
  work explicitly introduces a constants bucket.
- MUST keep dead-code roots source-backed and bounded to visible Ruby evidence:
  Rails controller actions, literal callbacks, `method_missing`,
  `respond_to_missing?`, literal dynamic-send symbol targets, and script guards.
- MUST keep unmodeled Rails/Rake DSL chains as bounded call evidence until
  tests and dogfood proof justify a root model.

## Change Scope

- Add Ruby behavior with a failing `parser_test.go` or parent parser test first.
- Keep registry ownership, Engine dispatch, and content-shape work outside this
  package unless the task explicitly includes those surfaces.
- Do not change Ruby extension ownership, bucket names, or root semantics
  without downstream fixture/query impact review.
