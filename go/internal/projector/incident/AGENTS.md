# AGENTS.md — incident projector namespace guidance

## Scope

This package is a documentation-only namespace. Runtime intent construction
belongs in a leaf such as `routing`; orchestration remains in the root
`internal/projector` package.

## Invariants

- Keep `doc.go` free of imports, declarations, initialization, and runtime work.
- Leaf packages may depend on `internal/projector/intent`, never on the root
  projector package that imports them.
- Do not move queue writes, retries, storage, graph work, or telemetry ownership
  into this namespace.
- Do not describe a leaf as independently extractable while it depends on
  repository-internal fact, intent, or reducer contracts.

## Verification

Run package-doc verification, dirgate, the affected leaf and root projector
tests, and the docs gates selected by changed paths.
