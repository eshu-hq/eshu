# Kotlin Parser Agent Notes

## Read First

Read parser.go first (the thin entry point), then ast_walk.go (the recursive
walker and scope frame), ast_declarations.go, ast_functions.go,
ast_variables.go, and ast_calls.go for AST extraction. Read
receiver_inference.go, type_reference.go, dead_code_roots.go,
repository_returns.go, helpers.go, scope_function_helpers.go, and prescan.go for
the supporting pure helpers. Keep changes scoped to Kotlin unless the caller
explicitly asks for a cross-language parser contract change.

## Invariants

Do not import the parent parser package. Use go/internal/parser/shared for
`shared.Options`, source reads, base payload construction, bucket appends,
sorting, and pre-scan name cleanup.

Extraction is AST-only. Do not reintroduce `regexp` or `strings.Split(src,
"\n")` line-scan symbol extraction. Confirm grammar node kinds with a compiled
grammar probe or the Go test harness, not filtered text search. The vendored
grammar is `github.com/tree-sitter-grammars/tree-sitter-kotlin v1.1.0`.

`Parse` must preserve the parent engine behavior and payload shape: the
`map[string]any` keys and value shapes are the contract proven by the
`engine_kotlin_*` tests, `kotlin_dead_code_roots_test.go`, the
`kotlin_comprehensive` golden fixture path, and the reducer Kotlin
code-call tests. `PreScan` must keep deriving names from the same `Parse` path
so collection pre-scan and full parsing agree.

## Common Changes

Declaration and call extraction belongs in the `ast_*.go` files keyed by node
kind. Receiver, return-type, and chain inference belongs in
receiver_inference.go or type_reference.go. Dead-code root classification
belongs in dead_code_roots.go. Package-neighbor return lookup belongs in
repository_returns.go. Smart-cast and when-subject narrowing live in
ast_variables.go and flow through the recursion `frame`, not a brace-depth
stack.

## Failure Modes

Missing imports show up as changed imports or absent function-call rows.
Over-broad return lookup can make unrelated sibling packages influence receiver
inference. Scope bugs usually change `class_context`, `inferred_obj_type`,
`dead_code_root_kinds`, `call_kind`, or duplicate call rows. A new grammar
construct that the walker does not handle silently drops a row; add a focused
test and probe the AST before editing.

## Anti-Patterns

Do not add parent-package imports, regex or line-scan extraction,
whole-repository scans, hidden fallbacks for ambiguous return types, or Kotlin
fixes in other language packages. Do not change payload keys without focused
Kotlin tests and downstream parser contract validation.

## Evidence notes

No-Regression Evidence (issue #3533, Kotlin AST migration): `go test
./internal/parser -run Kotlin -count=1`, `go test ./internal/reducer -run
Kotlin -count=1`, and `go test ./internal/parser/goldenaudit -count=1` failed
on no assertion and continue to pass after the regex/line-scan parser
(`patterns.go`, `scope.go`, `smart_cast.go`, `cast_receiver_calls.go`, and the
line-scan loop in the old `parser.go`/`tree_sitter_syntax.go`) was replaced by
an AST walk. The payload keys and value shapes are byte-identical to the prior
hybrid parser, so no downstream fact, shape, or reducer contract changed.

No-Observability-Change (issue #3533): the migration is parser-local. It adds no
metric instrument, metric label, span, log line, status field, env var, queue,
worker, lease, batch, runtime knob, or graph query. Operators still diagnose
Kotlin parsing through the collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.
