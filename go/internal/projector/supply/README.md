# Supply Projector Namespace

## Purpose

Groups source-local supply-chain evidence projector families.

## Ownership boundary

Leaf packages own intent construction. Root `projector` owns assembly,
queueing, telemetry, and graph writes.

### Move record (#6627)

No-Regression Evidence (#6627 supply nesting): base `db5c55e86`,
backend go1.27.1 darwin/arm64; new docs-only namespace parent created by this
move holds no runtime code, so there is no trigger, value, or fan-out change
to regress. Same build/vet/recursive-test record as the `impact` leaf;
B-12 replay reported 437/437 PASS in the move lane with CI required-gates as
the blocking authority, and B-7 was not run locally (Docker daemon
unreachable, socket EOF), leaving CI as the blocking authority there.
Rename-only relocation; no benchmark delta exists to measure.

No-Observability-Change (#6627 supply nesting): this package emits no
signal directly; intent volume and reducer execution stay covered by the
instruments named in the leaf section, unchanged.

## Related docs

- [Projector architecture](../README.md)
