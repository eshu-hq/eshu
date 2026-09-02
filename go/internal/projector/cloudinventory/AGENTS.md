# AGENTS.md — cloud-inventory-admission projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that the projector never makes cross-source admission decisions.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the Azure relationship-materialization probe and before the
   workload-cloud-relationship probe.
5. `go/internal/reducer/cloud_inventory_admission.go` for what the reducer
   does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildCloudInventoryAdmissionReducerIntent` triggers only on a provider
  cloud-inventory source fact — `aws_resource`, `gcp_cloud_resource`, or
  `azure_cloud_resource` — and anchors to the earliest such fact in original
  input order across the three kinds, via `FirstMatchingKindPredicate`. Do not
  introduce a per-kind priority (it changes `FactID` for generations that
  carry more than one provider kind and the root fan-out parity fixture pins
  the current anchor).
- `cloudInventoryAdmissionSourceFactKinds` stays in lockstep with the
  fact-kind allowlist the reducer's admission evidence loader reads. Adding a
  provider kind here without the loader-side allowlist entry enqueues intents
  the handler cannot admit.
- Keep the `provider cloud-inventory source facts observed` reason and the
  `cloud_inventory_admission:<scope>` entity key byte-identical. The reducer
  claims one intent per scope generation and reloads the generation's facts
  itself.
- `SourceSystem` is the shared `projectorintent.SourceSystem` two-tier
  fallback. Do not add a third tier here without a contract decision — the
  root file's private helper was a pure delegation to the shared one, which is
  why this family carries no local helper.
- Do not decode a payload or check a schema version here. This builder reads
  only `envelope.FactKind`; root projection already rejected unsupported
  provider schema versions.
- Do not classify candidates, write identity rows, or serve the inventory
  readback here. The reducer's `DomainCloudInventoryAdmission` handler owns
  all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
