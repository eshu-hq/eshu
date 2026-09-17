# Access Projector Namespace

## Purpose

Groups source-local access-posture projector families.

## Ownership boundary

Leaf packages own intent construction. Root `projector` owns assembly,
queueing, telemetry, and graph writes.

### Move record (#6627)

No-Regression Evidence (#6627 secretsiam nesting): base `cc3f77c61`,
backend go1.27.1 darwin/arm64; namespace-only parent trio added alongside
the rename-only `secretsiam` to `access/posture` move. Whole-module build
exit 0, vet clean, recursive projector tests green, moved tests green in the
new path via the test-run guard, and the repoint listing is non-empty (1
test). B-12 replay gate passes locally (437/437); B-7 golden-corpus was not
run locally: the Docker daemon is unreachable (socket EOF), so CI is the
blocking authority there. Rename-only, so no benchmark delta exists to
measure.

No-Observability-Change (#6627 secretsiam nesting): no stage added and no
metric, span, or log name changed; this namespace declares no function,
holds no state, and performs no I/O of its own.

## Related docs

- [Projector architecture](../README.md)
