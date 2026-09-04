# Crossplane-satisfied-by projector intents

## Purpose

This package recognizes Crossplane Claim-satisfaction evidence in one scope
generation and builds the reducer intent that asks the reducer to run its
`crossplane_satisfied_by_materialization` join. The trigger fires on the
earliest `content_entity` fact whose `entity_kind` (falling back to
`entity_type`) is `K8sResource` or `CrossplaneXRD` — the only two entity
types `crossplane.ExtractCrossplaneSatisfiedByEdgeRows` classifies (issue
#5347). A Crossplane Claim candidate is never parser-labeled: it is an
ordinary `K8sResource` row, so the trigger reads the entity type directly
rather than firing on any `content_entity` presence, which would enqueue a
(cheap but unnecessary) intent for every repository with parsed code
entities.

## Ownership boundary

The package owns only trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The
reducer's `DomainCrossplaneSatisfiedByMaterialization` handler
(`go/internal/reducer/crossplane/crossplane_satisfied_by_materialization.go` and its
sibling files) owns the cross-scope join against active CrossplaneXRD facts,
`ExtractCrossplaneSatisfiedByEdgeRows`, and the `SATISFIED_BY` graph write;
none of that happens here.

## Exported surface

- `BuildCrossplaneSatisfiedByMaterializationReducerIntent` builds the
  `crossplane_satisfied_by_materialization` intent, anchored to the earliest
  accepted `content_entity` fact.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup`,
`internal/projector/intent.ReducerIntent`, and
`internal/projector/intent.SourceSystem`; this package must not import the
root projector package — root already imports this package to dispatch to
it, so the reverse import would cycle. It reads `internal/reducer` for the
domain constant.

This family carries no decode seam: `triggerFact` reads only the
`entity_type`/`entity_kind` payload keys through a package-local
`payloadString` copy (`payload.go`, a byte-for-byte trim of root's
`payload.go` helper of the same name — this package cannot import root, so
the shared logic is duplicated rather than referenced).

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total`; the
reducer handler retains `eshu_dp_reducer_executions_total`,
`eshu_dp_reducer_run_duration_seconds`, and
`eshu_dp_crossplane_satisfied_by_edges_total` for its own execution and edge
writes. The `eshu_dp_crossplane_redrive_*` counters belong to the reducer's
separate redrive sweep, not to intent enqueue. Moving the pure builder adds
no queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- The candidate-kind list (`candidateFactKinds`) must stay in sync with
  `triggerFact` — it exists so `FirstAcrossKinds` can skip kinds the
  generation does not carry before evaluating the predicate, not to change
  admission.
- `triggerFact` checks `entity_kind` first, falling back to `entity_type`,
  mirroring `projector.buildContentEntityRecord`'s dual-path read. Both keys
  must be checked in that order; only `K8sResource` and `CrossplaneXRD` are
  candidates.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`
  (trimmed `SourceRef.SourceSystem`, else trimmed `CollectorKind`). The
  pre-extraction root helper (`crossplaneSatisfiedBySourceSystem`) had the
  identical two-tier body, so this is NOT a behavior change — do not
  reintroduce a package-local copy.
- **The root fan-out parity fixture does not cover this domain.**
  `reducer.DomainCrossplaneSatisfiedByMaterialization` is absent from both
  `fanOutParityExpectations` and `fanOutParityExpectedOrder` in
  `../scope_generation_intents_fanout_parity_test.go`, and the shared fixture
  in `../scope_generation_intents_fanout_test.go` carries no
  `K8sResource`/`CrossplaneXRD` content-entity fact. This package's own tests
  are the only coverage for the reason string, entity key, and source-system
  derivation.
- `crossplaneSatisfiedByEntityFactKind` duplicates root's
  `FactKindParsedEntityObserved` (`"content_entity"`,
  `go/internal/projector/stage_facts.go`) as a literal rather than an import,
  because root imports this package to dispatch and the reverse direction
  would cycle.
- Do not decode a second payload field beyond `entity_type`/`entity_kind`,
  and do not check a schema version here; the reducer handler owns the
  cross-scope evidence load.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Package restructure](../../../../docs/internal/design/package-restructure.md)

No-Regression Evidence: this extraction moves one builder without changing its
trigger, value, or fan-out position. The reducer intent domain
(`DomainCrossplaneSatisfiedByMaterialization`), the
`crossplane_satisfied_by_materialization:<scope>` entity key, the
`k8s_resource/crossplane_xrd content-entity facts observed` reason string, and
the fact-id selection are identical to the base commit; only the scope and
generation identifiers changed from struct-field reads to parameters, carrying
the same values from the call site. The dispatcher's ordered fan-out is
unchanged at 44 builder probes on both sides, with this probe still running
immediately after `projectorkubernetes.BuildCorrelationMaterializationReducerIntent`
and immediately before
`projectorsecurity.BuildSecurityGroupEndpointMaterializationReducerIntent`.

The family's private `crossplaneSatisfiedBySourceSystem` helper was compared
body-for-body against `projectorintent.SourceSystem` and found identical -- two
tiers, both trimmed, no third literal fallback -- so it was dropped in favour of
the shared seam rather than moved. The package's own tier tests set the two
tiers to different values (`kubernetes_live` against `kubelet_scanner`), so a
regression that swapped the tier order fails them; a test giving both tiers the
same value would pass either way and prove only that a label was produced.

Unlike some sibling families, the root fan-out parity fixture does NOT cover this
domain: `DomainCrossplaneSatisfiedByMaterialization` appears in neither
`fanOutParityExpectations` nor `fanOutParityExpectedOrder`, and the shared fixture
carries no `k8s_resource` or `crossplane_xrd` content-entity fact. This package's
own tests are therefore the only thing asserting these values -- do not rely on
the parity fixture as a safety net for a change here.

No-Observability-Change: no metric, span, log, or quarantine counter is added,
moved, or renamed by this extraction. Root assembly and
`eshu_dp_reducer_intents_enqueued_total` are untouched, the
`crossplane_satisfied_by_materialization` domain keeps
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds` on
the handler side, and the two new files under this package emit no signal of
their own -- `payload.go` is unexported map and scalar readers that perform no
I/O, and `satisfied_by_intents.go` is a pure trigger-and-value builder. The
telemetry-coverage rows for both were written from what the files contain rather
than copied from a sibling.
