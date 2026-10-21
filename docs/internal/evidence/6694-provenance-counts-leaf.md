# #6694 provenance-counts leaf move evidence

## Moved (behavior-preserving, `git mv` + package/import edits only)

The provenance count gauges move from the `storage/cypher` root into the
existing `edge/writer` leaf (`package writer`, external alias `edgewriter`),
next to the provenance edge writer whose `eshu_dp_edges_by_source_tool` and
eshu_dp_files_by_language` gauges they serve:

- `provenance_counts.go` (implementation diff is the package clause only)
- `provenance_counts_test.go` (package clause only)
- `provenance_counts_tiebreak_live_test.go` (package clause,
  `sourcecypher` import → `edgewriter`, run-path comments)

Repoints (direct caller migration, no root facade, per the #6694 lane
precedent):

- `go/cmd/reducer/provenance_gauge_wiring.go`: `sourcecypher.` import and
  `NewProvenanceCountStore` call → `edgewriter` (only production caller).
- `go/internal/queryplan/testdata/statement-builders.yaml`: two
  read-exemption callsites → new path (anchors and reasons untouched).
- `specs/live-tests.d/live-tests-04.yaml`: tiebreak live-test selector →
  new path.
- `sweep/orphan` twin comments (`AGENTS.md`, `queries.go`): the `int64Count`
  twin now lives in the edge-writer leaf; the copies stay behavior-identical
  because the orphan leaf imports the parent and cannot share one definition.
- `scripts/moved-file-refs-allowlist.txt`: the dated #7711 dispositions note
  keeps its old path (repointing would falsify the record).

No-cycle proof: the moved files import stdlib only (`context`, `fmt`,
`math`, plus test deps). No root non-test file imports `edge/writer`, and
after the move no root file references a moved symbol (`git grep` clean).
The leaf already imports the parent for other files; this move adds no new
edge in either direction.

## No-Regression Evidence (move-only, statement-identical):

Baseline: pre-move package at `origin/main 7d6d375081`. All Cypher statement
lines in the three moved files hash identically before and after
(sha256 `7daf3d80` on both sides; the only textual diffs are the package
clauses, the one import swap, and comment run-paths). The queries keep the
same relationship-type-index and File-label anchors, the same `LIMIT`-bounded
`ORDER BY` tiebreaker shape, and the same pinned-backend expectations, so no
benchmark bench is needed for the move itself. After:

- `go build ./internal/storage/cypher/... ./cmd/reducer/ ./internal/queryplan/` — clean.
- `go vet` on the touched trees — clean, including
  `-tags live_nornicdb_answer_truth` on the leaf (build-tag trap closed).
- `go test -count=1 ./internal/storage/cypher/edge/writer/` — ok (moved
  `TestProvenanceCountStore*` tests run and pass).
- `go test -count=1 ./internal/storage/cypher/` — ok (no dangling root refs).
- `go test -count=1 ./internal/queryplan/` — ok.
- `go test -count=1 -run 'TestProvenanceGauges|TestGraphBackedGauges' ./cmd/reducer/` — ok.
- `go test -list` on the leaf discovers every moved test, including the
  tagged `TestLiveProvenanceCountsTiebreakIsDeterministic`.
- `bash scripts/verify-moved-file-refs.sh` — 3 vacated go paths, no dangling
  references, exit 0.

Live-backend execution of the tiebreak proof and the golden-corpus
statement-coverage wave are CI-owned (no live Docker lane ran locally);
both legs are statement-identical to the pre-move runs.

## No-Observability-Change:

Same store type, same gauge names (`edges_by_source_tool`,
`files_by_language`), same snapshot registration and refresh wiring in
`registerProvenanceCoverageGauges`; no metric, span, log line, or knob added
or renamed. The refresh still reads through the graph read port on its own
goroutines, never on the scrape.
