# AGENTS.md — CI/CD projector namespace guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide ownership and fan-out invariants.
3. `run/AGENTS.md` before changing the run namespace or its children.

## Ownership

This is a documentation-only namespace. Root projector assembly owns lookup
construction, ordered dispatch, queue writes, retries, and telemetry. Leaf
packages may select immutable evidence and build values for reducer-owned
correlation or materialization work; they do not own lifecycle or storage.

Keep leaf packages independent of the root `projector` package. They may import
the neutral `internal/projector/intent` contract instead.

## Verification

Run package-doc verification, dirgate, affected leaf and root projector tests,
and the golden-corpus gates selected by the changed paths.
