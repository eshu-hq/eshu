# AGENTS.md - internal/parser/kotlin

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `patterns.go`, `prescan.go`, `scope.go`, and
   `dead_code_roots.go`.
3. `receiver_inference.go`, `repository_returns.go`, `smart_cast.go`,
   `type_reference.go`, `cast_receiver_calls.go`, and
   `scope_function_helpers.go`.
4. Parent Kotlin parser tests.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  path handling, and Engine signatures.
- MUST preserve Kotlin payload keys, row fields, receiver metadata,
  `class_context`, `dead_code_root_kinds`, and deterministic bucket ordering.
- MUST keep `Parse` and `PreScan` aligned through shared patterns and sorted
  names.
- MUST keep return lookup repository-bounded and package-aware. Do not let
  unrelated sibling packages influence receiver inference.
- MUST keep roots bounded to parser-backed Kotlin entrypoints, interfaces,
  overrides, Gradle, Spring, lifecycle, and JUnit callback evidence.
- MUST NOT add hidden fallbacks for ambiguous return types or whole-repository
  scans.

## Change Scope

- Add Kotlin behavior with a failing parent parser test first.
- Keep declaration/call extraction in `parser.go`, receiver/return inference in
  the inference helpers, and root classification in `dead_code_roots.go`.
- Do not change payload keys or cross-language parser behavior without
  downstream parser contract validation.
