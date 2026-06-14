# AGENTS.md — internal/parser/goldenaudit guidance

## Read first

1. `README.md` — package ownership, fixture rules, and exported surface
2. `doc.go` — godoc contract and no-self-comparison rule
3. `golden_audit.go` — loader, comparator, and stable report ordering
4. `go/internal/parser/README.md` — parent parser invariants
5. `docs/public/reference/local-testing.md` — verification gates

## Invariants this package enforces

- Golden expected data is independent fixture truth, not serialized observed
  output.
- Reports are deterministic. Sort new difference families before returning
  them.
- The package stays standard-library-only and does not import parser, reducer,
  storage, query, or collector packages.

## Common changes

- Adding a new fixture schema field requires loader validation, focused tests,
  and README/doc.go updates when the contract changes.
- Adding a new difference family requires a red test that proves the report
  fails when that drift appears.

## What not to change

- Do not make missing expected data pass as partial success.
- Do not add production telemetry or graph/storage dependencies here.
