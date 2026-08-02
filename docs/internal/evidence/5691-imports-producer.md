# 5691 — File-[:IMPORTS]->Module gets its first producer

Validation record for issue #5691. The read contract, the canonical writer, the
delta refresh, the retract path and `/code/import-dependencies` were all
complete; nothing populated `CanonicalMaterialization.Imports`. A freshly
indexed stack carried zero `IMPORTS` edges and `symbol_graph.imports` answered
empty with confidence, which is what #5552's deployed proof found.

The parsers have always written a per-file `imports` bucket into
`parsed_file_data`. The projector only matched the Python-runtime-era
`module_name` / `imported_module` fact payloads, which no Go collector emits.

## What the backend forced

The first implementation keyed the `IMPORTS` MERGE on `imported_name`, so that
`import { Router, json } from "express"` would stay two edges. Unit tests
passed. The first live run against the pinned NornicDB
(`eshu-nornicdb-pr261:149245885258`) returned 2 of 3 expected edges.

A probe isolated why: a relationship property map in a MERGE pattern is not part
of relationship identity on that build. Every statement shape collapses —
including a single, non-batched MERGE, so it is not the
`executeUnwindMergeChainBatch` fast path. The full measurement, and the two
other shipped writers it affects, are recorded in
[NornicDB Pitfalls](../../public/reference/nornicdb-pitfalls.md).

The extractor therefore folds every parser entry for one `(file, module)` pair
onto the single edge the backend can hold, carrying `imported_name` / `alias`
only when every folded entry agrees. A two-symbol import reports no symbol
rather than whichever one the batch wrote last.

## Graph truth

Golden corpus gate, run on this branch after the final edit
(`scripts/verify-golden-corpus-gate.sh`, private port set, 30-repo staged
corpus). Every one of these read zero, or was absent, before this change:

| Assertion | Result |
| --- | --- |
| `edge_count_IMPORTS` | 63, snapshot range `[30, 10000]` |
| `node_count_Module` | 72, snapshot range `[20, 5000]` |
| `rc-171` `(File)-[:IMPORTS]->(Module)` | count 63, want >= 30 |
| `rc-171_edge_prop_evidence_source` | 0/63 matching edges offending |
| `mcp:investigate_import_dependencies` | 1 result, object match on the pinned row (source_file, module, alias, line) |

The gate's own floors were vacuous before: `IMPORTS` carried `min: 0`, `Module`
had no node-count row, and the MCP query shape asked for `minimum_results: 0`.
A corpus with no import edges and a tool answering empty was indistinguishable
from a healthy one, which is why this defect survived every gate run.

Backend-required proof:
`go test ./internal/replay/offlinetier -run TestCanonicalImportEdgesGraphTruth`
against the pinned image, covering a first generation and a re-projection.

## Performance Evidence:

The extractor runs once per repository generation inside
`buildCanonicalMaterialization`. Measured with
`BenchmarkExtractImportsFromFiles` and
`BenchmarkBuildCanonicalMaterializationWithImports`
(`go/internal/projector/canonical_import_extract_bench_test.go`), same input
shape both sides: synthesized file facts, 12 imports per file, Apple silicon,
`-benchtime=5x -count=3`, median reported.

The first implementation re-decoded every file fact that
`extractFilesWithQuarantine` had already decoded. That pass now returns the
decoded files and the extractor reads them, so no file fact is decoded twice.
Before and after, on the whole materialization build at 2,000 files:

| Build, 2,000 files | Before | After | Delta |
| --- | --- | --- | --- |
| ns/op | 7,869,375 | 7,768,325 | −1.3% (within noise) |
| B/op | 18,446,856 | 16,271,041 | **−11.8%** |
| allocs/op | 62,275 | 58,265 | **−6.4%** |

Wall time is unchanged because decoding 24,000 import entries dominates either
way; the duplicate decode cost about 1 ms and 7,900 allocations per 2,000 files,
which the allocation counters show removed. Allocation counts are deterministic,
so those two columns are signal rather than timing noise.

Against parse, the stage that feeds it, the extraction is negligible: parsing
the corpus fixtures costs 72 ms and yields 63 distinct `(file, module)` pairs
across 149 files; folding those pairs costs 61 µs, or 0.084% of parse.

Terminal counts on the live corpus: 63 `IMPORTS` edges, 72 `Module` nodes, with
the gate's drain assertions green (`fact_work_items_residual: residual=0`,
`shared_projection_intents_nonterminal: 0`).

Backend/version: `eshu-nornicdb-pr261:149245885258` (the `docker-compose.yaml`
default), Postgres 16.

Why this is safe: the change adds rows to an existing write phase rather than a
new phase or query shape. `canonicalNodeImportEdgeCypher` is unchanged — the
first version's identity change was reverted after the live measurement above —
so the emitted statement, its batching, and its NornicDB fast-path eligibility
are byte-identical to what already shipped. The only new cost is building the
rows, measured above.

## No-Observability-Change:

The extractor appends to the same `CanonicalMaterialization.Imports` slice the
canonical writer has always consumed, so its edges are counted by
`eshu_dp_canonical_writes_total` and timed by
`eshu_dp_canonical_write_duration_seconds`, and the per-generation
`import_count` already appears in the projector's runtime-stage log
(`go/internal/projector/runtime_stages.go`). An operator diagnosing "no import
edges" at 3 AM reads `import_count` on the projector stage log, exactly as for
every other canonical row family. No new metric is warranted for a producer that
feeds an already-instrumented write path; the corresponding
`docs/public/observability/telemetry-coverage.md` row records the same.

## Related

- [NornicDB Pitfalls](../../public/reference/nornicdb-pitfalls.md) — the
  relationship-MERGE identity measurement and the two other affected writers.
- Issues: #5691, from the #5552 deployed proof; parent #5344.
