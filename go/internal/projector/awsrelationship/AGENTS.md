# AGENTS.md — AWS relationship projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.
5. `docs/internal/aws-relationship-edge-materialization-design.md` for the
   node-before-edge readiness design this intent participates in.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildAWSRelationshipMaterializationReducerIntent` triggers on the mere
  presence of an `aws_relationship` fact and anchors to the earliest one in
  original input order (`FirstOfKind`). Do not add a payload predicate or a
  cross-kind scan here; either changes `FactID` and the reducer claim.
- The entity key is `aws_resource_materialization:<scope>` on purpose. It is
  NOT a family-distinct key: the reducer's edge handler resolves the
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish under exactly this key, so edges never project before
  nodes commit. Renaming the key silently removes that gate.
- Do not decode the payload, resolve ARNs, or pick edge labels here. The
  reducer's `DomainAWSRelationshipMaterialization` handler owns the bounded
  join, the readiness check, and the `cloud_resource_edge_writer` call.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
