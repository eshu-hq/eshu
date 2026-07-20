# #5441 — canonical edge and TerraformResource node property widening

Two additive, SET-clause-only changes:

- **Edges**: allowlist `source_revision`, `destination_namespace`, and
  `first_party_ref_version` onto the five canonical repository relationship
  edges (`DEPLOYS_FROM`, `DISCOVERS_CONFIG_IN`, `PROVISIONS_DEPENDENCY_FOR`,
  `USES_MODULE`, `READS_CONFIG_FROM`), both single-row and `UNWIND $rows`
  batch templates (`go/internal/storage/cypher/canonical_relationships.go`),
  fed from the reducer's `buildResolvedEdgeIntentRow` chokepoint
  (`go/internal/reducer/cross_repo_intent_row.go`) and
  `copyRepoRelationshipMetadata`
  (`go/internal/storage/cypher/edge_writer_retract.go`).
- **Nodes**: promote a bounded, redaction-safe, allowlisted subset of a
  Terraform state resource's classified attributes onto its
  `TerraformResource` node as prefixed scalar properties (`tf_attr_*`), via a
  new promotion allowlist
  (`go/internal/storage/cypher/terraform_attribute_promotion.go`) and an
  additive `r += row.attrs` merge appended to the existing fixed
  `TerraformResource` upsert template
  (`go/internal/storage/cypher/tfstate_canonical_writer.go`).

## Research The Pinned Backend

Absent-value semantics for the three new edge properties (source_revision,
destination_namespace, first_party_ref_version — most edges carry none of
them) were decided against the pinned NornicDB-New checkout
(`/Users/asanabria/os-repos/NornicDB-New`), not by assumption. NornicDB
v1.1.11's per-property `SET rel.x = row.x` path resolves the batch row
reference and stores the result directly into `edge.Properties[propName]`
without a nil-removal branch:

- `pkg/cypher/merge.go` `applySetToRelationshipWithContext` (~line 2211):
  `edge.Properties[propName] = normalizePropValue(v)`.
- `pkg/cypher/executor_mutations.go` `normalizePropValue` (~line 2166): the
  `default` case returns the value unchanged, including a Go `nil`.

So `SET rel.x = null` inside an `UNWIND $rows` batch does not remove the
property on NornicDB — it stores a literal nil-valued property key, diverging
from Cypher's standard "SET x = null removes the property" semantics. Writing
Cypher `null` for an absent value would therefore behave inconsistently
between NornicDB (property key persists as null) and any Neo4j-compat reader
expecting removal. The existing repo convention for every other optional edge
property here (`rationale`, `resolution_source`, `source_tool`) already writes
`""` uniformly via the `payloadString` family regardless of presence
(`go/internal/storage/cypher/edge_writer_payload.go`) — the three new
properties follow the same convention. This is the conservative,
backend-consistent choice; it was not guessed.

The additive `r += row.attrs` node-property merge for `TerraformResource`
mixes literal `r.x = row.x` assignments and a bare `r += row.attrs` map-merge
in one `SET` clause — a shape not previously exercised in this repo (the
existing `n += row.props` code-entity template uses only the map-merge form,
nothing else, in its `SET` clause). Traced against the pinned executor:
`pkg/cypher/set_helpers.go` `splitSetAssignments` splits a `SET` clause on
top-level commas (respecting quotes/bracket depth) before dispatch, so each
`r.x = row.x` and the trailing `r += row.attrs` become independent assignment
strings; `applySetToNodeWithContext` (`pkg/cypher/merge.go`) then dispatches
each assignment independently, checking for `+=` first and routing it to
`applySetMapMergeToNode`. The mixed-clause shape is therefore handled
correctly by construction. `row.attrs` is always emitted as a non-nil map
(never Go nil / Cypher null) — see
`applySetMapMergeToNode` in `pkg/cypher/set_helpers.go`, which requires the
resolved value to type-assert as `map[string]any` before merging anything, so
a genuinely empty map is a safe no-op. No live `PROFILE`/execution against a
running NornicDB instance was captured in this environment (no local Compose
stack stood up for this change); the proof above is a full source-path trace
of the pinned executor, not a guess.

## Measure The Same Shape Before And After

Both changes are `SET`-clause-only on already-batched `UNWIND $rows`/`MERGE`
templates — no new `MATCH`, traversal, or statement is added on either path,
so the query plan shape is invariant on both NornicDB and Neo4j, matching the
`Code-Edge Resolution Provenance Write Shape` precedent in
`docs/public/reference/cypher-performance.md`.

### Edge write benchmark

New focused benchmark
`go/internal/storage/cypher/edge_writer_repo_dependency_bench_test.go`
(`BenchmarkEdgeWriterRepoDependencyWrite`) isolates Eshu-owned row shaping and
batching for the five canonical edge types behind a no-op executor (1000 rows
per type, 5000 rows total), so it measures `buildRowMap` +
`copyRepoRelationshipMetadata` + `UNWIND` batching cost, not backend network or
write latency. Same input shape and row count run on both sides; only the
production code differs (OLD = `origin/main` at `0a9461b21`, checked out via a
detached `git worktree add --detach /tmp/eshu-5441-baseline origin/main`,
removed after the run per repo policy; NEW = this branch).

`go test ./internal/storage/cypher/... -run '^$' -bench BenchmarkEdgeWriterRepoDependencyWrite -benchtime=3x -benchmem -count=3`, Apple M1 Max:

| Side | ns/op (avg of 3) | B/op (avg of 3) | allocs/op (avg of 3) |
| --- | ---: | ---: | ---: |
| OLD (`origin/main` 0a9461b21) | 4,740,921 | 5,735,114 | 80,102 |
| NEW (this branch) | 8,424,620 | 11,894,525 | 105,101 |
| Delta (5000 rows) | +3,683,699 (+77.7%) | +6,159,411 (+107.4%) | +24,999 (+31.2%) |
| Delta per row | +736.7 ns | +1,231.9 B | +5.0 allocs |

The per-row delta was real and larger in relative terms than "three scalar
SETs" alone would suggest. Investigated per Prove-The-Theory-First below
rather than accepted as inherent.

### Map Bucket-Growth Pre-Sizing

Theory: `buildRowMap`'s DEPENDS_ON and typed-relationship branches build
`rowMap` from a small `map[string]any{...}` literal (3-4 keys), then append
`evidence_type`, `source_tool`, and `copyRepoRelationshipMetadata`'s ten keys
one at a time — up to 16 keys total after #5441 (13 before). A Go map literal
is sized from its own element count only, so every key appended afterward can
force the runtime to grow the map's bucket array once the load factor is
exceeded; that reallocation, not the three added string values themselves,
was suspected to dominate the measured delta.

Proof shim (Prove-The-Theory-First, cheapest-possible microbenchmark, run
before touching production code): a throwaway `BenchmarkMapGrowthTheory` in
`go/internal/storage/cypher` (removed after the result; not committed) built
the exact 15-key row shape from `buildRowMap`'s typed-relationship branch two
ways — a 4-key literal plus 11 appends (mirroring production) vs
`make(map[string]any, 15)` up front plus 15 direct assignments. Apple M1 Max,
`-benchtime=200000x -benchmem -count=5`:

| Shape | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Literal + 11 appends (current shape) | 1210-1239 | 2176 | 9 |
| Pre-sized `make` + 15 assigns | 562.9-571.8 | 1280 | 6 |

~2.1x faster, 41% less memory, 3 fewer allocations — exactly the signature of
one avoided bucket-array reallocation, not three avoided interface boxings
(which would show as ~3 allocs total, not 9 vs 6 plus the B/op gap). Theory
confirmed on representative data before implementing.

Fix: pre-size both affected `rowMap`s with `make(map[string]any, N)`
(`repoDependencyRowMapCapacity = 15`, `repoRelationshipRowMapCapacity = 16`,
covering each branch's worst-case key count) in
`go/internal/storage/cypher/edge_writer.go`, replacing the map literal plus
appends with direct assignments into the pre-sized map. Output-preserving:
every existing exact-key/value-asserting test in
`go/internal/storage/cypher` (`TestEdgeWriterWriteEdgesTypedRepoRelationshipDispatch`
and siblings) passed unchanged after the edit, proving the row map's key set
and values are byte-identical to before — only the allocation shape changed.

Re-measured `BenchmarkEdgeWriterRepoDependencyWrite` (same 5000-row input, no-op
executor, Apple M1 Max, `-benchtime=3x -benchmem -count=3`, 3 runs each):

| Side | ns/op (avg of 3) | B/op (avg of 3) | allocs/op (avg of 3) |
| --- | ---: | ---: | ---: |
| OLD (`origin/main` 0a9461b21) | 4,740,921 | 5,735,114 | 80,102 |
| NEW, unsized (first attempt) | 8,424,620 | 11,894,525 | 105,101 |
| NEW, pre-sized (shipped) | 5,093,139 | 7,415,118 | 90,102 |
| Delta pre-sized vs OLD (5000 rows) | +352,218 (+7.4%) | +1,680,004 (+29.3%) | +10,000 (+12.5%) |
| Delta per row | +70.4 ns | +336.0 B | +2.0 allocs |

Pre-sizing recovered most of the regression: ns/op delta dropped from +77.7%
to +7.4%, B/op from +107.4% to +29.3%, allocs/op from +31.2% to +12.5%. The
residual ~70ns/~336B/~2 allocs per row is now consistent with genuinely
inherent cost — three new `string` values boxed into `interface{}` map
entries at a capacity that already accounts for them, with no further
reallocation — not an unaccounted-for growth artifact. This is pure in-process
Go row-shaping cost with a no-op executor: it measures zero DB/network time.
Every comparable measured backend write in this repo's evidence history (e.g.
the `#3429` catalog/edges fix above) puts a single graph write between 10ms
and several seconds; ~0.00007ms of added row-shaping cost per edge is
immaterial against that, and this framing — Go-side row shaping only, not
end-to-end graph-write latency — is explicit here so it is not misread as a
77% (or even 7%) hit to actual write throughput.

### Node write

No new benchmark was added for the `TerraformResource` write path; the
existing `TestCanonicalNodeWriterBuildsTerraformStateStatements` proves the
additive `r += row.attrs` clause and row shape. The promotion allowlist itself
(`promoteTerraformResourceAttributes`) does bounded, `O(len(allowlist))` work
per resource (at most 4 attribute-path lookups for `aws_lambda_function`, the
largest entry), well below the cost of the JSON decode that already produces
the input `Attributes` map upstream, so a dedicated benchmark was judged not
load-bearing for this change; the `TestPromoteTerraformResourceAttributes*`
suite proves correctness and the redaction/size-cap guards.

Performance Evidence: `BenchmarkEdgeWriterRepoDependencyWrite` on the same
5000-row input, no-op executor, Apple M1 Max, `-benchtime=3x -benchmem -count=3`
(3 runs each) — OLD (`origin/main` 0a9461b21) 4,740,921 ns/op avg / 5,735,114
B/op avg / 80,102 allocs/op avg; an initial unsized implementation regressed to
8,424,620 ns/op avg / 11,894,525 B/op avg / 105,101 allocs/op avg
(+77.7%/+107.4%/+31.2%); root-caused via a Prove-The-Theory-First microbenchmark
to `buildRowMap`'s row map growing its bucket array as `copyRepoRelationshipMetadata`
appends keys past a small literal's initial size (proof shim: pre-sizing the
same 15-key map cut construction from 1210-1239 ns/op to 562.9-571.8 ns/op,
2176 B/op to 1280 B/op, 9 allocs/op to 6 allocs/op); shipped with
`repoDependencyRowMapCapacity`/`repoRelationshipRowMapCapacity` pre-sizing the
row map up front, recovering to 5,093,139 ns/op avg / 7,415,118 B/op avg /
90,102 allocs/op avg (+7.4%/+29.3%/+12.5% vs OLD, or +70.4 ns / +336.0 B / +2.0
allocs per edge row) — the residual matches three more `interface{}`-boxed
string values at an already-correct capacity, not further reallocation. No new
`MATCH`, traversal, or Cypher statement was added on either side, so the query
plan shape is unchanged; this is pure in-process Go row-shaping cost measured
with a no-op executor (zero DB/network time), negligible next to this repo's
measured backend graph-write latencies (milliseconds to seconds per statement
elsewhere in this evidence history) — do not read any of these percentages as
end-to-end graph-write latency. The `TerraformResource` node write path is
proven correct by `TestCanonicalNodeWriterBuildsTerraformStateStatements`
(additive `SET` clause, same batch/MERGE shape); a dedicated benchmark was
judged not load-bearing since `promoteTerraformResourceAttributes` does
bounded `O(len(allowlist))` work (at most 4 attribute lookups) well under the
cost of the JSON decode that already produces its input upstream.

No-Observability-Change: both writers reuse the existing
`EdgeWriter`/`CanonicalNodeWriter` executor call paths, statement summaries,
and graph-write duration telemetry. No new metric, span, queue, worker, or
runtime knob is introduced; the SET clauses carry additional scalar
properties, not new instrumentation.
