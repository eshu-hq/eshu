# #6694 edge leaf: cypher/edge/{writer,materialized} — no-regression evidence

The `edge_writer` (16 files) and `materialized_edge` (3 files) families move
from `go/internal/storage/cypher` into `edge/writer` and
`edge/materialized`. The shared seam (statement templates, row builders,
payload accessors, batching helpers) is exported in place; the leaves import
the parent package. No statement text, predicate, batching, retry, or
telemetry behavior changes.

## No-Regression Evidence (#6694):

- Baseline: `origin/main` at branch creation; after: branch HEAD.
  Backend/version: NornicDB/Neo4j Cypher text compared statically; live
  backend lanes run in CI (Ifá/Odù cells).
- Cypher proof: every backtick literal extracted from each renamed file
  (`origin/main` path vs new path, sorted) compares byte-identical, and
  the same extraction over every modified root file shows diffs only in
  Go identifier renames inside string-concatenation expressions — the
  template text and label-slice values are unchanged, so every emitted
  statement evaluates to the same string as before.
- Tests: `go test ./internal/storage/cypher/...` green (root, writer,
  materialized, fault/executor), plus whole-repo `go vet ./...` clean.
  The narrowing-call-site guard, the property-keyed MERGE inventory, and
  the registry-reason citation guard all pass unweakened; the narrowing
  floor was recalibrated 50 to 10 for the smaller leaf package.
- Input shape: unchanged — same reducer projection rows in, same
  `Statement` batches out through the same `Executor` seam.
- Why safe: the diff is package clauses, import qualifications
  (`sourcecypher`/`edgewriter`/`materialized` aliases), exported-identifier
  renames, and doc trios. No predicate, ordering, batch-size, retry, or
  lease logic was touched.

## No-Observability-Change:

- Instrument names, labels, and emission sites are unchanged
  (`eshu_dp_shared_edge_*`); only the file paths holding them moved.
- `telemetry_test.go` and the root instrumented-executor
  tests assert the same instruments and pass in both packages.
