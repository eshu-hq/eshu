# #10878 change signals: no-regression evidence

Pure-Go, additive-only change: `go/internal/reducer/contract/change_signal.go`
(new, 284 lines) plus `change_signal_test.go` (new, 186 lines). No existing
file changed behaviorally; no SQL, Cypher, queue, lease, claim, or handler
call path touched; no production caller reads the new helpers yet.

No-Regression Evidence: baseline is the pre-change contract package suite
(green on base 10222a5892; the only RED observed was the new tests failing
to build before the implementation existed, i.e. the TDD failing-first
step). After: `go test ./internal/reducer/contract/ -count=1` PASS
(7 new tests, whole package green), `go vet ./...` clean, `gofmt` clean,
under Go 1.26.11 linux/arm64. Input shape is in-memory unit inputs
(ARN-to-hash maps up to 1000 entries, dirty-key sets, fingerprint strings);
terminal queue/row counts are zero by construction — the helpers emit no
I/O. Safe because additive-only: reverting is deleting two unreferenced
files.

No-Observability-Change: the helpers register no instrument, open no span,
and emit no log; owning passes stay covered by
`eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, and future reopen skips will use
the existing `eshu_dp_correlation_reopen_skipped_total{reason=input_unchanged}`
counter (row added to `telemetry-coverage-reducer-core.md`). Per-run
`changed_keys`, `skipped_no_change`, `fallback_full`, and
`audit_corrections` counters arrive with the handler wiring, not here.
