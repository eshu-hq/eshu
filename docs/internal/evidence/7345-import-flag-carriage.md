# 7345 — IMPORTS edges carry the parser's import flags

Validation record for issue #7345, split from #6851. The parser (#7344) now marks
imports that cannot close a load-time cycle: `type_only`, `deferred`, `inferred`
(present only when true). Nothing carried those flags to the graph, so the
`file_import_cycles` reader (#7346) had no way to use them. This change carries
them onto the `File-[:IMPORTS]->Module` edge. It changes no reader and no API
shape.

## Design (arbiter-reviewed)

The projector folds every parser entry for one `(file, module)` pair into ONE
edge (`importIdentity{filePath, moduleName}`), and the flags are per entry, so
the edge answers "can this file's import of that module close a load-time
cycle?" from all of them.

| Edge flag | Holds when |
| --- | --- |
| `type_only` | every folded entry is type-only |
| `deferred` | every folded entry is deferred or type-only, and the edge is not `type_only` |
| `inferred` | every folded entry is inferred |

Every rule under-claims on disagreement. One entry that really runs at load time
keeps the edge a runtime edge, so folding can never suppress a real cycle.

Plain per-flag AND was the first draft and the arbiter rejected it for
`deferred`. An `if TYPE_CHECKING: import m` beside a function-local `import m`
never runs at load time, but per-flag AND reports neither flag and so claims a
load-time edge that does not exist: a false cycle. Folding `deferred` over
"deferred or type-only" fixes that at no cost. An entry carrying both flags
counts as type-only, so `type_only` and `deferred` are never both true on an edge.

`inferred` is AND, not OR. Every entry folded into one edge names the identical
module string, so one resolved entry proves the target exists; OR would label a
confirmed edge ambiguous.

The writer sets all three properties unconditionally as explicit booleans. The
MERGE names no relationship property, so a re-projected edge is an existing edge,
and only an unconditional SET of an explicit false overwrites a stale true. The
SET reads only the row, never the existing edge.

`Import` in `sdk/go/factschema/codegraph/v1` gains three named optional fields.
Contract classification: minor. `Import` is an inner `parsed_file_data` struct
with no payload contract row and no schema artifact, and an optional field is
additive. The alternative, reading the flags out of `Attributes`, allocates one
map per import entry on every generation to inspect three booleans.

## Premise correction

The issue said the pinned NornicDB treats a relationship property map in MERGE as
non-identity, so per-symbol flags could not be carried. That is stale: the pinned
v1.3.3 build includes property-map MERGE identity (see
[NornicDB Pitfalls](../../public/reference/nornicdb-pitfalls.md)), and Neo4j
always did. It is also beside the point: the edge is keyed on its endpoints by
design, because every consumer reasons about files and modules. The stale comment
in `import_extract.go` and the stale "flags do not reach the graph" comment in
`code_import_dependencies_cycles.go` are corrected in this change.

## Graph truth

`TestCanonicalImportEdgesGraphTruth` (extended) writes generation 1 with
`type_only` on one edge and `deferred` plus `inferred` on the other, then
re-projects generation 2 with every flag flipped, through the production
`CanonicalNodeWriter`. It reads the properties back and requires an explicit
boolean on each edge (a missing property fails the test, it is not read as
false) and an unchanged edge count.

| Backend | Result |
| --- | --- |
| Neo4j, digest-pinned `neo4j:2026-community` (Kernel 2026.08.1) | PASS. gen1 flags `[true false false]` and `[false true true]`; gen2 `[false false false]` and `[true false false]`; 2 edges each generation |
| NornicDB, pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu` v1.3.3 (secondary) | PASS, identical values and edge count |

Sensitivity: with the `r.inferred` SET removed from the writer statement the
Neo4j run fails with `property "inferred" = <nil>, want an explicit boolean`.
The writer statement was restored byte-identical (matching file hash).

What the live re-projection does and does not prove: generation 2 is a non-first
generation with files set, so the writer's refresh deletes each file's IMPORTS
edges before the upsert recreates them. The live test therefore proves the
end-to-end outcome (flags take their new values, no duplicate or dropped edge),
not the unconditional SET in isolation. That shape is pinned by the writer
statement test, and by the shim's re-projection without a delete (all flags
false afterwards, 0 true), which is local smoke.

Fold-order sensitivity: the fold matrix includes each mixed case in both entry
orders. With a last-entry-wins fold six cases fail; with plain per-flag AND for
`deferred` (the rule the arbiter rejected) the `deferred then type_only` case
fails, which the original single-order cases missed because the accumulator is
seeded from the first entry. Both mutants were applied and the file restored
byte-identical.

NornicDB chain-batch: the live test passes on the pinned NornicDB, but whether
its UNWIND-MERGE chain-batch fast path still engages for this statement was not
recorded (NOT_CHECKED). The IMPORTS SET is single-variable, which is the shape
that path accepts.

## Performance Evidence:

Stage: the canonical structural-edge phase, IMPORTS upsert
(`canonicalNodeImportEdgeCypher`), which runs once per repository generation.
Change: three more unconditional property SETs (`r.type_only`, `r.deferred`,
`r.inferred`) on the existing statement. Backend: digest-pinned
`neo4j:2026-community`. Input cardinality: 38,240 edges over 2,000 File and 3,000
Module nodes (the largest measured corpus has 38,240 IMPORTS edges), batches of
500, with the production `File.path` uniqueness constraint and `Module.name`
lookup index in place.

Correctness invariant: one edge per `(file, module)`, unchanged; flags read back
exactly; a re-projection overwrites every flag. Metric: median wall time to write
the full edge set. Pass bar: extended median within +10% of the baseline median,
same operator tree. Stop threshold: a healthy regression above 10% stops the
change for profiling.

What was established locally, as correctness smoke only: the PROFILE operator
tree is identical before and after (`NodeIndexSeek`, `NodeUniqueIndexSeek
(Locking)`, `LockingMerge`, `Expand(Into)`, `SetProperties`; no Eager operator
and no new scan), the edge count is unchanged at 38,240, roughly 10% flagged
rows read back exactly (3,824 of each flag, no null flags), and a re-projection
with all flags false leaves 0 true. PROFILE reports the same 10 db hits per row
for both statements, so db hits do not reflect the added property writes.

Wall time, before/after: NOT_CHECKED on a valid test bed. A local run measured a
median of 1.181 s (baseline) against 1.184 s (extended) over 5 interleaved runs,
but that run was on a shared laptop and is not accepted timing evidence. The
dedicated remote was unreachable when this was written. The write-cost timing
must be re-run on the remote before merge against the pass bar above.

## Benchmark Evidence:

`BenchmarkExtractImportsFromFiles` and
`BenchmarkBuildCanonicalMaterializationWithImports` (`go test
./internal/projector/canonical -bench ... -benchmem -count=3`), base
`182a31124` against this change, same machine:

| Benchmark | allocs/op before | allocs/op after | B/op before | B/op after |
| --- | --- | --- | --- | --- |
| Extract, 100 files | 2,725 | 2,725 | 613,715 | 668,495 |
| Extract, 2,000 files | 54,135 | 54,135 | 11,473,010 | 12,437,614 |
| Build, 100 files | 3,005 | 3,005 | 900,364 | 971,582 |
| Build, 2,000 files | 58,267 | 58,267 | 17,649,117 | 18,810,384 |

Allocations per operation are unchanged. Bytes per operation rise about 8%
because three booleans widen the SDK `Import`, the fold accumulator, and
`ImportRow`; that is the cost of naming the fields, and the alternative
(decoding `Attributes`) adds an allocation per entry. `ns/op` is NOT_CHECKED:
the machine was under a load average near 25, and the same base benchmark moved
from 6.8 ms to 18-21 ms between runs, so no timing comparison is reported.

## Observability Evidence:

No new metric, span, or log. The IMPORTS statement is written inside the existing
canonical structural-edge phase group, whose duration and statement-count logs
(`canonical phase group completed`, seen in the live run output) already cover
it. The flags are directly inspectable on the edge
(`MATCH ()-[r:IMPORTS]->() RETURN r.type_only, r.deferred, r.inferred`).

## Limits stated plainly

- Legacy edges: an edge written before this change has no flag property. Edges on
  files that never change stay that way under delta-only operation until that
  file changes or a full generation runs (non-first full generations and delta
  refreshes delete a file's IMPORTS edges and rewrite them, so flags cannot
  survive a refresh). #7346 must treat a missing property as "flags unknown" and
  must not count it as proven runtime.
- The B-12 golden snapshot (`IMPORTS` floor of 63) is unchanged because fold
  identity is unchanged; the live B-7 gate is a CI check and was not run here.
- Timing on the remote, and the NornicDB CI legs, are the remaining proof.
- The `shared.ImportFlag*` drift test that ties the SDK field names to the
  parser constants cannot exist until the parser change (#7344, PR #7432) is on
  `main`. Today the strings match exactly (`type_only`, `deferred`, `inferred`).
  It is tracked as the first item of #7346 so it does not get lost.
- A legacy path in `builder.go` (`extractRelationships`, the Python-runtime-era
  payload keys) can append an unfolded `ImportRow` with zero flags. It predates
  this change, no Go collector emits its keys, and `imported_name` already has
  the same exposure.
