# crossrepo

Resolves cross-repository relationships from persisted evidence facts and emits
the durable repo-dependency projection intents the shared projection later turns
into graph edges.

This package moved out of the flat `internal/reducer` root under issue #6061. It
is a domain family: it owns one handler and the pipeline behind it, and nothing
else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `CrossRepoRelationshipHandler` | `cross_repo_resolution.go` | the reducer handler the `deployment_mapping` domain registers; loads evidence and assertions, runs `relationships.Resolve`, persists the audit trail, emits intents, then activates the generation |
| `CrossRepoEvidenceSource` | `cross_repo_resolution.go` | the `evidence_source` this resolver stamps on every edge it writes |
| `EvidenceFactLoader`, `AssertionLoader`, `RepoDependencyIntentWriter` | `cross_repo_resolution.go` | three of the ports the handler is wired through |
| `ResolutionPersister`, `ClaimFencedResolutionPersister` | `cross_repo_resolution_claim.go` | the persistence and claim-fenced activation ports |
| completion wake (#8294) | `cross_repo_resolution_completion_wake.go` | `ResolvedUpsertResult`, `CompletionWakeResult`, and the `DeployableUnitCorrelationWaker` port: after each resolution pass the handler wakes the deployable_unit_correlation items whose recorded input no longer holds over the pass's full resolved repo scope, on retries too; the queue write lives in `storage/postgres` |
| `ExtractRepoDependencyIntentRows` | `cross_repo_intent_row.go` | the resolved-relationship to intent-row conversion, exported for Ifá's materialized-edge vacuity guards |
| retraction rows | `cross_repo_resolution_retract.go` | the rows emitted for a source repo that resolved to no edges, so a stale edge is withdrawn rather than left standing |
| evidence artifacts | `cross_repo_evidence_artifacts.go` | the bounded `EvidenceArtifact` summaries an edge carries, including citation fields and the `GITHUB_ACTIONS_*` ref/pinned classification |
| evidence type and source tool | `cross_repo_evidence_type.go` | the pure evidence-kind to `evidence_type` / `source_tool` classifier |
| own gate (#7597) | `own_gate.go` | `EvaluateOwnGate`, the own-generation gate of `deployable_unit_correlation` and `workload_materialization`, bounded by this handler's own `deployment_mapping` work item: `ProducerLookup`/`ProducerState`, the counting `ProducerDeadLetteredError` and `ProducerMissingError`, and the `*_producer_dead_lettered` and `*_producer_missing` classes, all derived from one per-domain map |

`CrossRepoEvidenceSource` is exported on purpose. Workload materialization
writes the identical `(WorkloadInstance)-[:RUNS_ON]->(Platform)` shape under a
different `evidence_source`, so relationship type and endpoint labels cannot
tell the two families' edges apart. The Ifá assert gate reads this constant
rather than a copied literal, so its partition cannot drift from what the writer
stamps.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/crossscope` and `reducer/factload` (the own gate's elapsed bound and
lookup-error classifier), `reducer/gpphase`, `reducer/payloadcore`,
`reducer/sharedintent`,
`internal/environment`, `internal/ghactionsref`, `internal/relationships`,
`internal/telemetry` and `pkg/log`, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in the cross-repo stanza of `compat_correlation.go` for every name its own files,
`cmd/reducer`, `internal/ifa/materializededges` and `internal/storage/cypher`
still spell as `reducer.X` — ten of them today, and that file is the list.

Every blocker the move surfaced was of the first kind described in `AGENTS.md`
-- a root one-line forwarder or type alias to a leaf that had already moved out
-- so nothing was hoisted and no body was copied. `SharedProjectionIntentRow`,
`SharedProjectionIntentInput` and `BuildSharedProjectionIntent` resolve to
`sharedintent`; `GraphProjectionPhaseKey`, `GraphProjectionPhase`,
`GraphProjectionReadinessLookup`, `GraphProjectionReadinessPrefetch`,
`GraphProjectionPhaseBackwardEvidenceCommitted` and
`GraphProjectionKeyspaceCrossRepoEvidence` resolve to `gpphase`;
`toStringSlice` resolves to `payloadcore`; `DomainRepoDependency` resolves to
`reducer/contract`.

## Telemetry

`Resolve` emits five instruments, all registered in
`go/internal/telemetry/instruments.go`:

| instrument | when |
|---|---|
| `eshu_dp_cross_repo_resolution_duration_seconds` | once per generation, on each **successful** exit path -- an error return skips it, so the histogram counts completed resolutions only |
| `eshu_dp_cross_repo_evidence_loaded_total` | after the loaded evidence facts are deduped |
| `eshu_dp_cross_repo_edges_resolved_total` | once per owned resolved edge routed for materialization, labeled by `relationship_type`; this preserves the established counter contract |
| `eshu_dp_cross_repo_edges_dropped_total` | once per edge withheld by the ownership partition, labeled by `relationship_type` and bounded `reason` (`foreign_owned`) |
| `eshu_dp_cross_repo_activation_fenced_total` | when durable acceptance intents fail to commit and generation activation is fenced |

`eshu_dp_cross_repo_activation_fenced_total` is the one to look at first when
resolved edges stop appearing in the graph while resolution itself looks
healthy: the edges resolved, the intents did not commit, and activation was
withheld on purpose.

`eshu_dp_cross_repo_edges_dropped_total{reason="foreign_owned"}`
records the edges discarded by the single-writer ownership partition. A rise
means the expected foreign copy was observed; a change in its relationship-type
distribution helps expose attribution drift without relying on log search.

Every `Instruments` access is nil-guarded, so a handler constructed without
telemetry resolves normally and reports nothing -- which is also why an empty
panel can mean "not wired" rather than "no work".

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers, `contract` for vocabulary) and leave
  a root alias, rather than reaching upward.
- **A retraction is a row, not a silence.** A source repo that resolves to no
  edges emits retraction intent rows; dropping that path would leave a stale
  edge standing rather than withdrawing it.
- **`""` and `unknown` are different source-tool answers.**
  `sourceToolForEvidenceKind` returns `""` for a kind it cannot map, and
  `resolvedRelationshipSourceTool` turns that into the explicit
  `sourceToolUnknown` token only when a primary evidence kind was present at
  all; an edge with no evidence kind yields `""` and is not stamped. Collapsing
  the two loses the difference between "unclassified tool" and "no evidence".
  `scripts/verify-edge-source-tool-coverage.sh` reads
  `cross_repo_evidence_type.go` directly to prove every evidence kind is
  classified, so adding an evidence kind fires that gate; its path is pinned in
  `specs/ci-gates.v1.yaml`.
- **`evidence_type` and `source_tool` derive from the FIRST evidence kind in
  the preview**, so preview ordering is part of the contract, not a display
  detail.
- **The own gate counts a dead producer, never defers on it forever.** When
  a consumer's own relationship generation is inactive, `EvaluateOwnGate`
  reads the newest `deployment_mapping` row of the same scope generation and
  the generation's `relationship_generations` status. Only `dead_letter` (or
  legacy `failed`) counts; live, superseded and succeeded producers, a moved
  scope, and a non-active generation row (re-resolution) keep the caller's
  non-counting deferral without a bound; only when neither row exists does it
  count, after `crossscope.ProducerReadinessMaxWait`. The
  counting errors must never wrap the deferral (errors.As would find its
  non-counting class), and their classes must never be enrolled as
  non-counting. A lookup error goes through `factload.ClassifyFactLoadError`.
- **Activation is fenced, not skipped, on an intent-commit failure.** The
  handler returns without activating the generation so downstream consumers
  never read a half-committed resolution; the fenced counter is the only signal
  that this happened.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- [Edge source-tool provenance](../../../../docs/public/reference/edge-source-tool-provenance.md)
