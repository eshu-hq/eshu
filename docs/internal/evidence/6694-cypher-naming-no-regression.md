# #6694 Cypher naming: no-regression evidence

Rename-only move of the fault-injection executor out of the `cypher`
package root: `go/internal/storage/cypher/fault_executor*.go` (10 files,
R073–R095) into `go/internal/storage/cypher/fault/executor/`, plus the
`sourcecypher` → `faultexecutor` qualifier rewire in
`go/cmd/reducer/ifa_fault_wiring.go` and the `db.Rows` readiness-fake
follow-up to #6754. No query text, no control flow, no concurrency
shape, and no telemetry identifiers change.

## No-Regression Evidence (#6694):

- Baseline: `0150a0018` (main after #6754/#6755/#6750/#6751), measured in
  a detached throwaway worktree on the same machine, serially in one
  session (base first, then branch), sharing nothing but the Go build
  cache.
- After: branch `feat/6694-cypher-naming` at `bff8dc5cd`.
- Backend/version: no live backend exercised. Unit tests only, with the
  in-repo fakes; no NornicDB, Postgres, or Docker dependency in these
  packages. Toolchain `go1.27.1 darwin/arm64` both sides. Wall times are
  same-machine relative readings, not reference targets.
- Input shape: `go test -count=1 ./internal/storage/cypher/...`
  `./cmd/reducer ./internal/storage/postgres`, default tags and again
  with `-tags ifafaultinjection` for the cypher tree.
- Baseline measurement: cypher ok 1.674s, reducer ok 1.117s (default);
  cypher ok 1.777s (ifafaultinjection). Postgres BUILD FAILED on the
  base with `status_readiness_test.go:141: undefined: Rows` — main is
  RED at the base commit; this branch carries the fix.
- After measurement: cypher ok 1.620s, fault/executor ok 0.268s,
  reducer ok 1.216s, postgres ok 5.367s (default); cypher ok 1.649s,
  fault/executor ok 0.924s (ifafaultinjection). Same test files, now
  split across the two packages; totals match the baseline order.
- Terminal counts: 6/6 runnable packages green on the branch
  (`cmd/reducer`, `replay/faultreplay`, `storage/cypher`,
  `storage/cypher/fault/executor`, `storage/postgres`; `fault` has no
  test files). Zero failures, zero flakes across the runs above.
- Query-text proof: the set of quoted Cypher literals containing
  MATCH/MERGE/UNWIND/CREATE in added diff lines is byte-identical to
  the set in removed diff lines (`QUERY-STRINGS-IDENTICAL`); the only
  touching lines are Go qualifier rewrites (`Statement` to
  `cypher.Statement`). No Cypher reaches a backend differently.
- Telemetry/log/status evidence: an added-line scan of the whole Go
  diff for metric/counter/histogram/gauge/tracer/otel/prometheus/pprof
  identifiers returns only the English word "counterpart". No signal
  added, removed, renamed, or relabeled; `go vet ./...` clean.
- Why the change is safe: every moved file keeps its logic — git
  rename detection pairs all 9 moves, the compiler type-checks the
  qualifier rewire at every call site, and the relocated tests pass
  with timings in the baseline band. The one behavior delta relative
  to the RED base is the intended fix (readiness fake follows the
  hoisted `db.Rows` contract), which turns the base build failure
  green rather than changing any passing behavior.

## No-Observability-Change (#6694):

No operator-facing signal changes. No metrics, spans, structured-log
fields, status endpoints, or pprof knobs are added or altered — the
diff adds no telemetry identifiers (scan above), and the moved
executor's stderr marker prefix
(`OnceFiredMarkerWriteFailedPrefix`) is unchanged, so existing
fault-injection gate assertions keep matching.
