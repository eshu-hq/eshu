# Coordinator scheduling substrate

## Purpose

`schedule` collects the scheduling primitives shared by the coordinator root
and its `vulnerability` and `registry/package` families: target-class ranking,
derived-target skip evidence and budget accounting, rotation buckets, offsets and plan
keys for paging a derived target set, read-limit widening, and small
string-set and dependency-version helpers.

## Ownership boundary

This package owns the primitives themselves. It does not own a scheduler: the
coordinator root, `vulnerability`, and `registry/package` each keep their own
planner and `Service` methods and call into `schedule` for target ranking,
plan-key rendering, and rotation math. `schedule` imports nothing from the
coordinator root, so the root and both families can depend on it without a
cycle.

## Exported surface

- Target-class constants (`TargetClassConfiguredDirect`,
  `TargetClassOwnedPackage`, `TargetClassInstalledOS`,
  `TargetClassSBOMComponent`, `TargetClassBroad`) and `TargetClassRank`.
- `TargetCreatedAt` for Postgres-precision-safe target ordinal spacing.
- `DefaultReconcileInterval`, planning-mode constants
  (`PlanningModeRotating`, `PlanningModeSinglePass`) and
  `NormalizePlanningMode`.
- `DerivationEcosystems`, `StringSet`, `StringSetContains`,
  `SortedStringSetValues`, `DerivationLimit`.
- `DerivedTargetRotationOffset`, `DerivedTargetRotationOffsetForMode`,
  `DerivedTargetRotationBucket`, `DerivedTargetRotationBucketForMode`,
  `DerivedTargetSlotLimitForMode`, `DerivedTargetPlanKey`,
  `DerivedTargetReadLimit`. The owned-package reader selects hash slot
  `bucket mod P` (#9422); the installed-advisory readers still page by
  `bucket * limit` until #9427.
- `ExactOwnedDependencyVersion`, `NonVersionOwnedDependencyPrefix`,
  `FirstNonBlank`.
- `ScanIntervalConfigKey`, `ParseScanInterval` (one configuration document),
  and `ParseScanIntervalValue` (the JSON value under that key). The
  coordinator validates and plans with them; the status reader in
  `storage/postgres` sizes the collector terminal-failure window (#8803) from
  `configuration->'scan_interval'` through `ParseScanIntervalValue`, so both
  read one configured value with the same rules.
- `DerivedTargetSkipEvidence` and its skip-reason constants, plus
  `DerivedTargetSkipEvidenceByReason`,
  `DerivedTargetSkipEvidenceByReasonForClass`, `RecordDerivedTargetSkip`, and
  `DerivedTargetBudgetSkipEvidence`.

See `doc.go` for the godoc contract.

## Dependencies

- Standard library only (`strings`, `sort`, `time`, `fmt`, `encoding/json`) plus
  `golang.org/x/mod/semver` for owned-dependency version validation.

The package does not import the parent coordinator package or either family
package.

## Telemetry

None. This package computes values consumed by the callers' reconcile passes;
it emits no metrics, spans, or logs of its own.

No-Observability-Change: this move adds or renames no metric, span, log
field, status field, queue, worker, lease, or runtime setting. Each caller's
existing reconcile telemetry is unchanged.

## Gotchas / invariants

- `TargetClassRank` ranks an unrecognized class last (4) so it can never
  preempt a known class.
- `DerivedTargetRotationBucket` truncates the observed time with the same
  basis the plan key uses, and `DerivedTargetRotationOffset` is that bucket
  times the limit; changing the truncation basis in one without the other lets
  a rotating instance page to new targets under a stale plan key. A clock
  before 1970 clamps to bucket 0 so the SQL `MOD` stays non-negative.
- `DerivedTargetSlotLimitForMode` returns 0 for `single_pass`, which the
  owned-package reader treats as one slot (the first page of the whole set).
- `DerivedTargetPlanKey` returns a fixed `<prefix>-single-pass` key for
  single-pass mode and an interval-truncated timestamp key for rotating mode;
  callers must not mix the two modes for the same plan without also changing
  the key.
- `ExactOwnedDependencyVersion` rejects ranges, tags, and non-registry
  references (git/http/workspace/etc. prefixes) even when they parse as valid
  semver after a `v` prefix is added.
- `DerivedTargetReadLimit` widens by exactly one row
  (`budgetExhaustionLookahead`) so a caller can distinguish "exactly at
  budget" from "budget exhausted with more waiting"; it returns the raw limit
  unchanged when the limit is non-positive.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/vulnerability/intelligence_scheduler.go`
- `go/internal/coordinator/registry/package/scheduler.go`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
