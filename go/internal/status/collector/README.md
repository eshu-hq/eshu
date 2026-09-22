# Collector Status

## Purpose

`internal/status/collector` owns the collector-fleet family of the status
report: claim backpressure, fact evidence, generation dead letters, the
readiness catalog, promotion proofs, the unified runtime-status view, and
vulnerability-intelligence source checkpoints. It exists so the root
`internal/status` package (which aggregates every family into `RawSnapshot`
and `Report`) has one place that owns "is this collector working, and why
not."

## Ownership boundary

This package owns collector-fleet evidence and its derived readiness
verdicts. It does not own coordinator registration data beyond
`InstanceSummary` (the rest of `CoordinatorSnapshot` stays in root, since it
aggregates counts across every collector), AWS scan-row shape (owned by
`cloud`, imported here), or Terraform-state, semantic-extraction, queue, or
generation evidence (each owns its own leaf).

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `collector`; `collector` may import neither the root nor
a sibling leaf, with one declared exception — `collector` imports `cloud` to
fold `cloud.AWSScanStatus` rows into `RuntimeStatus`, because AWS cloud-scan
evidence is one of the inputs the unified runtime view merges alongside
coordinator registration, vulnerability-source evidence, and fact evidence.

## Exported surface

- `BackpressureSnapshot` — bounded claim pressure for one collector
  family/instance/source-system tuple
- `FactEvidence` — persisted source/reducer fact evidence for one collector
  runtime, without source payload identifiers
- `GenerationDeadLetterSnapshot` — collector generation commit failures
  quarantined before normal queue rows existed
- `CatalogEntry`, `DefaultCatalog`, `KnownKinds` — the readiness catalog:
  every known collector family, in `scope.AllCollectorKinds` order
- `RuntimeStatus`, `RuntimeStatuses` — the unified operator view of one
  collector runtime identity, merged from coordinator registration, AWS
  scans, vulnerability sources, and fact evidence
- `PromotionProof`, `PromotionProofs`, `PromotionOptions` — the deterministic
  per-family/instance promotion verdict (implemented, partial, failed,
  stale, gated, disabled, permission_hidden, unsupported) and its blockers
- `VulnerabilitySourceState` — one durable vulnerability-intelligence source
  checkpoint
- `InstanceSummary` — the operator-visible shape of one configured collector
  instance (used by root's `CoordinatorSnapshot`)

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NamedCount`, JSON scalar helpers, duration
  clamping
- `internal/status/cloud` — `AWSScanStatus`, folded into `RuntimeStatus`
  (the one declared leaf-to-leaf exception)
- `internal/scope` — `scope.CollectorKind`, `scope.AllCollectorKinds` for
  the readiness catalog

## Telemetry

None. This package performs no I/O; it derives readiness and runtime-status
views from data the root's status reader already gathered.

## Gotchas / invariants

- `RuntimeStatuses`, `PromotionProofs`, and the catalog's
  `presentCollectorCatalog` helper currently take the root `Report` type
  directly (they read `Report.Coordinator`, `.AWSCloudScans`,
  `.VulnerabilitySources`, and `.CollectorFactEvidence`). Since this package
  cannot import root `Report`, the mover must give these functions a
  collector-native signature (the raw ingredient slices, not `Report`) and
  leave a `Report`-shaped forwarder in `status/compat_collector.go` for the
  root and for `internal/query`'s collector-readiness and evidence-bundle
  callers, which currently call `status.CollectorRuntimeStatuses(report)` and
  `status.CollectorPromotionProofs(report, opts)` directly.
- Nearly every render/clone/JSON-projection function in this family
  (`renderCollectorBackpressureLines`, `renderCollectorRuntimeStatusLines`,
  `renderCollectorPromotionProofLines`, `renderVulnerabilitySourceLines`,
  `renderCollectorGenerationDeadLetterLine`, `cloneCollectorBackpressure`,
  `cloneVulnerabilitySourceStates`, `cloneCollectorGenerationDeadLetterSnapshot`,
  `collectorBackpressureJSONRows`, `collectorRuntimeStatusesJSON`,
  `collectorPromotionProofsJSON`, `vulnerabilitySourcesJSON`) is called
  directly from root `status.go`/`json.go`/`coordinator.go` today and is
  currently unexported. All of it must become exported on the move so the
  root can call it as `collector.RenderBackpressureLines`, etc.
- `vulnerabilitySourcesJSON` (moving here from `json_cloud.go`) returns
  `[]vulnerabilitySourceJSON`, but that struct type is defined in root
  `json.go`, not in `json_cloud.go`. It is not in this leaf's stated file
  list. The wire type must move here too (as `VulnerabilitySourceJSON`) or
  the function cannot compile in this package — confirm this before treating
  the move as complete.
- `CoordinatorSnapshot` and `CoordinatorRecentFailures` (the rest of
  `coordinator.go`) stay in root; only `InstanceSummary` moves here. Root's
  remaining `coordinator.go` will reference `collector.InstanceSummary` and
  `collector.BackpressureSnapshot` (via `CoordinatorSnapshot`'s
  `CollectorBackpressure` field) and must call the exported
  `collector.RenderBackpressureLines` from `renderCoordinatorLines`.
- `DefaultCatalog` synthesizes a display name and marks `ClaimDriven: true`
  for any `scope.AllCollectorKinds` entry with no declared metadata, so a new
  collector kind always gets a readiness lane instead of being silently
  hidden.
- `PromotionProofs` evaluates promotion state in fixed precedence — disabled,
  then failed, then gated, then stale, then implemented/partial — so the most
  actionable blocker is always first.
- `BackpressureSnapshot`, `GenerationDeadLetterSnapshot`, and the age fields
  on `RuntimeStatus`-adjacent rows are clamped through
  `shared.NonNegativeDuration`; a negative age from a status-read clock skew
  must never render.
- The `*JSON` projections are the operator-facing wire contract (status HTTP
  surfaces and MCP status tools) and are locked by byte-for-byte goldens in
  `internal/status/testdata/`; a failure there is a real API break to
  justify.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
