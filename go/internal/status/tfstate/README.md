# Terraform State Status

## Purpose

`internal/status/tfstate` owns the Terraform-state family of the status
report: the most recent observed state serial per locator, and the recent
warning-fact evidence collected against those locators. It exists so the
root `internal/status` package (which aggregates every family into
`RawSnapshot` and `Report`) has one place that owns "is this Terraform-state
scope fresh, and what warnings has it thrown."

## Ownership boundary

This package owns Terraform-state serial and warning row shape, grouping,
summarization, and rendering. It does not own warning classification itself
(that is `internal/tfstatewarning`, which this package calls to backfill
severity/actionability); it does not own any other status family.

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `tfstate`; `tfstate` may import neither the root nor a
sibling leaf.

## Exported surface

- `LocatorSerial` — most recent observed state serial for one locator
- `LocatorWarning` — one recent warning_fact observation for one locator
- `WarningSummary` — bounded warning totals by kind/reason/scope class
- `MaxRecentWarnings` — the per-locator cap on recent warning rows returned
- `CloneSerials`, `CloneWarnings` — defensive copies (`CloneWarnings` also
  backfills severity/actionability via `tfstatewarning.Classify`)
- `SortSerials`, `SortWarnings` — deterministic ordering for stable JSON
- `GroupWarningsByKind` — buckets warnings per locator, then per warning kind
- `SummarizeWarnings` — deterministic aggregate warning counts
- `Report` — the projected per-locator serial/warning shape the admin status
  surface renders
- `ReportJSON` and the row-level JSON projections — the wire shapes

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NullableRFC3339Value`
- `internal/tfstatewarning` — `Classify`, the warning-kind/reason →
  severity/actionability classifier this package backfills from

## Telemetry

None. This package performs no I/O; the Postgres query that bounds and
sources the raw rows lives in the status reader, and this package only sorts,
groups, and projects what it is given.

## Gotchas / invariants

- `CloneWarnings` only backfills `Severity`/`Actionability` when a row
  arrives with either field blank; it never overwrites a value the caller
  already set. A row with a stale or wrong classification supplied by the
  caller passes through unchanged.
- `SummarizeWarnings` skips any row whose `WarningKind` or `Reason` is blank
  after trimming — a malformed row is silently excluded from the summary
  rather than counted under an empty key.
- `SummarizeWarnings`'s `ScopeClass` is the lowercased, trimmed
  `BackendKind`, defaulting to `"unknown"` when blank. It is not a distinct
  field sourced elsewhere; do not expect a scope-kind lookup.
- `MaxRecentWarnings` bounds the JSON projection, not the database. Postgres
  is expected to already cap rows per `safe_locator_hash`; this constant is a
  second, in-process bound so a misbehaving query cannot grow the rendered
  payload without limit across restarts.
- `terraformStateReportJSON` (called from root `json.go` as
  `terraformStateReportJSON(report.TerraformState)`) is unexported today and
  must become `ReportJSON` on the move. It returns `nil` when the report
  carries no serials, warnings, or summary rows, so the admin status
  response omits the tfstate section entirely for runtimes that never
  observe tfstate evidence — preserve that nil-on-empty behavior.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
