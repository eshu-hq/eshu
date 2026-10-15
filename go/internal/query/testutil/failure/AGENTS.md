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
- Every assertion in `Check` has a sample route in `check_test.go` that only
  that assertion catches. Adding an assertion means adding its sample; removing
  one means the matching sample starts passing, which the RED test reports.
- The `Canary` tokens must stay distinctive. A token a real fixed message
  could contain makes the leak scan fail a correct route.

## Verification

From `go/`: `go test ./internal/query/testutil/failure/ -count=1`. Prove a
change to `check.go` by mutation: switch the assertion off and confirm
`TestCheckCatchesWrongRoutes` names the sample route that now passes, then
restore.
