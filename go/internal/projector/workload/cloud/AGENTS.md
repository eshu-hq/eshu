# AGENTS.md — workload-cloud projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` and `../../README.md` for projector-wide invariants.
3. `../../intent/AGENTS.md` for the neutral builder contract.
4. `../../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildReducerIntent` must keep the
  shared `aws_resource_materialization:<scope>` entity key — do not give it a
  family-distinct key; the durable claim gate matches that prefix directly.
- The builder anchors on the earliest matching `aws_resource` fact so the
  reducer claim stays stable across reprojections.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.
- Do not add a local `factschema_decode_aws.go` seam here: the builder
  triggers on fact presence only and never decodes the payload.

## Verification

Use TDD. Run the focused child test, root ordered fan-out parity and probe-count
tests, package-doc verification, the projector package tree, and the
golden-corpus gates selected by the changed paths.
