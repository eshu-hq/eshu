# Zero-live-file guard evidence (eshu-hq/eshu#8657)

`rejectZeroLiveFileWipe` runs once per `BuildMaterialization`, after the
extractor families complete and before the final return. Common builds exit
through three cheap checks (first-generation/delta test, `len(mat.Files) >
0`, repository-scope test); the fact scan runs only when the materialization
carries zero File rows on a wipe-capable non-first, non-delta build, the
shape that previously wiped prior File nodes and reported success.

## Measurements

No-Regression Evidence: the guard adds a constant-time check to a normal
build; the input scan runs only on the fail-loud path that replaces a silent
graph wipe.

- Fast path (normal build, `Files` non-empty): three comparisons, no
  allocation, dwarfed by the surrounding extraction work.
- Scan path (zero File rows, non-first, non-delta, repository-scoped): one
  `FilterFileFacts` pass over the input envelopes; runs only when the build
  is about to fail loudly instead of wiping the graph.
- Terminal counts: `go test ./internal/projector/...` ok (exit 0);
  `./internal/query/codequery/imports/...` and `./internal/storage/cypher/...`
  ok (exit 0); canonical package ok under `-race` (exit 0); `go vet` and
  `gofmt` clean.

## Correlation truth

Positive case (repository-only and repository-plus-entities inputs with zero
file facts fail loud naming generation and scope), negative cases (first
generation, delta, all-tombstone mass delete, and empty input still succeed),
and the ambiguous case (entity rows without file evidence still fail loud)
are pinned by `builder_zero_live_guard_test.go` (5 tests, RED first; a sixth
all-tombstone test was dropped as a duplicate of the #8660-merged identical
test). No
fixture truth changed, so no compose re-verification applies. The graph
after the guard matches input intent: file evidence present means files
project; explicit tombstone deletes mean an empty graph; no file evidence at
all means no projection instead of an unexplained wipe.

## Empty-scope contract

No empty-scope marker exists in the codebase: no scope, generation, or fact
field declares an intentionally fileless generation. Until such a marker is
defined (a collector-to-projector contract outside this lane), a non-first,
non-delta generation that carries zero file facts of any kind fails loudly
instead of succeeding; the error names the generation, scope, and the
zero-file-facts shape so the operator investigates the missing file input.
An all-tombstone generation keeps succeeding as an explicit mass delete.
Reconciliation replays that carry the stored generation's file facts are
unaffected; a fileless replay of a file-bearing generation fails at the same
gate.

## Phase rows

File-phase rows still derive from the repository fact alone
(`canonicalGraphPhaseStates` in `runtime/phase.go` publishes a
`code_entities_uid` committed row from the repository fact). That limitation
is unchanged; the guard prevents the fileless shape from reaching
publication, because the `BuildMaterialization` error aborts
`buildProjection` before any writer call or phase publish.

## Observability

No-Observability-Change: the guard emits no log, metric, or span. Failure
surfaces as a `BuildMaterialization` error through the existing
projection-failure path (`buildProjection` wraps it, the service fails the
work item loudly), whose callers treat a non-nil error as authoritative. No
new operator signal was added.
