# Cloud-inventory-admission projector intents

## Purpose

This package recognizes provider cloud-inventory source facts for one scope
generation and builds the reducer intent that asks the reducer to admit that
provider evidence — AWS, GCP, and Azure resource observations — into the
shared canonical CloudResource identity keyspace backing
`GET /api/v0/cloud/inventory` (#2209).

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
rejects unsupported provider schema versions before any builder runs,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainCloudInventoryAdmission` handler owns candidate classification and
every `reducer_cloud_resource_identity` row write; the projector never makes
cross-source admission decisions.

## Exported surface

- `BuildCloudInventoryAdmissionReducerIntent` builds the
  `cloud_inventory_admission` intent, anchored to the earliest `aws_resource`,
  `gcp_cloud_resource`, or `azure_cloud_resource` fact in the generation's
  original input order.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the three
provider fact-kind constants and `internal/reducer` for the domain constant.
There is no decode seam: like `packagesource`, this builder reads only
`envelope.FactKind` and never a payload field.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_cloud_inventory_admissions_total`. Moving
the pure builder adds no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- A provider cloud-inventory source fact is required: `aws_resource`,
  `gcp_cloud_resource`, or `azure_cloud_resource`. The
  `cloudInventoryAdmissionSourceFactKinds` set stays in lockstep with the
  fact-kind allowlist the reducer's admission evidence loader reads.
- No candidate kind outranks another: the anchor is the earliest source fact
  in original input order across the three kinds, via
  `FirstMatchingKindPredicate`. Introducing a per-kind priority would change
  `FactID` for any generation that carries more than one provider kind and
  destabilize the reducer claim.
- The `Reason` is always `provider cloud-inventory source facts observed` and
  the `EntityKey` is `cloud_inventory_admission:<scope>`, one intent per
  scope generation regardless of how many source facts it carries. The root
  fan-out parity fixture pins both; the reducer reloads the generation's
  facts itself.
- `SourceSystem` is the shared `projectorintent.SourceSystem` two-tier
  fallback (`SourceRef.SourceSystem`, then `CollectorKind`, each trimmed).
  The root file's private helper was a pure delegation to the shared helper —
  its whole body was that call — so this family, unlike `servicecatalog`,
  whose helper carries a third scope-level tier, keeps no local copy.
- The payload is never decoded here, and the schema version is not checked
  here either. Root projection's central schema-version gate rejects an
  unsupported provider `schema_version` before this builder runs, pinned at
  root by `TestProjectEnforcesCentralSchemaVersionForPreviouslyUngatedFamily`.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

No-Regression Evidence: this extraction moves one builder without changing
the trigger, value, or fan-out position. The reducer intent domain, entity
key, reason string, and input-order anchor selection across the three
provider kinds are identical to the base commit, and the dispatcher's ordered
fan-out is unchanged at 44 builder probes with this probe still running
immediately after `azure.BuildRelationshipMaterializationReducerIntent` and
before `workloadcloud.BuildWorkloadCloudRelationshipMaterializationReducerIntent`.
The private `cloudInventoryAdmissionSourceSystem` helper the root file owned
was a pure delegation to `projectorintent.SourceSystem` — its entire body was
`return projectorintent.SourceSystem(envelope)` — so inlining the shared call
is behavior-identical by construction and the focused test pins the
`CollectorKind` fallback; the root `firstMatchingKindPredicate` forwarder was
a direct delegate to `projectorintent.FactLookup.FirstMatchingKindPredicate`,
so that substitution is behavior-identical the same way, and the forwarder
stays at root for the observability-coverage correlation probe that still
calls it. Focused proof, run from the `go/` module root:
`../scripts/go-test-run-guard.sh 1 'TestBuildCloudInventoryAdmissionReducerIntent' -- ./internal/projector/cloudinventory -count=1`
and `go test ./internal/projector/... -count=1` green, whole-module `go build`
and `go vet` clean.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)
