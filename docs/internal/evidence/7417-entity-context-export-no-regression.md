# #7417 Entity-context builder export: no-regression evidence

Review-round-2 export of the entity-context statement builder so the
backend-divergence allowlist pins bind to production code instead of
duplicated strings: `entityContextStatements` →
`EntityContextStatements` in
`go/internal/query/entity/context_anchor_neo4j.go` (plus its doc
comment), the one-word caller rename in
`go/internal/query/entity/context_handler.go`, and the two call-site
renames in `context_anchor_wide_test.go`. The non-test Go diff is 6
insertions and 2 deletions across 2 files. No query text, no control
flow, no concurrency shape, and no telemetry identifiers change; the
re-derived `GetEntityContext` digest in
`go/internal/queryplan/testdata/query-source-coverage.yaml` reflects
the symbol rename only (same anchor kind, same single-key support,
same statement count).

## No-Regression Evidence (#7417):

- Baseline: `ea67463a9` (origin/main), measured in a detached
  throwaway worktree on the same machine, serially in one session
  (after first, then base), sharing nothing but the Go build cache.
- After: branch `fix/7417-allowlist-imports` at `fed653223`.
- Backend/version: no live backend exercised. Unit tests only, with
  the in-repo fixtures; no NornicDB, Postgres, or Docker dependency
  in these packages. Toolchain `go1.26.2 linux/amd64` both sides.
  Wall times are same-machine relative readings, not reference
  targets.
- Input shape: `go test -count=1 ./internal/query/entity/
  ./internal/graph/capture/ ./internal/queryplan/`, default tags.
- Baseline measurement: entity ok 0.238s, capture ok 0.055s,
  queryplan ok 2.520s.
- After measurement: entity ok 0.077s, capture ok 0.042s,
  queryplan ok 1.394s. Same suites, same band or faster; the after
  tree additionally runs the new builder-pin tests.
- Terminal counts: 3/3 packages green on both trees. Zero failures,
  zero flakes across the runs above.
- Query-text proof: the non-test Go diff contains zero lines
  matching MATCH/MERGE/UNWIND/DETACH DELETE/CREATE — no Cypher
  literal is added, removed, or altered. The new test file's
  literals come from the production builders themselves and are
  asserted equal to them, so no query text can drift. No Cypher
  reaches a backend differently.
- Telemetry/log/status evidence: a case-insensitive added-line scan
  of the non-test Go diff for
  metric/counter/histogram/gauge/tracer/otel/prometheus/pprof
  identifiers returns zero matches. No signal added, removed,
  renamed, or relabeled; `go vet` on the touched packages is clean.
- Why the change is safe: the export is additive — the method body
  is untouched, the compiler type-checks the rename at all three
  call sites, and the hot-Cypher manifest gate re-verifies the
  re-derived digest (it failed with the exact expected mismatch
  before the re-derivation and passes after). The only behavior
  delta relative to the base is the intended test coverage: pins
  that fail on stale allowlist text.

## No-Observability-Change (#7417):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
diff adds no telemetry identifiers (scan above), and the exported
builder is a pure statement-table accessor with no logging path, so
existing dashboards and alerts are unaffected.
