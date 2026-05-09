# Parser Language Layout Plan

Issue: #77

## Goal

Split `go/internal/parser` into language-owned areas without changing parser
behavior. The current package has more than 200 files. That makes reviews slow,
puts unrelated language work in the same directory, and makes subagent work
riskier than it should be.

This PR should make ownership obvious first. New dead-code logic, new parser
features, and new language maturity work can follow once the layout is easier
to reason about.

## Ground Rules

- Keep parser output unchanged unless a test exposes a real bug.
- Move tests with the parser behavior they protect.
- Keep shared helpers shared only when more than one language family uses them.
- Keep registry wiring explicit; avoid clever dynamic registration.
- Preserve the existing `go/internal/parser` import path until a smaller,
  evidence-backed package split is ready.
- Run `go test ./internal/parser -count=1` after every meaningful move.

## Proposed Layout

Go treats every directory as a separate package. The first implementation pass
therefore must not create child folders that pretend to share the parent
`parser` package. Start with narrow language-owned subpackages only when their
dependency direction is clean:

```text
go/internal/parser/
  java/
  javascript/
  python/
  shared/
  sql/
  go/
  yaml/
  hcl/
  php/
  kotlin/
  longtail/
```

The first pass is intentionally boring, but it must compile as real Go. Move
leaf helpers into language packages first, keep `engine.go`, `registry.go`, and
payload assembly in `internal/parser`, and widen exported helper APIs only for
actual parent-package consumers. Large language adapters can move later after
their tree-sitter helpers and shared payload helpers have explicit package
contracts.

## Work Slices

1. Inventory the current files by language family and shared helper ownership.
2. Add folder docs for each target parser subpackage before moving code into
   it.
3. Move leaf helpers first, starting with Java metadata extraction because it
   can return typed class-reference evidence without importing the parent
   parser package.
4. Move JavaScript/TypeScript, Python, Java, and Go adapters only after their
   shared tree-sitter and payload dependencies are explicit.
5. Move SQL, YAML/HCL/IaC, PHP, Kotlin, and long-tail languages after the main
   families are stable.
6. Keep `registry.go`, `engine.go`, SCIP support, and truly shared helpers in
   the top-level or `shared` area until a package-boundary decision exists.
7. Run parser tests after each slice and the collector parser gate before the
   PR leaves draft.

## Acceptance

- The parser package still builds and tests with `go test ./internal/parser
  -count=1`.
- The collector parser gate passes:

  ```bash
  go test ./internal/parser ./internal/collector/discovery ./internal/collector -count=1
  ```

- Each new parser folder has a short `README.md` explaining what belongs there.
- No dead-code maturity claim changes in this PR unless backed by a failing
  test and a focused fix.
- The PR description lists what moved, what stayed shared, and any follow-up
  package-boundary work.
