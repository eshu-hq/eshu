# AGENTS.md — AWS resource-materialization projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs immediately after `multicloudruntimedrift.BuildMultiCloudRuntimeDriftReducerIntent`
   and immediately before `gcp.BuildResourceMaterializationReducerIntent`.
5. `go/internal/reducer/aws_resource_materialization.go` for what the reducer
   does with the intent this package enqueues: payload decode,
   `CloudResourceUID` identity derivation, and the canonical `CloudResource`
   node write.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildAWSResourceMaterializationReducerIntent` triggers on the mere presence
  of an `aws_resource` fact and anchors to the earliest one in original input
  order (`FirstOfKind`). It does not inspect the payload, and it does not gate
  on any relationship, posture, or Terraform fact.
- **The `aws_resource_materialization:<scope>` entity key is shared, not
  private.** Eleven other reducer-intent builders emit the identical key so
  their handlers gate on the `CloudResource` substrate this domain publishes,
  and `internal/storage/postgres`'s `reducerCloudResourceNodeConflictKey`
  hashes any intent carrying the prefix into the shared cloud-resource-node
  queue conflict family. Changing the literal here silently changes readiness
  gating and queue conflict grouping across every one of those families. Grep
  the repository for the literal before touching it; do not treat it as this
  package's private string.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed `CollectorKind`.
  The pre-extraction root file already called that shared seam directly, so
  this extraction substituted no helper.
- Do not decode the payload, derive `CloudResourceUID`, or write nodes here.
  The reducer handler owns all three.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by this package's tests, by the root dispatch test
  (`../aws_resource_materialization_projection_test.go`), and by the root
  fan-out parity fixture (`../scope_generation_intents_fanout_parity_test.go`,
  which lists `reducer.DomainAWSResourceMaterialization` in both
  `fanOutParityExpectations` and `fanOutParityExpectedOrder`). Change them
  together — and for the entity key, read the shared-key invariant above
  first.
- **Changing the trigger kind.** This is a correctness decision, not a
  cleanup. AWS is scanned whole every generation, so `aws_resource` presence
  is the signal several sibling families rely on for retraction-safe
  reconciliation; narrowing it starves them. Update the root dispatch tests in
  the same change.
- **Adding a probe to root assembly.** Root's
  `documentedReducerIntentProbeCount` (`../reducer_intent_fact_index.go`) and
  the "N probes" prose in `../README.md` are pinned by
  `TestReducerIntentProbeCountMatchesDocumentedCount`. Replacing a root
  builder with a package-qualified one — as this extraction did — leaves the
  count unchanged at 44, because that test counts distinct probes, not
  identifiers of one shape.

## Failure modes

- **A route-serves-data registry citation was checked and found absent, but
  the check is not free.** `go/internal/mcp/route_serves_data_registry.go` and
  `route_serves_data_registry_routes*.go` cite projector source files by full
  repository path and read them looking for a marker string, so a
  projector-only file rename can fail a test in `internal/mcp` with
  `read <path>: no such file` and nothing in `internal/projector` pointing at
  the coupling. Neither registry file cites this package or its
  pre-extraction root path
  (`go/internal/projector/aws_resource_materialization_intents.go`) — the
  registry's only projector citations are three
  `go/internal/projector/cloudinventory/admission_intents.go` rows, which
  served as the positive control proving the search string was right. Re-run
  the check on any future rename here:
  `rg -n 'internal/projector' go/internal/mcp/route_serves_data_registry*.go`
  then `cd go && go test ./internal/mcp/ -run TestRouteServesDataRegistry -count=1 -v`,
  and confirm the `-v` output actually lists `=== RUN` lines — a `-run`
  pattern that matches nothing still exits 0.
- **A stale citation of the pre-extraction unexported name.** Before this
  move, `buildAWSResourceMaterializationReducerIntent` was named by six
  places outside the file that defined it, including
  `go/internal/reducer/ec2_uses_profile_materialization.go` and a synthetic
  AST fixture in `../reducer_intent_probe_count_test.go` labelled "root
  builder", which would have kept naming a function that no longer exists. No
  gate scans `docs/internal/**`, so sweep the whole repository — not just
  `go/` — for the old symbol whenever a builder here is renamed or exported.
- **The root test file is not this package's test file.**
  `../aws_resource_materialization_projection_test.go` drives `buildProjection`,
  a root-only function this package cannot call, so it stayed at root. Do not
  move it here and do not delete it as a duplicate; it is the only proof that
  root assembly still reaches this builder.
