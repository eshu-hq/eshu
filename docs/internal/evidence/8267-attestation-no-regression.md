# #8267 per-collector attestation: no-regression evidence

Scope: branch `feat/8267-per-collector-attestation`. PagerDuty and
Confluence gain the `allow_empty_successor` operator key on both
config surfaces, stamped onto the generation as `TrustedDeletion`;
the extension host forwards it. The gate flagged collector config
and mapping files as hot by path; this note records why the change
cannot regress runtime cost.

No-Regression Evidence (config-parse-time boolean): the old code
parsed configs without the key; the new code parses one extra
optional boolean per target and copies it through two struct
assignments per collection setup. Both are O(1) with no allocation
beyond the config structs, no I/O, no lock, and no retry change,
and the parse runs once per collector setup, never per row, scope,
or generation, so there is no corpus scaling term. Baseline:
`go test ./internal/collector/pagerduty/
./internal/collector/confluence/ ./internal/collector/extensionhost/
./internal/workflow/ ./cmd/collector-pagerduty/ -count=1` all `ok`
(0.2-0.3 s each) at HEAD `b3d287ecb` (GOTOOLCHAIN=go1.26.9,
darwin/arm64 dev harness, not a production backend). After
measurement equals the baseline shape: the same suites plus
round-trip, stamp, and forward tests, all passing. Input shape:
attested and unattested target configs across both surfaces.
Queue/row counts: not applicable, the key is read at setup before
any enqueue and touches no rows. Change is safe because the key
defaults false on every surface, so unattested inputs take the
identical path before and after, and the guard predicate itself is
untouched.

No-Observability-Change: the collection path emits no new spans,
metrics, or logs. `TrustedDeletion` travels inside the existing
generation identity the freshness hint already mints. No operator
dashboard or alert changes.
