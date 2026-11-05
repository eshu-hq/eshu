# 10167 shard-p2 followup: no-regression evidence

Rebase followup for `lane3/10167-shard-p2-followup` onto
`origin/main` (`5037807422`). The rebased commit keeps the four P2 Go
files byte-identical to the pre-rebase commit and drops the six
generated readiness files: re-deriving them on the merged tree with the
merged `SHARD_BOUNDARIES` reproduces `origin/main` byte-for-byte, so
the branch's shard-split halves were already landed there by
`#10160`/`#10161` and only the P2 fixes remain.

Scope of the rebased diff (`f10dd309bc`, new-base `5037807422`):

- `go/internal/storage/postgres/delta_overlay_refusal.go`: rewords the
  `BaselineCommitSHA` field doc comment (states `warnDeltaOverlay`
  carries it in the refusal WARN log; never in the error string or a
  label). No code, log-call, or carrier change.
- `go/internal/storage/postgres/facts_active_crossplane_xrd.go`: rewords
  the placeholder doc comment above the delta-overlay scope params. No
  SQL text change.
- `go/internal/telemetry/instruments.go`: rewords the
  `CrossplaneSatisfiedByReadDeltaUnion` doc comment to state the exact
  per-outcome behavior (fail closed with
  `CrossplaneDeltaBaseRefusedError` or the resolution error vs return
  delta rows only). Counter name, `read`/`outcome` labels, and
  cardinality unchanged.
- `go/internal/storage/postgres/satisfied_by_read_index_live_test.go`:
  test-only; the `base` EXPLAIN arm now binds candidate base generation
  ids (`deltaOverlayBaseGenerations`, `-full` suffixed) instead of scope
  ids, matching what production binds (`#9611`).

The perf-evidence gate names `delta_overlay_refusal.go` and
`instruments.go` as hot files because their doc comments are
tab-indented `//` lines, which its classifier reads as code changes.
The `facts_active_crossplane_xrd.go` hunk is column-zero `//` lines and
the test file is excluded, so neither is hot.

No-Regression Evidence: hermetic/static proof on the rebased tree
(`GOTOOLCHAIN=go1.26.10`, `CGO_CFLAGS="-O2 -g -std=gnu17"`,
`GOTMPDIR=$HOME/.cache/lane3/gotmp`):

- `go build ./...` (from `go/`): exit 0.
- `go vet` on `go/internal/storage/postgres` and
  `go/internal/telemetry`: exit 0.
- `python3 scripts/generate-live-postgres-readiness.py --check`: exit 0,
  `registration current (42 surfaces)`.
- `bash scripts/test-generate-live-postgres-readiness.sh`: exit 0,
  GREEN (385 exact proof rows, four capped fragments, idempotent,
  stable shards, all RED arms fire).
- Focused Crossplane satisfied-by unit tests, `-count=1`: exit 0, 5/5
  PASS (`TestCrossplaneXRDIndexPredicateMatchesTheReader`,
  `TestCrossplaneReaderTextsDeriveDeltaOverlayBase`,
  `TestListActiveCrossplaneSatisfiedByFactsStatements`,
  `TestListActiveCrossplaneSatisfiedByFactsFailsOnAMissingBase`,
  `TestListActiveCrossplaneSatisfiedByFactsRetriesAFailedProbe`;
  package time 0.290s).
- Reshard identity: union of `part1..part4` enrollment keys is 216/216
  against the pre-shard `part1+part2` union at `3464f63236`, with 0
  missing, 0 extra, and 0 per-key test-tuple mismatches.
- `bash scripts/verify-doc-citations.sh`: exit 0 (292 test, 340
  fixture, 256 raw line citations).
- `gofmt -l` on the four files: clean; `git diff --check`: clean.
- No live backend was exercised: input shape is the static source tree
  (42 readiness surfaces, 385 proof rows, 216 enrollment keys); no
  Postgres rows were read or written by this proof. The live EXPLAIN
  test above is covered by registration, not executed here.

No-Observability-Change: no metric, label, log-emit, span, or status
surface changes. `CrossplaneSatisfiedByReadDeltaUnion` keeps its name,
labels, and cardinality; the WARN log call and its fields are
untouched; the refusal error type keeps its fields and string form.
The only production-adjacent edits are doc comments, so operator
dashboards, alerts, and `telemetry/` scrapes observe identical series.

Why this is safe: the two gate-named hunks are comment-only with zero
runtime effect (build, vet, and the five focused tests confirm); the
test hunk binds the EXPLAIN to the generation ids production binds, so
the partial-index proof cannot pass on unrepresentative args; and the
reshard moves entries between shards without adding, dropping, or
retargeting any enrollment (216/216, 0 mismatches, byte-identical
regeneration against merged `main`).
