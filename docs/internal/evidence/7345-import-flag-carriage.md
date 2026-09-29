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
re-projects generation 2 with every generation-1 true flag now false and `type_only` true on the other edge, through the production
`CanonicalNodeWriter`. It reads the properties back and requires an explicit
boolean on each edge (a missing property fails the test, it is not read as
false) and an unchanged edge count.

| Backend | Result |
| --- | --- |
| Neo4j, digest-pinned `neo4j:2026-community` (Kernel 2026.08.1) | PASS at the final code: `ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=neo4j go test ./internal/replay/offlinetier -run TestCanonicalImportEdgesGraphTruth -count=1 -v` on a throwaway tmpfs container, exit 0 (the `go/` and `sdk/` diff has patch-id `b6559c19662cff40`, unchanged by the later documentation commits and by the base-only rebase). gen1 flags `[true false false]` and `[false true true]`; gen2 `[false false false]` and `[true false false]`; 2 edges each generation |
| NornicDB, pinned `ghcr.io/eshu-hq/nornicdb-amd64-cpu` v1.3.3 (secondary) | PASS at the build before the rebase onto #7438, identical values and edge count; not re-run at the final head (secondary backend, #7331) |

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

NornicDB (secondary, #7331): correctness proven by the live graph-truth test on the pinned v1.3.3 (explicit true/false stored and read back, edge
count unchanged); NornicDB write-path performance and chain-batch fast-path engagement not measured, withdrawn by arbiter ruling under #7331
(budgets are defined on Neo4j). The required NornicDB CI legs remain blocking.

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

Wall time, before/after, accepted run on the dedicated remote validation host.
The reviewed branch head was fetched and checked out detached in its own checkout;
the base and head statement text came from `git show` of the two commits and differ
only by the three SETs. The branch was later rebased onto a newer base with no conflict
and gained only documentation; the cumulative diff of `go/` and `sdk/` has the same stable
patch-id (`b6559c19662cff40`) at the measured head and at the final head, so the timed code is the
code under review. Neo4j was the digest-pinned
`neo4j:2026-community@sha256:eabfbb04...` (Kernel 2026.08.1, heap 8 GiB, page cache
8 GiB) with the production graph schema applied by `eshu-bootstrap-data-plane`.
Host: Linux x86_64, 16 logical CPUs, 123 GiB RAM. The result is a same-machine
relative ratio; `absolute_target_applicable` is false.

The harness ran, per set: 2,000 File, 3,000 Module and 38,240 IMPORTS edges (checked
before and after every timed write, and the edge set verified empty before each), batches of
500; one cold pair reported separately; one discarded warm-up per statement; then nine
warm rounds in a rotating Latin-square order of baseline, extended and a control (the
baseline statement again). Two A/A-only sets of nine rounds came first, and the control
bound was derived from their pooled control-to-baseline ratios (n=18, min 0.9590,
max 1.0397, SD 0.0234): bound = max |ratio - 1| + 3 SD = 0.1112. A round is valid only if its
control stays within that bound; a breach discards the round and re-runs it. None did (0 invalid
rounds in 9 attempts).

| Measure | Value |
| --- | --- |
| Baseline, nine warm runs (s) | 2.013, 1.995, 2.003, 2.017, 1.980, 2.032, 2.043, 1.953, 1.951 |
| Extended, nine warm runs (s) | 2.078, 2.014, 2.092, 2.083, 2.053, 2.030, 2.036, 2.083, 2.032 |
| Median baseline / extended | 2.003 s / 2.053 s |
| Ratio of medians | 1.0247 (+2.5%) |
| Same-round ratios, mean and SD | 1.0288 and 0.0229; range 0.9968 to 1.0666 |
| Cold pair, baseline / extended | 2.088 s / 2.067 s |
| Control-to-baseline ratios in the gated set | 0.9583 to 1.0359 |
| DB hits per row, baseline / extended | 10.0 / 10.0 (delta 0) |

Pass bar (extended median within +10% of the baseline median, same operator tree): met.
The PROFILE operator trees are identical (13 operators, no `Eager`, no new scan); as before,
DB hits do not reflect the property writes. The read-back after the extended write was
3,824 of each flag with no null flags, and the all-false re-projection left 0 true with 38,240 edges.
The extended statement is measurably a little slower (7 of 9 same-round ratios above 1),
about 2 to 3%, which is inside the bar. The estimator's own spread on unchanged code is small: the A/A ratio of
medians was 1.0156 and 1.0071 in the two pre-sets, and in the gated set the control against the baseline was
0.9997 while the extended against the control was 1.025. The +10% bar sits well outside that spread; the
wider 11.1% control bound is an outlier filter for a starved round, not the precision of the ratio.

Rule PD (host quiet): load1 was 2.96 at the start, 2.85 at the end and at most 3.04 in
the run (sampled every second, 79 samples), against a limit of 8 (half the 16 CPUs); the A/A sets
peaked at 3.97 and 3.40. The host was not idle: an unrelated compose project owned by another lane
was up throughout and is disclosed here rather than stopped. Its containers averaged about 110% CPU
(Postgres, peak 202%) and 56% CPU (Neo4j, peak 249%) across the seven samples taken during the gated set. The
run therefore measures the change under a bursty background that averaged about 1.7 cores (Postgres mean
110%, peak 202%; Neo4j mean 56%, peak 249%, near idle at the start and end), and the interleaved A/A
control, not an idle machine, is what bounds the noise. The derived control bound of 11.1% is wider than a
quiet host would give, which is why the verdict rests on the ratio of medians and the spread, and the stated
result is "inside the +10% bar with a measured cost of about 2 to 3%", not a precise cost.

Host snapshots taken by the runner at the start (20:01:50Z, load average 2.04, 2.04, 1.93) and the end (load average 2.70, 3.02, 2.55):
five containers were up throughout, none of them mine: the four `eshu7033proof-*` containers (eshu and
resolution-engine on the `eshu-7033-proof` image, Postgres 18, Neo4j 2026-community) and an idle `pg-6809`
Postgres. The run's own `eshu7345-timing-neo4j` and `eshu7345-timing-pg` containers were removed at exit.

This run used Neo4j only; NornicDB is covered by the paragraph under Graph truth.

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
- The required NornicDB CI legs must be green on the pushed head; NornicDB write-path performance is not measured (withdrawn under #7331). The Neo4j write timing is in the Performance Evidence section.
- Key drift: the parser change (#7344, merged as #7432) wrote the flags under
  the `shared.ImportFlag*` keys, and the SDK reads them through `json` tags. Two
  tests in `projector/canonical` (`TestImportFlagKeysMatchTheSDKFieldTags`,
  `TestParserFlagKeysReachTheImportEdgeFold`) tie the two together and fail if
  either side is renamed. Changing the `inferred` tag to `inferrd` fails both;
  the SDK file was restored byte-identical.
- A legacy path in `builder.go` (`extractRelationships`, the Python-runtime-era
  payload keys) can append an unfolded `ImportRow` with zero flags. It predates
  this change, no Go collector emits its keys, and `imported_name` already has
  the same exposure.
