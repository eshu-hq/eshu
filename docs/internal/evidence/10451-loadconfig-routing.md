# LoadConfig staged-audits admission: regression and observability note (#10451)

## Change

One new `case "staged-audits"` in the `LoadConfig` routing switch plus the
matching enum description in the env registry. No query text, plan shape,
pool sizing, lease, queue, worker, or concurrency knob changes.

## No-Regression Evidence:

- Baseline: `TestLoadConfigAdmitsStagedAuditsRouting` failed pre-fix with
  `ESHU_POSTGRES_READ_ROUTING must be off or stale-audits`, reproducing the
  startup rejection a flip would hit.
- After: the test passes, and the full `go test ./internal/runtime/postgres/`
  and `./internal/envregistry/` suites pass with no other test touched.
- Backend: none — pure config parsing, no database, no fixture, no row
  counts. Input shape is four routing strings plus one bogus value.
- Why safe: additive enum case; the closed set is preserved (bogus still
  rejected); the distinct-standby-DSN rule above the switch applies
  unchanged to the new mode.

## No-Observability-Change:

Config parsing emits no metric, span, or log line; the change adds no
operator signal and alters no existing one. Startup acceptance is covered
by the routing table test above.
