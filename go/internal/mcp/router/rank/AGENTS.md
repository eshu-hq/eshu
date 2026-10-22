# AGENTS.md - MCP router lexical ranker guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../README.md` and `../AGENTS.md` for the `Ranker`, `Query`, and `Candidate`
   contract this package implements.
3. Epic #8752 for the decisions this ranker encodes.

## Invariants

- Rank must be deterministic. No map iteration order, clock, randomness, or
  goroutine scheduling may reach the output. Sort with a total order that ends in
  registry position. `TestRankDeterministicOverRuns` runs each query 100 times.
- Call `allowed` for every tool before any scoring, and compute document
  frequencies and average length over the allowed tools only. A hidden tool must
  never appear or shift a score.
- A nil `allowed` returns `ErrNilAllowed`. Never treat nil as "allow all".
- Tie-break order is `Canonical`, then cheaper `Cost`, then name. A tool with no
  catalog entry has `CostHigh`.
- Anchors only boost tools whose lexical score is already above zero.
- Build the index in `New`. `Rank` must not rebuild it for the same inputs;
  `TestRankBuildsIndexOnce` checks the build counter.
- Never import `internal/mcp` (the root). `TestRankDoesNotImportMCPRoot` fails
  on an import.
- Keep `maxIntentBytes` and `maxQueryTerms` in force: the intent is caller
  text and must not make `Rank` do unbounded work.
- Boilerplate stripping applies to descriptions only, never names or intents.
- Never change `router.Query`, `router.Candidate`, or `router.Ranker` here.
  Comment on epic #8752 first.

## Common changes

- Retune a field weight, the anchor boost, or a BM25 constant only with an #8756
  eval run, and keep the unit tests that pin weight order (intent and name above
  description).
- Add an anchor: extend `anchorPairs` in `lexical.go` and add a test in
  `TestRankAnchorsPreferToolsAcceptingThem`'s style.
- Change boilerplate thresholds in `boilerplate.go`. Check the effect on the real
  registry, not only fixtures.
- Keep `synthetic_test.go` at 163 tools so the benchmark matches the registry
  size named in #8755.

## Failure modes

- Computing IDF over the whole registry instead of the allowed tools leaks
  hidden tools into visible scores.
- A fixture of 12 or more tools with one repeated sentence is stripped as
  boilerplate, so give each tool a unique word in tests.
- Mutating a tool definition or the entries map after `New` leaves a stale index
  that `Rank` reuses.
- Comparing raw floats for ties lets noise beat the Canonical, Cost, and name
  rules.

## Verification

From `go/`, run `go test -race ./internal/mcp/router/... -count=1`,
`go vet ./internal/mcp/router/...`, and the benchmark with `-benchmem`. From the
repository root, run `scripts/verify-package-docs.sh`.
