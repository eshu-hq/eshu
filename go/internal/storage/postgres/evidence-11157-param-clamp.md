# Keyed winners chunk parameter clamp (#11157)

Follow-up of #10839 (closes deferred F1: the batch had no upper bound
against the Postgres 65,535-parameter ceiling).

No-Regression Evidence: failing-first test proves the clamp fires before any
SQL is built (13,108 identifiers into a 13,107-row ceiling returns an explicit
bound error with zero identifier INSERT Execs; pre-fix the same input built an
over-limit statement with nil error). Default-bound behavior is unchanged:
the saturated-200 batch test proves exactly 1 INSERT, and the
single-identifier and empty-window tests pass unmodified. Backend: unit
fake-tx (go test ./internal/storage/postgres). Input shape: 13,108
five-column identifiers (65,540 params) against the 13,107-row ceiling; the
guard precedes both SQL construction and slice allocation, so no partial
statement can reach the server.

Observability Evidence: no new telemetry; over-limit batches surface through
the existing `record supply chain impact winners chunk identifiers`
tick-error wrap, which operators already watch. Default-bound ticks emit
nothing new.
