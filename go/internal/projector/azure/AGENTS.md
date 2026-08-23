# AGENTS.md — Azure projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- Preserve the shared Azure acceptance-unit key between resource and
  relationship intents.
- Select only the exact Azure fact kind and preserve the lookup's earliest-fact
  anchor semantics.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.
- Keep source-system selection bounded to source ref with collector fallback.

## Verification

Use TDD. Run focused Azure package and root assembly tests, ordered fan-out
parity, package-doc verification, the full projector package tree, and the
golden-corpus gates selected by the changed paths.
