# AWS activation key wiring: historical backup evidence

The owner ordered a push of work in progress at backup commit `d994e48dc8a1`,
without a PR. At that commit, the dedicated activation ServiceAccount test
failed because the chart did not support that configuration. This note records
only the backup state; it does not assess later implementation commits. The
backup commit was not ready for merge or deployment.

No-Regression Evidence: The active, nonblank, non-comment lines of
`deploy/helm/eshu/values.yaml` are identical at baseline
`ebe03d6aa9a612c5f73f232e2f1a025eed4fe842` and commit `d994e48dc8a1`.
After joining those lines with newlines and adding a trailing newline, both
SHA-256 hashes are
`bbda86b677fffd3ebca346ca4493196e2ce9c25e852f3d3bbd20f2e3638a274a`.
The hook classifies indented YAML comments as code because it checks for `#`
before trimming indentation. This is a static values-file comparison; backend,
queue and row counts do not apply. It is not a measurement of the changed activation templates. Their
runtime performance and dedicated ServiceAccount behavior remain NOT_PROVEN.

Observability Evidence: The existing collector guide names `/healthz`,
`/readyz`, `/metrics`, and `/admin/status?format=json` as operator surfaces.
This comparison did not run a deployment or observe those signals. The activation template changes require separate runtime and
operator-signal verification before promotion; this backup does not establish
that evidence.
