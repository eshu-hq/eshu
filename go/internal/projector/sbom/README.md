# SBOM Projector Namespace

## Purpose

Groups source-local SBOM evidence projector families.

## Ownership boundary

Leaf packages own intent construction. Root `projector` owns assembly,
queueing, telemetry, and graph writes.

### Move record (#6627)

No-Regression Evidence (#6627 sbomattestation nesting): base `f316c0593`,
backend go1.27.1 darwin/arm64; namespace-only parent trio added alongside
the rename-only `sbomattestation` to `sbom/attestation` move. Scoped build
(`internal/projector/...`, `internal/coordinator/...`,
`cmd/workflow-coordinator/...`) exit 0, vet clean on the touched packages,
recursive projector and coordinator tests green, moved tests green in the
new paths, moved-file-refs gate clean (8 vacated paths, no dangling
references), doc-citations clean, and filename-stutter clean. B-12 replay
and B-7 golden-corpus gates were not run locally, so CI is the blocking
authority there. Rename-only, so no benchmark delta exists to measure.

No-Observability-Change (#6627 sbomattestation nesting): no stage added and no
metric, span, or log name changed; this namespace declares no function,
holds no state, and performs no I/O of its own.

## Related docs

- [Projector architecture](../README.md)
