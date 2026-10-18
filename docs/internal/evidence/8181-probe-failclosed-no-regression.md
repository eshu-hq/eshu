# #8181: probe fail-closed no-regression note

Refs #8181. `deltaUnionBasePruned` used to return `false` when the retention
probe itself errored, so `resolveDeltaUnionBase` overlaid a possibly
half-pruned superseded base as a complete result. The fix maps both probe
failure modes (`QueryContext` error, `rows.Err`) to the existing
`deltaBaseMissing` refusal. Probe SQL text is unchanged.

No-Regression Evidence (#8181): baseline is the old fail-open code at
`633a3969`, where the seeded test fails with `ListFacts() = 3 facts, want
nil` (partial projection confirmed). After the fix at `24e46ad8` the same
test passes: nil facts, `DeltaBaseMissingError` with scope, generation, and
baseline, 4 queries with base facts never read. Full package
`go test ./internal/storage/postgres/ -count=1` passes in about 6.1 s and
`go vet` is clean. Backend is the Go unit suite with a stubbed queryer
(Postgres 18.6 live wave runs in CI before merge). Input shape is a
half-pruned superseded base plus a failing probe. Query count is identical
on every path: one indexed probe for superseded bases only, served by
`generation_retention_events_generation_idx`, so no plan change is possible.

No-Observability-Change (#8181): the refusal flows through the existing
`union_base_missing_refused` outcome via `recordDeltaUnionRefused`, with the
existing counter, span event, and log line. No new label or signal was
added, and the forced-full path after refusal is untouched.
