# Cloud Status

## Purpose

`internal/status/cloud` owns the AWS cloud-collector family of the status
report: per-tuple scan status and the aggregate freshness-trigger backlog
that drives re-scans. It exists so the root `internal/status` package (which
aggregates every family into `RawSnapshot` and `Report`) has one place that
owns "is the AWS collector scanning, and how fresh is its backlog."

## Ownership boundary

This package owns AWS scan-row and freshness-backlog shape and rendering. It
does not own the unified cross-collector runtime view or promotion proofs —
those live in `collector`, which imports `cloud.AWSScanStatus` as one of the
several evidence sources it merges. It does not own vulnerability-source,
Terraform-state, semantic-extraction, queue, or generation evidence.

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `cloud`; `cloud` may import neither the root nor a
sibling leaf. `collector` importing `cloud` is the one declared exception to
the no-sibling-imports rule, and it runs in this direction only — `cloud`
itself has no knowledge of `collector`.

## Exported surface

- `AWSScanStatus` — one AWS collector tuple status row
  `(collector_instance_id, account_id, region, service_kind)`
- `CloneAWSScanStatuses` — defensive copy of a scan-status slice
- `RenderAWSScanLines` — plain-text operator lines for scan rows
- `AWSScanJSON`, `AWSScansJSON` — the scan-row wire shape and its projection
- `AWSFreshnessSnapshot` — aggregate freshness-trigger backlog state
- `CloneAWSFreshnessSnapshot` — defensive copy with a clamped oldest-queued
  age
- `RenderAWSFreshnessLines` — plain-text operator line for the freshness
  backlog
- `AWSFreshnessJSON`, `AWSFreshnessJSONFrom` — the freshness wire shape and
  its projection

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NamedCount`, `NamedCountJSON`,
  `NullableRFC3339Value`, `NonNegativeDuration`

## Telemetry

None. This package performs no I/O and emits no metrics, spans, or logs; it
is pure value transformation over rows the status reader already gathered.

## Gotchas / invariants

- `AWSScanJSON`'s field tags and `AWSFreshnessJSON`'s formatting are
  operator-facing API. They are locked by the byte-for-byte wire goldens in
  `internal/status/testdata/`; changing them is an API change, not a
  refactor.
- `RenderAWSFreshnessLines` returns `nil` when the snapshot has no status
  counts and a zero oldest-queued age, so an empty section renders nothing
  rather than a zeroed line.
- `AWSFreshnessJSONFrom` returns `nil` under the same empty condition so the
  wire payload omits the `aws_freshness` key entirely rather than emitting an
  all-zero object.
- `awsFreshnessCount` (unexported) looks up a named bucket by exact string
  match against `AWSFreshnessSnapshot.StatusCounts`; a caller that renames a
  status bucket without updating the four hardcoded names
  (`queued`/`claimed`/`handed_off`/`failed`) in `RenderAWSFreshnessLines`
  silently drops that bucket from the rendered line.

## Evidence

No-Regression Evidence (#6949 batch 1, cloud family): this change moves
the two `cloud.*` compat entries' Go importers (`AWSCloudScanStatus` and
`AWSFreshnessSnapshot`) off the transitional `status.*` compat spellings
onto `cloud.AWSScanStatus` and `cloud.AWSFreshnessSnapshot`, and deletes
the emptied `compat_cloud.go`. No type shape, wire field, SQL text, or
executable statement changes: across 13 files, every production hunk
requalifies an identifier or import path only, every other hunk is a
package-doc rewording, a ledger row, or the compat file's own deletion,
and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6949-cloud-batch1-before,
ledger:6949-cloud-batch1-after). The command is `go test -count=1` over
the 3 affected package targets (`./internal/status/...`,
`./internal/query/`, `./internal/storage/postgres/`) on baseline
`16c8c2a365` vs measurement commit `7f68c9cb5c` (this Evidence section,
the two ledger rows, and content-identical rebases tracking main are the
only later changes): 3 packages ok, 0 fail on both sides, with the
ok-package set byte-identical after timing strip. `go test -list`
inventory is identical on both sides (5482 tests). Backend/version:
go1.26.9 linux/amd64, in-memory; no backend touched. Input shape: n/a
(no runtime input). Terminal queue/row counts: none — no queue, lease,
Cypher, or SQL path is touched. Contract gates green on the branch:
the three byte-for-byte wire goldens
(`TestRenderWireJSON_ByteForByteGolden`,
`TestRenderWireText_ByteForByteGolden`,
`TestRenderWireJSON_KeyPathsGolden`), `verify-openapi.sh` (261/261
routes), `verify-contracttest.sh`, and `verify-package-docs.sh`. The
change is safe because it cannot alter runtime behavior: the compiler
resolves the same types through their new paths, and the compat
deletion is compile-enforced total — any missed caller would fail the
build.

No-Observability-Change (#6949 batch 1, cloud family): this package
carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in
any touched package. The status readers, query handlers, and renderers
that use these types are untouched; operator signals are identical
before and after.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
