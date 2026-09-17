# Semantic Projector Namespace

## Purpose

Groups source-local semantic-entity projector families.

## Ownership boundary

Leaf packages own intent construction. Root `projector` owns assembly,
queueing, telemetry, and graph writes.

### Move record (#6627)

No-Regression Evidence (#6627 semanticentity nesting): base `1e046489d`,
backend go1.27.1 darwin/arm64; namespace-only parent trio added alongside
the rename-only `semanticentity` to `semantic/entity` move. Whole-module build
exit 0, vet clean, recursive projector tests green, moved tests green in the
new path via the test-run guard, and the repoint listing is non-empty.
B-12 replay gate passes locally; B-7 golden-corpus was not run locally: the
Docker daemon is unreachable, so CI is the blocking authority there.
Rename-only, so no benchmark delta exists to measure.

No-Observability-Change (#6627 semanticentity nesting): no stage added and no
metric, span, or log name changed; this namespace declares no function,
holds no state, and performs no I/O of its own.

## Related docs

- [Projector architecture](../README.md)
