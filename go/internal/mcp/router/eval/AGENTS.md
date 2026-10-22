# AGENTS.md - MCP router eval guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../README.md` and `../AGENTS.md` for the `Ranker`, `Query`, and `Candidate`
   contract this package scores.
3. Epic #8752 and issue #8756 for the decisions this package encodes.

## Invariants

- Write corpus questions blind to the curated intents. Do not open
  `go/internal/ask/catalog/annotations_intents_*.go` before writing or editing a
  question. A corpus derived from the intents scores them against themselves.
- Never tune the corpus to a ranker. Change a question only when its expected
  tool is wrong or ambiguous, and add an alternate for a genuinely ambiguous one.
  Moving a question to make a number rise defeats the measurement.
- Accuracy is report-only. `TestRoutingAccuracyReport` must never fail on a
  top-1 or top-5 number. Blocking thresholds need a comment on epic #8752 with at
  least five recorded runs on `main`.
- `Validate` stays the blocking part of the gate: unknown family, unknown source,
  unregistered tool, duplicate id or question text, and the size floors.
- Never put an employer, customer, or private repository name in the corpus.
- Never import `internal/mcp` (the root) from non-test code in this package.
  The registry-backed tests belong in `eval_test`.
- Keep `Run` and `Format` deterministic. No map iteration order may reach output.

## Common changes

- Add a question: append it to `testdata/corpus.json` with a new unique `id`, a
  known `family` and `source`, and a registered `expected` tool. Run the package
  tests; `TestCorpusMatchesRegistry` names every problem.
- Add a family: extend `knownFamilies` in `corpus.go`, the README table, and
  `DefaultRequirements` only if the epic's family table grew.
- A new MCP tool needs no corpus change unless you want it measured. The distinct
  tool floor is 40, not the full registry.

## Failure modes

- A tool rename leaves a stale `expected` name; `Validate` fails and names it.
- Passing a ranker a different `tools` slice than it was built from forces the
  lexical ranker to rebuild its index per call, which slows the run without
  changing results.

## Verification

From `go/`, run `go test ./internal/mcp/router/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh` and `scripts/verify-ci-gates-registry.sh`.
