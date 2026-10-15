# testutil/failure — agent instructions

## Read first

- `doc.go` and `README.md` — the API and the assertion table.
- `../AGENTS.md` — the invariants every `testutil` package follows.
- `../../tracing/AGENTS.md` — the server-failure contract this harness checks.

## Rules

- A route converted to `tracing.WriteServerFailure` proves it with
  `failure.Run`, not a new inline copy of the canary / fence / cancel harness.
  Package-specific extras (a 400 or 404 sentinel, a stage log, a configuration
  503) stay in the route's own test and use `Serve` and `Message`.
- Never weaken an assertion in `check.go` to make a route pass. If a route
  legitimately answers differently (a 504 budget, a write path with no reader
  fence), keep its own harness and note why, or add an explicit option here
  with a sample route in `check_test.go` that proves the option still rejects
  the wrong answer.
- Every condition `Check` tests is an `Assertion` constant listed in
  `Assertions()`, evaluated on its own (never folded into another's `if`), and
  has exactly one wrong sample route in `samples_test.go` that violates it and
  nothing else. `TestEveryAssertionHasOneWrongSample` fails when the two lists
  drift; `TestCheckCatchesEachWrongSampleOnItsAssertionOnly` fails when an
  assertion is weakened (its sample violates nothing) or a sample is wrong in
  two ways. Adding a condition means adding its constant and its sample.
- The `Canary` tokens must stay distinctive. A token a real fixed message
  could contain makes the leak scan fail a correct route.

## Verification

From `go/`: `go test ./internal/query/testutil/failure/ -count=1`. Prove a
change to `check.go` by mutation: switch the assertion off and confirm
`TestCheckCatchesEachWrongSampleOnItsAssertionOnly` names the sample that now
violates nothing. Use `go test -overlay` with a mutated copy of `check.go`, so
the tracked file is never edited. `TestEveryReportedAssertionIsListed` reads
source from disk and cannot see an overlay: to mutate against it, commit,
edit the file, run, and `git restore` it.
