# AGENTS.md - internal/parser/php guidance

## Read first

1. README.md - package boundary and PHP context invariants
2. doc.go - godoc contract for the PHP adapter
3. parser.go - declaration, variable, import, and payload behavior
4. calls.go - call row, receiver, and deduplication helpers
5. alias.go - receiver and return-type inference helpers
6. returns.go - return signature and chained reference helpers
7. support.go - import, base, parameter, source, context, and argument helpers

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the legacy PHP payload shape, including traits, interfaces,
  namespace, context metadata, aliases, and receiver inference.
- PreScan derives names from Parse so parent pre-scan and full parse agree.
- Brace-depth scope handling in parser.go:86 keeps variables and calls attached
  to the same context rows that parent PHP tests assert.

## Common changes and how to scope them

- Add PHP evidence by writing a focused parent parser test first unless a child
  package contract test already covers the behavior.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets, sorting, source reads,
  and common name normalization.

## Failure modes and how to debug

- Missing class_context metadata usually means parser.go scope push/pop behavior
  changed.
- Missing inferred_obj_type values usually mean alias.go receiver inference or
  returns.go chained reference resolution changed.
- Missing call rows usually mean calls.go filtered a chained, static,
  null-safe, or constructor call shape that parent parser tests rely on.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Treating PHP parsing as full syntax analysis without fixture proof.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change PHP extension ownership or registry behavior from this package.
