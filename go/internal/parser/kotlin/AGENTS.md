# Kotlin Parser Agent Notes

## Read First

Read parser.go first, then dead_code_roots.go, patterns.go, helpers.go,
repository_returns.go, smart_cast.go, type_reference.go,
receiver_inference.go, cast_receiver_calls.go, scope_function_helpers.go,
prescan.go, and scope.go. Keep changes scoped to Kotlin unless the caller
explicitly asks for a cross-language parser contract change.

## Invariants

Do not import the parent parser package. Use go/internal/parser/shared for
`shared.Options`, source reads, base payload construction, bucket appends,
sorting, and pre-scan name cleanup.

`Parse` must preserve the parent engine behavior and payload shape from
parser.go:12 through parser.go:484. `PreScan` must continue to derive names from
the shared patterns in patterns.go so collection pre-scan and full parsing
agree.

## Common Changes

Kotlin declaration and call extraction belongs in parser.go. Receiver, return
type, and chain inference helpers belong in receiver_inference.go, helpers.go,
or type_reference.go. Dead-code root classification belongs in
dead_code_roots.go. Package-neighbor return lookup belongs in
repository_returns.go. Smart-cast and when-subject behavior belongs in
smart_cast.go.

## Failure Modes

Missing imports usually show up as changed imports or absent function call
rows. Over-broad return lookup can make unrelated sibling packages influence
receiver inference. Scope bugs usually change `class_context`,
`inferred_obj_type`, `dead_code_root_kinds`, or duplicate call rows.

## Anti-Patterns

Do not add parent-package imports, whole-repository scans, hidden fallbacks for
ambiguous return types, or Kotlin fixes in other language packages. Do not
change payload keys without focused Kotlin tests and downstream parser contract
validation.
