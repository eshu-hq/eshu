# Telemetry Contract — Third-Party Sources

## Purpose

`thirdparty` holds the source-collector span, span-attribute, and
metric-dimension declarations for Eshu's hosted third-party-source
collectors: Jira work-item evidence, PagerDuty incident evidence, and the
live Vault collector's redaction dimension. These used to live directly in
root package `telemetry` as `contract_jira.go`, `contract_pagerduty.go`, and
`contract_vaultlive.go` (issue #6777).

## Ownership boundary

Declarations only — one file per source. No registration order, no
`func init()`, no `go/internal/*` imports (including root package
`telemetry`, which would cycle).

## Exported surface

- `SpanJiraObserve`, `SpanJiraFetch`, and the `SpanAttrJira*` bounded
  fetch-span attribute keys (search/changelog/remote-link/metadata page
  counts, emitted/rejected counts, rate-limit and staleness signals)
- `SpanPagerDutyObserve`, `SpanPagerDutyFetch`
- `MetricDimensionFieldClass` and the `FieldClassURI*` bounded values the
  live Vault collector uses on `eshu_dp_secrets_iam_source_redactions_total`

## Dependencies

None beyond the declarations themselves.

## Telemetry

This package is telemetry declarations — see Purpose.

## Gotchas / invariants

- Root `go/internal/telemetry` re-exports every name here as a compat alias
  (`compat_thirdparty.go`). Registration into the ordered `spanNames` /
  `metricDimensionKeys` slices happens in root `registration.go` /
  `registration_steps.go`, not here.
- `TestSpanNames` and `TestMetricDimensionKeys` in root `contract_test.go`
  pin the exact frozen splice order.
- Jira's `SpanAttrJira*` constants are span attributes, not metric labels,
  specifically so site IDs, issue keys, user identifiers, summaries,
  metadata names, custom-field IDs, and URLs stay out of dashboard
  cardinality — keep new Jira signal that carries an identifier as a span
  attribute here, not a new metric dimension.

## Related docs

- `go/internal/telemetry/contract/README.md`
- `go/internal/telemetry/README.md`
