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
  edge cases; run both fuzz targets for a while after any change to a `skip*`
  or `*Len` helper:
  `go test ./internal/query/graph/statement -run xxx -fuzz '^FuzzRedact$' -fuzztime 30s`
  and the same with `'^FuzzRedactLeaksNoPlantedSecret$'`.
- Keep the token classes a superset of NornicDB's `RedactLiterals` AND of the
  Neo4j 5 lexer's literal forms (`Cypher5Lexer.g4`: digit separators, the
  trailing `PART_LETTER` characters of a number token, and the `SPACE` set).
  Neo4j is a supported backend and accepts inputs NornicDB's lexer rejects, so
  matching NornicDB alone leaks. If NornicDB adds a redacted class, the
  operator recipe in `docs/public/reference/telemetry/graph-read-safety.md`
  changes with it.
- A new literal form needs a planted-secret row in `redact_test.go` and a case
  in `FuzzRedactLeaksNoPlantedSecret`, which plants fuzzer-chosen secrets in
  literal positions behind ASCII and Unicode whitespace and fails if any
  survives.

## Verification

From `go/`: `go test ./internal/query/graph/statement ./internal/query -count=1`.
