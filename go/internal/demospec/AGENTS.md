# AGENTS — demospec

Scoped rules for editing the demo-first-answers manifest loader. Load
`eshu-golden-corpus-rigor` and `eshu-mcp-call-rigor` before touching this
package or `specs/demo-first-answers.v1.yaml` — the manifest's referential
integrity depends on both the golden-corpus cassette/fixture contract and the
MCP/CLI/HTTP query-shape registry those skills own. Add `golang-engineering`
for any Go edit.

## Invariants

- **Exactly five questions.** `LoadManifest` hard-fails if the manifest does
  not declare exactly five `Question` entries. This is an acceptance
  criterion from issue #4741, not a soft default — do not relax it.
- **No new query capability.** Every `surface.kind`/`surface.ref` in the
  manifest MUST resolve to an existing playbook, MCP tool, CLI verb, or HTTP
  route the golden-corpus gate already proves is live. Do not add a manifest
  entry pointing at a surface that does not exist yet; land the surface first
  (in its owning package), prove it in the golden snapshot, then reference it
  here.
- **Playbook surfaces need an `execute` target.** A playbook id is not directly
  callable, so a `kind: playbook` question MUST carry a `surface.execute`
  (`kind: mcp|http`, `ref`, `arguments`) naming the underlying tool/route the
  demo-answers golden-gate phase (issue #4776) invokes to fetch the live
  answer. `LoadManifest` hard-fails a playbook surface with no execute. The
  execute `ref` is referential-integrity-checked against the golden snapshot
  query shapes, same as a surface ref.
- **`minimum_results` reflects the live answer, not an aspiration.** Set it to
  the floor the answer's first result array actually meets on the deterministic
  corpus (captured from a golden-gate run), or `0` for an object-shaped answer
  with no result array. The demo-answers phase asserts it live, so a wrong
  floor is a false red; when the corpus legitimately changes the count, update
  the floor under review.
- **Existence, not greenness.** `manifest_test.go`'s `TestDemoFirstAnswers`
  proves referenced cassette files, fixture directories, playbook IDs, and
  query-shape keys exist. It does not run the pipeline or hit a live backend.
  Golden-gate-live validation (the actual JSON responses) is a manual,
  documented step recorded in the PR that introduces or changes a question,
  not something this package's tests do automatically — do not add a
  Docker-Compose dependency to this test suite.
- **Failing-test-first for any manifest change.** Before changing a
  question's `artifacts` or `surface` block, prove the test catches a broken
  reference (temporarily point at a nonexistent cassette/repo/ref, confirm the
  specific assertion fails, then land the real change). This mirrors the
  #4741 acceptance criterion and keeps the oracle honest.
- **HTTP refs match method + path prefix.** The golden snapshot's HTTP
  query-shape keys embed a specific querystring
  (e.g. `?provider=tempo&limit=50`); a manifest HTTP ref is considered
  resolved if it shares method and path with any snapshot key, regardless of
  querystring. Do not require an exact string match — that would make the
  manifest brittle to changes in the gate's chosen query parameters (limit
  values, ordering) that do not change the underlying route.
- **`go.mod` boundary.** `specs/` sits outside `go/`, so this package cannot
  `go:embed` the manifest. `LoadManifest` takes an explicit path; the test
  suite resolves the repository root via `runtime.Caller` walking upward
  (mirrors `moduleRoot` in
  `go/internal/graph/edgetype/coverage_schema_test.go`, extended one level up
  past `go.mod` since the target lives above it).

## When the manifest changes

Adding, removing, or repointing a question requires:

1. A failing-test-first proof (see above).
2. Referential integrity green (`go test ./internal/demospec -count=1`).
3. A live golden-corpus-gate run capturing the actual JSON response for the
   new/changed question, pasted into the PR — the manifest's
   `expected_answer` fields should reflect what the live gate returns, not a
   guess.
4. `specs/README.md` kept in sync if the manifest's purpose or design
   changes materially.

## Tests

`cd go && go test ./internal/demospec/... -count=1 -v`. The `correlation_coverage`
subtest asserts the union of every question's `demonstrates_correlations` is
non-empty and every ID exists in
`testdata/golden/e2e-20repo-snapshot.json`'s `graph.required_correlations` —
do not remove it when adding or editing questions.
