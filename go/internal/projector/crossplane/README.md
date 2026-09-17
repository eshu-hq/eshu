# Crossplane Projector Namespace

## Purpose

Groups source-local Crossplane evidence projector families.

## Ownership boundary

Leaf packages own intent construction. Root `projector` owns assembly,
queueing, telemetry, and graph writes.

### Move record (#6627)

No-Regression Evidence (#6627 crossplane nesting): base `f21c0e0b`,
backend go1.27.1 darwin/arm64; namespace-only parent trio added alongside
the rename-only `crossplanesatisfiedby` to `crossplane/satisfaction` move.
Whole-module build exit 0, vet clean, recursive projector tests green. B-12
replay and B-7 golden-corpus gates were not run locally: the Docker daemon
is unreachable (socket EOF), so CI is the blocking authority there.
Rename-only, so no benchmark delta exists to measure.

No-Observability-Change (#6627 crossplane nesting): no stage added and no
metric, span, or log name changed; this namespace declares no function,
holds no state, and performs no I/O of its own.

## Related docs

- [Projector architecture](../README.md)
