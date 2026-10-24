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
<<<<<<< HEAD
- `TerminalFailure` (with the operator acknowledgement flag, #8830),
  `AcknowledgedFailure` (the distinct sub-record an acknowledged failure leaves
  on `RuntimeStatus` and `PromotionProof`), `BoundedFailureClassForKind`, `BoundedFailureClasses`,
=======
- `TerminalFailure`, `BoundedFailureClassForKind`,
  `BoundedFailureClassForInstance`, `BoundedFailureClasses`,
>>>>>>> 0db2364a6b (feat(status): read extension-defined failure classes on component-activated first-party instances)
  `FailureClassesForKind`, `ExtensionDefinedFailureToken`,
  `BoundedFailureOperation` — the
  newest unresolved terminal work-item failure per instance and the
  per-kind allowlist that bounds its class (the nine common classes, the four
  extension host classes every kind admits, each first-party kind's own, and
  one token for extension-defined classes on kinds that are not first-party or
  on an extension-hosted instance, `InstanceSummary.ExtensionHosted`, #8951) and
  operation tokens; `RuntimeStatus` and
  `PromotionProof` carry the result as `LastFailure*` fields (#8803)
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
  `PresentCatalog` helper take the collector-native `Evidence` value
  (coordinator instances, AWS scans, vulnerability sources, and collector
  fact evidence) instead of the root `Report` type, because this package
  cannot import root `Report`. Callers project with
  `report.CollectorEvidence()` and call
  `collector.RuntimeStatuses(report.CollectorEvidence())` and
  `collector.PromotionProofs(report.CollectorEvidence(), opts)` directly;
  the transitional `status/compat_collector.go` forwarders are deleted.
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

## Evidence

No-Regression Evidence (#6949 batch 7, collector family): this change moves
the 30 `collector.*` compat entries' Go importers (9 type aliases, 17 consts,
and 4 funcs, reached as both `status.*` and `statuspkg.*`) off the
transitional compat spellings onto the destuttered `collector.*` names, and
deletes the emptied `compat_collector.go`. The two `Report`-taking forwarders
(`CollectorRuntimeStatuses`, `CollectorPromotionProofs`) cannot move into the
leaf as-is — the leaf cannot import root `Report` — so the root keeps the
projection as a new `Report.CollectorEvidence()` method with a real doc
comment, and the 6 caller files of those two (4 query prod files, 2
external root tests) now call `collector.RuntimeStatuses(...)` and
`collector.PromotionProofs(...)` over it; the 4 same-package root callers
that used the bare names move the same way. No shadowing local needed a
rename (the one `collector` loop variable sits outside the import's use
scope). No type shape, wire field, SQL text, or executable statement
changes: across 45 code files, every hunk requalifies an identifier or edits
an import path only (zero non-mechanical changed lines outside the compat
deletion, the new projection method holding the existing field copies, and
the doc rewords). The status, collector, and query READMEs, the collector
AGENTS.md, and the leaf doc.go are reworded to the settled names.
Measurement: packages ok on both sides with identical sets (ledger:6949-collector-batch7-after).
Sibling before-row `6949-collector-batch7-before`; baseline `19a12e107f`
vs measurement commit `e4a8070df8` (the two ledger rows and this Evidence
section are the only later changes), with the ok-package set byte-identical
after timing strip.
`go test -list` inventory is identical on both sides (11571 tests).
Backend/version: go1.26.2 linux/amd64, in-memory; no backend touched. Input
shape: n/a (no runtime input). Terminal queue/row counts: none — no queue,
lease, Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`,
`verify-package-docs.sh`, and `verify-performance-evidence.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same types through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.
No-Observability-Change: no metric, span, log line, or status output changes;
telemetry evidence is the unchanged test outcomes above.
