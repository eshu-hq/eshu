# AGENTS.md — storage/postgres/boundederr guidance

## Read first

1. `go/internal/storage/postgres/boundederr/doc.go` -- the contract and why the
   bound sits at the driver seam.
2. `go/internal/storage/postgres/boundederr/README.md` -- surface and limits.
3. `docs/internal/evidence/7253-postgres-store-error-public-text.md` -- the
   theory proof, the before/after, and the residual sites.

## Invariants

- The client-facing text of an `*Error` is one fixed string per `Kind`. Never
  put driver text, a SQLSTATE, a relation name, or a bound value in it.
- `Unwrap` must keep returning the driver's error. Callers classify with
  `errors.Is` and `errors.As`; a wrapper that drops the cause breaks retry,
  timeout, and connection-loss handling.
- Classify from the error's type and SQLSTATE, never from `err.Error()`. Once the
  text is bounded, a text match cannot see the driver message.
- `io.EOF`, `driver.ErrBadConn`, `driver.ErrSkip`, and `driver.ErrRemoveArgument`
  pass through unchanged, matched by equality. An `errors.Is` match would also
  let a wrapped driver error keep its connection target.
- Every optional driver interface pgx implements must stay forwarded;
  `var _ pgxConn = (*stdlib.Conn)(nil)` and the per-type assertions fail to
  compile if a method goes missing. A dropped interface silently changes
  `database/sql` behavior (context-aware statements, session reset, ping).
- `postgres.store.error` is an operator contract: keep its name and keys stable
  and document changes in `docs/public/reference/telemetry/logs.md`. A canceled
  request logs nothing; a timeout and a class 22 or 23 error log at WARN.
- Do not use a wrapped pool for `CopyFrom`: the connection is not a
  `*stdlib.Conn`.
