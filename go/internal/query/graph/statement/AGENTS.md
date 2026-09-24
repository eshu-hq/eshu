# Agent instructions: statement

Read `doc.go` and `README.md` first.

## Invariants

- No literal-derived byte may survive `Redact`. When you add a token form
  (a new number syntax, a new string delimiter), add a table row in
  `redact_test.go` that plants a secret in it and asserts it is gone, before
  the scanner change.
- Fail closed. An unterminated string, block comment, or backtick identifier
  redacts to the end of the statement; never emit the unterminated remainder.
- Keep it single-pass and allocation-light: it runs on every graph read to
  build the fingerprint, not only on the warning path. Re-measure with
  `BenchmarkGraphStatementFingerprint` in `internal/query` after a change.
- The scanner must never panic or index past the input. `FuzzRedact` seeds the
  edge cases; run it for a while after any change to a `skip*` helper:
  `go test ./internal/query/graph/statement -run xxx -fuzz FuzzRedact -fuzztime 30s`.
- Keep the token classes a superset of NornicDB's `RedactLiterals`. If NornicDB
  adds a redacted class, the operator recipe in
  `docs/public/reference/telemetry/graph-read-safety.md` changes with it.

## Verification

From `go/`: `go test ./internal/query/graph/statement ./internal/query -count=1`.
