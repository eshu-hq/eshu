# AGENTS.md — incident-routing projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildIncidentRoutingMaterializationReducerIntent` triggers on
  `incident.record` plus every kind `facts.IncidentRoutingFactKinds()` returns.
  Add a new routing source kind in `internal/facts`, not here; the builder
  must keep reading the registry so the trigger set cannot drift from the
  collector contract.
- The anchor is the earliest candidate fact in original input order across
  all kinds (`FirstAcrossKinds`), not the earliest fact of the first-listed
  kind. Do not replace it with a per-kind `FirstOfKind` loop; that changes
  `FactID` and the reducer claim.
- The entity key is `incident_routing_materialization:<scope>` — one intent
  per scope generation, family-distinct from the AWS
  `aws_resource_materialization` key.
- Do not decode the payload, compare declared/applied/live routing, or infer
  service truth here. The reducer owns `IncidentRoutingEvidence` and its
  `HAS_INTENDED_ROUTING` edges.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root incident-routing
`buildProjection` tests, ordered fan-out parity, package-doc verification, the
projector package tree, and the golden-corpus gates selected by the changed
paths.
