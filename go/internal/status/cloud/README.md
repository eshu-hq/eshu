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

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
