# AGENTS.md — CI/CD run projector namespace guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for the parent CI/CD namespace.
3. `../../AGENTS.md` and `../../README.md` for projector-wide invariants.
4. A child package's `AGENTS.md` before changing that family.

## Ownership

This is a documentation-only namespace. It must not introduce imports, shared
state, initialization, dispatch, queue behavior, storage, or telemetry. Child
packages own only their documented trigger-selection and intent-building
contracts; root projector and reducer ownership stays unchanged.

## Verification

Run package-doc verification, dirgate, affected child and root projector tests,
and the golden-corpus gates selected by the changed paths.
