# AGENTS.md - internal/parser/shared/toml/nesting guidance

## Read First

1. `README.md` - what depth means and why the guard exists
2. `check.go` - `Check`, the depth state machine, and the error values
3. `lexer.go` - comment and string skipping
4. `check_test.go`, `generator_test.go`, `bounds_test.go` - exact depth
   cases, the seeded generator oracle, and the allocation ceilings

## Invariants This Package Enforces

- The lexer must agree with the go-toml v2.3.1 scanner on every document
  go-toml accepts. A string boundary it misreads can hide brackets and let a
  hostile file through; the first Gradle copy of this guard had exactly that
  bypass (`["""x"""",` on one line).
- Depth counts arrays, inline tables, and dotted-key dots, and lines under a
  table header start at the header's depth. go-toml recurses on all of them.
- The pass is linear and allocates only a frame stack capped at `limit+1`.
- An unterminated string is rejected, never guessed.

## Common Changes And Scope

- Before changing the lexer, read the go-toml scanner for the pinned version
  (`unstable/scanner.go`) and add a case to `depthCases` that go-toml accepts.
- After any change, run the generator at depth:
  `ESHU_NESTING_GENERATOR_CASES=30000 go test -run TestScanAgreesWithGeneratorOracle ./internal/parser/shared/toml/nesting/`.
- When go-toml is upgraded, re-check its recursion and string rules; a new
  recursive construct needs a depth rule here.
- Mutation-check a lexer change with `go test -overlay`: dropping the comment
  skip, the multi-line handling, the extra-quote run, escapes, or the header
  reset must each fail a test.

## Anti-Patterns

- Counting brackets without skipping strings and comments.
- Treating a size cap as a substitute: about 0.9 MB is enough to crash, and
  real Cargo.lock files are larger.
- Swallowing `ErrTooDeep` in a caller without saying why the file is skipped.
