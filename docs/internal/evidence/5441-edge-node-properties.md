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

The per-row delta is real and larger in relative terms than the "three
scalar SETs" characterization alone would suggest, because `copyRepoRelationshipMetadata`'s row map crosses a Go map bucket-growth
threshold once three more keys are added (13 keys before this change, 16
after, per row) — this is Go map internals reallocating bucket arrays, not a
new Cypher statement, traversal, or backend round trip. In absolute terms it
is still small: ~737ns and ~1.2KB of additional Go heap per edge row, entirely
inside Eshu's own in-process row shaping. Every comparable measured backend
write in this repo's evidence history (e.g. the `#3429` catalog/edges fix
above) puts a single graph write between 10ms and several seconds; ~0.0007ms
of added row-shaping cost per edge is immaterial against that. No optimization
(e.g. pre-sizing the row map) was attempted — flagged as a minor, non-blocking
follow-up if a future profile shows row shaping as a real contributor.

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
5000-row input, no-op executor, Apple M1 Max — OLD (`origin/main` 0a9461b21)
4,740,921 ns/op avg / 5,735,114 B/op avg / 80,102 allocs/op avg vs NEW (this
branch) 8,424,620 ns/op avg / 11,894,525 B/op avg / 105,101 allocs/op avg
(3 runs each, `-benchtime=3x -benchmem -count=3`). Delta is +736.7 ns,
+1,231.9 B, and +5.0 allocs per edge row, attributable to
`copyRepoRelationshipMetadata`'s row map crossing a Go map bucket-growth
threshold at 16 keys (13 before this change); no new `MATCH`, traversal, or
Cypher statement was added on either side, so the query plan shape is
unchanged and this is pure in-process Go row-shaping cost, negligible next to
this repo's measured backend graph-write latencies (milliseconds to seconds
per statement elsewhere in this evidence history). The `TerraformResource`
node write path is proven correct by
`TestCanonicalNodeWriterBuildsTerraformStateStatements` (additive `SET`
clause, same batch/MERGE shape); a dedicated benchmark was judged not
load-bearing since `promoteTerraformResourceAttributes` does bounded
`O(len(allowlist))` work (at most 4 attribute lookups) well under the cost of
the JSON decode that already produces its input upstream.

No-Observability-Change: both writers reuse the existing
`EdgeWriter`/`CanonicalNodeWriter` executor call paths, statement summaries,
and graph-write duration telemetry. No new metric, span, queue, worker, or
runtime knob is introduced; the SET clauses carry additional scalar
properties, not new instrumentation.
