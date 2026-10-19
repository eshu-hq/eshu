# Recovery fixed error messages (#8394)

## Contract

The replay, replay-collector-generations, and refinalize admin routes
(`go/internal/runtime/recovery_handler.go`) wrote the raw store error into
422 bodies, quoting SQL, hosts, and constraint names from an unauthenticated
admin-mux surface. Each route now pre-validates its filter (a 422 keeps the
caller-actionable validation text) and answers a store failure with one fixed
JSON message and 500 through `writeRecoveryFailure`: the cause goes to the
request span and the server log, mirroring `status.WriteReadFailure`. A caller
cancel answers the same fixed message with 499, not a server fault. Refinalize
claim-fence contention keeps its 409 blocked/claim_fence_busy verdict,
matching /api/v0/admin/refinalize. Per-site regression tests pin the fixed
bodies (no store text), the kept validation text, the 499 cancel contract
(plus the inner-cancel fault case), and the 409 verdict.

## Performance and observability evidence

The `perf-evidence` gate selects this change because
`go/internal/runtime/recovery_handler.go` sits under its hot-file location.
The touch is error-body text plus failure-path observability on three admin
routes, so this note records no-regression plus observability rather than a
benchmark shaped like proof.

No-Regression Evidence: the diff adds three fixed string constants, one
`filter.Validate()` call per route, one `errors.Is` branch for the existing
`recovery.ErrRefinalizeClaimFenceBusy` sentinel, and one shared
`writeRecoveryFailure` helper. `Validate` is a pure local struct check (stage
allowlist, scope-ids presence, all-scopes exclusivity) with no I/O. The helper
runs only on the store-failure path: one span `RecordError`/`SetStatus`, one
`slog.ErrorContext`, one fixed-body write. It performs no Cypher, opens no
connection, emits no SQL, spawns no goroutine, touches no channel, mutex,
batch size, worker claim, lease, queue, or retry budget, and changes no
success-path or steady-state instruction: every success and validation-reject
path compiles and runs the same code as before, with validation now checked
before the store call instead of inside it.

Baseline and after are the same program on all success and steady-state
paths. The input shape is synthetic and credential-free: `httptest` requests
against a stub handler returning fixed store errors, canceled contexts, and
the claim-fence sentinel, exercised by the `TestRecoveryHandler*`,
`TestRefinalize*`, and `TestReplay*` unit tests. `go test
./internal/runtime/ -count=1` PASS (6.207s); the 19 recovery-focused tests
pass standalone and under `-race` (1.043s); `go vet` on the touched packages
(`internal/runtime`, `internal/recovery`, `internal/status`) exits 0. No live
backend is a variable here because no backend is touched (no Neo4j/Postgres
version applies); there is no terminal queue depth or row count for this
change to move. A before/after latency measurement would be measuring
identical instructions on the throughput-sensitive paths, so none is
recorded.

Observability Evidence: `TestRecoveryHandlerReplayStoreErrorAnswersFixedMessage`,
`TestRecoveryHandlerReplayCollectorGenerationsStoreErrorAnswersFixedMessage`,
and `TestRecoveryHandlerRefinalizeStoreErrorAnswersFixedMessage` prove the
store error text never reaches a response body; `TestRecoveryHandlerClientCancelAnswers499`
(three routes) plus `TestRecoveryHandlerInnerCancelOnLiveRequestIsAFault`
prove the 499 cancel contract and the inner-cancel fault boundary; and
`TestRecoveryHandlerRefinalizeClaimFenceBusyAnswersConflict` pins the 409
verdict. Server-side, a fault records the cause on the request span
(exception event and Error status with the fixed message) and logs it with
path and error; a cancel adds only the `eshu.request.client_canceled` span
event with no error record or log. No production metric, span name, status
field, or telemetry label is added, removed, or renamed.

Why it is safe: failure-path-only helper, closed fixed-message set with no
cause-text formatting, redaction and cancel/fault boundaries pinned by unit
test, full package suite and vet green.
