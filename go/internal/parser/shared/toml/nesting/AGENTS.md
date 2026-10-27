# AGENTS.md - internal/parser/shared/toml/nesting guidance

## Read First

1. `README.md` - what depth means and why the guard exists
2. `check.go` - `Check`, the depth state machine, and the error values
3. `lexer.go` - comment and string skipping
4. `keywork.go` - `Bounds`, `CheckBounds`, `ErrTooManyKeys`, and the work
   accounting
5. `check_test.go`, `generator_test.go`, `bounds_test.go` - exact depth
   cases, the seeded generator oracle, and the allocation ceilings;
   `keywork_test.go`, `keywork_oracle_test.go` - hostile tables, exact work
   totals, and the go-toml oracle

## Invariants This Package Enforces

- The lexer must agree with the go-toml v2.3.1 scanner on every document
  go-toml accepts. A string boundary it misreads can hide brackets and let a
  hostile file through; the first Gradle copy of this guard had exactly that
  bypass (`["""x"""",` on one line).
- Depth counts arrays, inline tables, and dotted-key dots, and lines under a
  table header start at the header's depth. go-toml recurses on all of them.
- The pass is linear and allocates only a frame stack capped at `limit+1`,
  plus one 8-byte hash per distinct table header (capped by the key-work
  limit).
- Key work is a running upper bound of the decoder's tracker steps: plain
  entries charge their segments times the scope so far plus distinct headers,
  inline entries charge against their own table, every header repeat is
  charged, and the only exemption is plain entries under a single-segment
  header naming no table in `Bounds.KnownTables`, because go-toml never
  enters such a table for a struct target (#8991). Unknown keys in known
  tables are charged in full: the decoder tracker-checks them and clears its
  skip per key, so no field-count or first-unknown-key skip may be
  reintroduced (PR #9270 P0). It may over-count and must never under-count:
  `TestKeyWorkNeverUndercountsTheDecoder` and
  `TestStructShapeWorkCoversTheDecoder` hold the work at or above the steps of
  a step-counting copy of the go-toml tracker.
- A zero `Bounds` field allows nothing; never read zero as unlimited.
- An unterminated string is rejected, never guessed.

## Common Changes And Scope

- Before changing the lexer, read the go-toml scanner for the pinned version
  (`unstable/scanner.go`) and add a case to `depthCases` that go-toml accepts.
- After any change, run the generator at depth:
  `ESHU_NESTING_GENERATOR_CASES=30000 go test -run TestScanAgreesWithGeneratorOracle ./internal/parser/shared/toml/nesting/`.
- When go-toml is upgraded, re-check its recursion and string rules; a new
  recursive construct needs a depth rule here.
- Mutation-check key-work changes the same way: dropping the `=` count, the
  header count, the header-text dedupe, the repeat charge, the per-header
  scope reset, the unknown-table exemption, or the end-of-file header must
  each fail a test. The same applies to widening the oracle's skip in
  `trackerSteps` beyond unknown-table scopes.
- Mutation-check a lexer change with `go test -overlay`: dropping the comment
  skip, the multi-line handling, the extra-quote run, escapes, or the header
  reset must each fail a test.

## Anti-Patterns

- Counting brackets without skipping strings and comments.
- Treating a size cap as a substitute: about 0.9 MB is enough to crash, and
  real Cargo.lock files are larger.
- Swallowing `ErrTooDeep` in a caller without saying why the file is skipped.
