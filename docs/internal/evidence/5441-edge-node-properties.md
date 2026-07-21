# #5441 — canonical edge and TerraformResource node property widening

Two additive, SET-clause-only changes:

- **Edges**: allowlist `source_revision` and `first_party_ref_version` onto
  the five canonical repository relationship edges (`DEPLOYS_FROM`,
  `DISCOVERS_CONFIG_IN`, `PROVISIONS_DEPENDENCY_FOR`, `USES_MODULE`,
  `READS_CONFIG_FROM`) in
  `go/internal/storage/cypher/canonical_relationships.go`, fed from the
  reducer's `buildResolvedEdgeIntentRow` chokepoint
  (`go/internal/reducer/cross_repo_intent_row.go`) and
  `copyRepoRelationshipMetadata`
  (`go/internal/storage/cypher/edge_writer_row_metadata.go` as of #5441
  review round 3's file-cap split; `edge_writer_retract.go` before that). The
  production write path is exclusively the `UNWIND $rows` batch templates via
  `EdgeWriter.WriteEdges`/`buildRowMap`; the file also carries a matching
  single-row `BuildCanonicalRepoRelationshipUpsert` builder and its five
  single-row Cypher constants for contract/test symmetry, but that builder
  has no production caller today (pre-existing, not introduced by #5441) —
  see F6 in the #5441 review notes. Both were updated so the two shapes stay
  byte-identical, but only the batch path matters for the perf evidence
  below. A third candidate property, `destination_namespace`, was scoped,
  implemented, investigated, and then deliberately REMOVED before merge —
  see "destination_namespace: Investigated And Removed" below.
- **Nodes**: promote a bounded, redaction-safe, allowlisted subset of a
  Terraform state resource's classified attributes onto its
  `TerraformResource` node as prefixed scalar properties (`tf_attr_*`), via a
  new promotion allowlist
  (`go/internal/storage/cypher/terraform_attribute_promotion.go`) and an
  additive `r += row.attrs` merge appended to the existing fixed
  `TerraformResource` upsert template
  (`go/internal/storage/cypher/tfstate_canonical_writer.go`).

## Research The Pinned Backend

Absent-value semantics for the two new edge properties (source_revision,
first_party_ref_version — most edges carry neither) were decided against the
pinned NornicDB-New fork checkout (path per user-local config), not by
assumption. NornicDB
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
(`go/internal/storage/cypher/edge_writer_payload.go`) — the two new
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

## destination_namespace: Investigated And Removed

#5441 review round 2 raised a DECISION NEEDED item: `destination_namespace`
was implemented (typed field, Cypher SET clauses, reducer chokepoint) but a
review pass found it honestly self-documented as never populating in
production. That claim needed a real mechanism, not a restated assumption, so
it was probed directly rather than argued from memory.

**Probe** (a throwaway test, not committed, run before deciding): fed a
realistic ArgoCD Application document — `source.repoURL`/`targetRevision`
plus a `destination.namespace` — through `DiscoverEvidence` ->
`Resolve`, and printed every resulting `EvidenceFact` and `Candidate`:

```
fact[0]: kind=ARGOCD_APPLICATION_SOURCE relType=DEPLOYS_FROM srcRepo=repo-gitops tgtRepo=repo-deployable-source
  details=map[...]{"source_revision":"v1.4.0", ...}   (no destination_namespace key)
fact[1]: kind=ARGOCD_DESTINATION_PLATFORM relType=RUNS_ON srcRepo=repo-deployable-source tgtRepo=
  tgtEntity=platform:kubernetes:none:server/kubernetes.default.svc:none:none
  details=map[...]{"destination_namespace":"fixture-service", ...}

candidate[0]: relType=RUNS_ON srcRepo=repo-deployable-source tgtEntity=platform:... destNS="fixture-service"
candidate[1]: relType=DEPLOYS_FROM srcRepo=repo-gitops tgtRepo=repo-deployable-source destNS=""
```

**Mechanism, confirmed by the probe, not assumed:** ArgoCD evidence
discovery (`yaml_iac_evidence.go`'s `discoverArgoCDDocumentEvidence`) emits
TWO separate facts from one Application document: a `DEPLOYS_FROM` fact
(`SourceRepoID` = the manifest's own repo, `TargetRepoID` = the deployed
repo) with no `destination_namespace` key at all, and a SEPARATE `RUNS_ON`
fact (`appendDestinationPlatformEvidence`) whose `SourceRepoID` is the
DEPLOYED repo (note: equal to the `DEPLOYS_FROM` fact's `TargetRepoID`, not
its `SourceRepoID`) targeting a `Platform` entity, which DOES carry
`destination_namespace`. `aggregateCandidate` groups facts strictly by
`(SourceEntityID/RepoID, TargetEntityID/RepoID, RelationshipType)`, so a
`RUNS_ON` fact and a `DEPLOYS_FROM` fact for "the same deployment" never land
in the same bucket — confirmed directly by the probe's two distinct
candidates. This is true for every ArgoCD Application evidence path (the
only current producer of `destination_namespace`), so all five widened
relationship types would have written `destination_namespace: ""` on every
edge, forever, with zero live producer.

**Is it cheap to fix?** No. A real fix needs a genuine cross-candidate join
— `DEPLOYS_FROM.TargetRepoID == RUNS_ON.SourceRepoID` — added as a second
pass after `buildCandidates` produces the full candidate list. That is a
change to the resolver's core join algorithm (used by every relationship
type, not just these three fields), with real unresolved design questions:
which `RUNS_ON` candidate wins when more than one shares the same target
repo (multiple platforms/environments deployed from one repo)? Does a
non-ArgoCD `DEPLOYS_FROM` source (Kustomize, Helm chart dependency) borrow a
namespace from an unrelated ArgoCD `RUNS_ON` candidate that happens to share
the same target repo? None of that has a test matrix today. This is
architecture work, not a property addition.

**Decision: REMOVED.** Per the review's explicit framing — "shipping two
working properties is better than shipping three where one is a permanent
lie" — `destination_namespace` was stripped from every layer before merge:
the `Candidate`/`ResolvedRelationship` typed fields and their doc comments
(`go/internal/relationships/models.go`), `evidenceFactDestinationNamespace`
and its wiring in `aggregateCandidate` (`resolver.go`,
`evidence_edge_fields.go`), the reducer chokepoint
(`cross_repo_intent_row.go`), `copyRepoRelationshipMetadata`
(`edge_writer_retract.go` at the time of removal, later moved to
`edge_writer_row_metadata.go` by #5441 review round 3's file-cap split), all
ten Cypher `SET` clauses and the
`CanonicalRepoRelationshipParams` field (`canonical_relationships.go`), the
row-map pre-sizing constants (now 14/15 instead of 15/16 —
`edge_writer.go`), and every test that asserted on it. The pre-existing,
unrelated `destination_namespace` write onto `RUNS_ON`/`EvidenceFact.Details`
(`yaml_iac_evidence.go`'s `appendDestinationPlatformEvidence`) is untouched —
that is legitimate, real, pre-#5441 evidence-layer behavior, not part of
this feature. The cross-candidate join is a real gap worth fixing, but as
its own change with its own design review and test matrix, not folded into
this PR's scope.

## Measure The Same Shape Before And After

Both changes are `SET`-clause-only on already-batched `UNWIND $rows`/`MERGE`
templates — no new `MATCH`, traversal, or statement is added on either path,
so the query plan shape is invariant on both NornicDB and Neo4j, matching the
`Code-Edge Resolution Provenance Write Shape` precedent in
`docs/public/reference/cypher-performance.md`.

### Edge write benchmark (current, reproducible)

New focused benchmark
`go/internal/storage/cypher/edge_writer_repo_dependency_bench_test.go`
(`BenchmarkEdgeWriterRepoDependencyWrite`) isolates Eshu-owned row shaping and
batching for the five canonical edge types behind a no-op executor (1000 rows
per type, 5000 rows total), so it measures `buildRowMap` +
`copyRepoRelationshipMetadata` + `UNWIND` batching cost, not backend network or
write latency. Same input shape and row count run on both sides; only the
production code differs. OLD and NEW below are both re-run at the current true
merge-base `a5350625f` (checked out via a detached
`git worktree add --detach`, removed after the run) against this branch's
shipped code (`repoDependencyRowMapCapacity`/`repoRelationshipRowMapCapacity`
pre-sizing the row map, two tracked fields — source_revision and
first_party_ref_version — after the destination_namespace removal). This is
the authoritative, currently-reproducible measurement for this benchmark; an
earlier iteration's numbers are recorded separately below as a superseded
hypothesis, not restated here.

`go test ./internal/storage/cypher/... -run '^$' -bench BenchmarkEdgeWriterRepoDependencyWrite -benchtime=3x -benchmem -count=3`, Apple M1 Max, 3 runs each:

| Side | ns/op (avg of 3) | B/op (avg of 3) | allocs/op (avg of 3) |
| --- | ---: | ---: | ---: |
| OLD (`origin/main` `a5350625f`) | 4,839,055 | 5,735,771 | 80,103 |
| NEW, pre-sized (shipped) | 5,051,662 | 7,335,095 | 85,102 |
| Delta shipped vs OLD (5000 rows) | +212,607 (+4.4%) | +1,599,324 (+27.9%) | +4,999 (+6.2%) |
| Delta per row | +42.5 ns | +319.9 B | +1.0 allocs |

The deterministic allocs/op delta is +6.2% (2 new `string` values boxed into
`interface{}` map entries per row, one full allocation each — matching the
5,000-allocation gap for 5,000 rows exactly). This is pure in-process Go
row-shaping cost with a no-op executor: it measures zero DB/network time.
Every comparable measured backend write in this repo's evidence history (e.g.
the `#3429` catalog/edges fix above) puts a single graph write between 10ms
and several seconds; the per-row row-shaping cost here is immaterial against
that, and this framing — Go-side row shaping only, not end-to-end
graph-write latency — is explicit here so it is not misread as a hit to
actual write throughput.

**Why the row map is still pre-sized with `make(map[string]any, N)`**
(`repoDependencyRowMapCapacity = 14`, `repoRelationshipRowMapCapacity = 15`,
covering each branch's worst-case key count) in
`go/internal/storage/cypher/edge_writer.go`, replacing the map literal plus
appends an earlier iteration used: it measures no worse than the
literal-plus-appends alternative on this repo's pinned toolchain today (see
"Superseded hypothesis" below), it makes the intended final key count
explicit at the call site instead of leaving it to accumulate implicitly
across a dozen conditional appends, and reverting it now would be pure churn
with no measured benefit either way. This is a capacity-explicitness and
readability rationale, not a performance win — do not read it as one.
Output-preserving regardless of rationale: every existing
exact-key/value-asserting test in `go/internal/storage/cypher`
(`TestEdgeWriterWriteEdgesTypedRepoRelationshipDispatch` and siblings) passed
unchanged, proving the row map's key set and values are byte-identical to the
literal-plus-appends form.

### Superseded hypothesis: Map Bucket-Growth Pre-Sizing

**This section is historical, not a current claim.** An earlier development
iteration measured a large gap between an unsized `rowMap` (small literal plus
per-key appends) and a pre-sized one, attributed it to Go map bucket-array
growth, and used that theory to justify pre-sizing. A fresh same-shape
re-check on this repo's pinned `go1.26.5` toolchain does not reproduce the
gap: `go env GOEXPERIMENT` is empty, meaning Go's swiss-table map
implementation is already the default, and swiss-table incremental-growth
behavior differs materially from the classic bucket-hmap this theory assumed.
Re-running the same production benchmark with the pre-sizing temporarily
reverted (not committed) landed on B/op and allocs/op statistically
indistinguishable from the shipped pre-sized code (~7,335,000 B/op /
~85,102 allocs/op either way) — not the gap this section originally
documented. The numbers below are kept as the original discovery record for
traceability; do not cite them as current fact.

Original theory: `buildRowMap`'s DEPENDS_ON and typed-relationship branches
build `rowMap` from a small `map[string]any{...}` literal (3-4 keys), then
append `evidence_type`, `source_tool`, and `copyRepoRelationshipMetadata`'s
keys one at a time. A Go map literal is sized from its own element count
only, so every key appended afterward can force the runtime to grow the
map's bucket array once the load factor is exceeded; that reallocation, not
the added string values themselves, was suspected to dominate the measured
delta.

Original proof shim (`BenchmarkMapGrowthTheory`, throwaway, not committed)
built a 15-key row shape two ways — a 4-key literal plus 11 appends
(mirroring production at the time) vs `make(map[string]any, 15)` up front
plus 15 direct assignments. Apple M1 Max, `-benchtime=200000x -benchmem -count=5`:

| Shape | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Literal + 11 appends (original shape) | 1210-1239 | 2176 | 9 |
| Pre-sized `make` + 15 assigns | 562.9-571.8 | 1280 | 6 |

Original production benchmark (before the destination_namespace removal,
three tracked fields, OLD = `origin/main` at the then-current merge-base
`0a9461b21`):

| Side | ns/op (avg of 3) | B/op (avg of 3) | allocs/op (avg of 3) |
| --- | ---: | ---: | ---: |
| OLD (`origin/main` `0a9461b21`) | 4,740,921 | 5,735,114 | 80,102 |
| NEW, unsized (first attempt) | 8,424,620 | 11,894,525 | 105,101 |
| NEW, pre-sized (shipped, at the time) | 5,093,139 | 7,415,118 | 90,102 |

This originally read as "pre-sizing recovered a ~2.1x-slower, 107%-more-memory
regression." It does not read that way today: see the current, reproducible
numbers in "Edge write benchmark" above, and the rationale there for why the
pre-sizing still ships despite the theory not holding up.

### Resolver candidate aggregation (#5441 review round 2, P1-2)

The writer-side benchmark above does not cover the resolver: the #5441 P0 fix
added `evidenceFieldWinner.consider()` calls plus two per-fact `Details`
reads (one of which, `evidenceFactFirstPartyRefVersion`, can fall through to
`ExtractTerraformRefPin`'s string parsing) inside `aggregateCandidate`
(`resolver.go`) — a repo-scale hot path that runs once per resolver pass over
every candidate's evidence facts. This was measured independently in review
and found undisclosed; fixed here with a committed benchmark and a real
before/after, following the existing `gcp_evidence_bench_test.go` /
`catalog_anchor_bench_test.go` precedent in this package.

New benchmark `go/internal/relationships/resolver_edge_fields_bench_test.go`
(`BenchmarkResolveCandidateAggregation`): 2000 distinct
(source, target, relationship type) candidates, 5 evidence facts each,
deliberately the worst case for the new fields — every fact carries a
`source_ref` needing the `ExtractTerraformRefPin` fallback (real corpora will
have this key far less often; most non-Terraform evidence kinds never set
`source_ref` at all). OLD = `origin/main` at the current true merge-base
`a5350625f` (checked out via a detached `git worktree add --detach`, removed
after the run); NEW = this branch (pre-`settled()` and post-`settled()`
measured separately below). Re-run for #5441 review round 3 (P2): the prior
figures in this section were captured before the destination_namespace field
was removed (3 tracked fields, not 2) and against a stale `982323ef6`
merge-base, so both OLD and NEW below are fresh, matched-methodology
re-measurements against the current branch and the current true merge-base.

`go test ./internal/relationships/... -run '^$' -bench BenchmarkResolveCandidateAggregation -benchtime=10x -benchmem -count=7`,
Apple M1 Max, machine under lighter concurrent load than the round-2 run
(`uptime` load averages 5.47/4.74/5.56 during these runs, vs 10.84/12.19/25.00
previously):

| Side | ns/op (avg of 7) | B/op (avg of 7) | allocs/op (avg of 7) |
| --- | ---: | ---: | ---: |
| OLD (`a5350625f`) | 5,621,160 | 12,435,674 | 78,078 |
| NEW, before `settled()` optimization | 6,851,635 | 12,792,330 | 88,078 |
| NEW, after `settled()` optimization (shipped) | 6,641,843 | 12,792,369 | 88,078 |

`ns/op` is still noisy run-to-run on a shared machine, though less so than the
round-2 run; `B/op` and `allocs/op` are deterministic and reproducible across
every run regardless of noise, so they remain the trustworthy signal:

- **B/op: +356,695 (+2.9%)** (12,792,369 vs 12,435,674 for 2000 candidates) —
  down from the previously-reported +3.5%, because two tracked fields (not
  three) now flow through `evidenceFieldWinner`/`Candidate`.
- **allocs/op: +10,000 (+12.8%)** (88,078 vs 78,078) — unchanged from the
  prior measurement, because this delta comes entirely from
  `strings.Split` inside `ExtractTerraformRefPin`'s `?ref=` fallback path
  firing on every one of this benchmark's deliberately-worst-case facts (5
  extra allocations per candidate, 5 facts × 1 alloc each) — a per-fact
  allocation count the destination_namespace removal never touched.

**Optimization measured, not assumed** (Prove-The-Theory-First): the review
suggested short-circuiting `evidenceFieldWinner.consider()` once a field's
winner already holds a maximum-confidence (1.0) value, since
`clampConfidence` never lets any value exceed 1.0, so no later fact could
ever change that outcome. Implemented as `evidenceFieldWinner.settled()`
(`evidence_edge_fields.go`), gating the call site in `aggregateCandidate` so
the (possibly expensive) per-fact value is never computed only to be
discarded, and proven correctness-preserving by
`TestAggregateCandidateSourceRevisionSettledWinnerSkipsLaterFacts`
(`resolver_test.go`) — a later fact at the same max confidence must not
overwrite an already-settled winner.

Measured result: **no change** in `B/op`/`allocs/op` between the
before/after-`settled()` NEW rows above (88,078 both times) — confirming,
not assuming, that this optimization is a no-op for this benchmark's
realistic confidence range (0.80–0.88; nothing ever reaches exactly 1.0),
exactly as predicted in the code comment before measuring. It is kept
anyway as a strict, zero-downside early exit for the narrow case it does
help (an explicit maximum-confidence fact), not claimed as the fix for this
regression.

**Disposition: ACCEPTED, not further optimized**, for three reasons: (1) the
measured magnitude (allocs +12.8%, bytes +2.9%) is the same order as the
already-accepted writer-side delta (+6.2% allocs, +27.9% bytes after
pre-sizing); (2) this benchmark is a deliberate worst case — 100% of facts
require the `ExtractTerraformRefPin` fallback, which real corpora will hit far
less often (most evidence kinds never set `source_ref`, and ArgoCD/GitHub
Actions evidence sets `first_party_ref_version` directly, skipping the
fallback entirely — see `evidenceFactFirstPartyRefVersion`'s doc comment);
(3) `aggregateCandidate` runs once per resolver pass over a bounded candidate
set, not per API request — it is a batch/queued-cadence cost, not a
user-facing latency path.

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

Performance Evidence: #5441 review round 3, P2 — re-measured against final
HEAD and reframed per review's decision; see "Edge write benchmark (current,
reproducible)" and "Superseded hypothesis: Map Bucket-Growth Pre-Sizing"
above for the full context. `BenchmarkEdgeWriterRepoDependencyWrite` on the
same 5000-row input, no-op executor, Apple M1 Max,
`-benchtime=3x -benchmem -count=3` (3 runs each), OLD and NEW both re-run at
the current true merge-base `a5350625f` — OLD 4,839,055 ns/op avg /
5,735,771 B/op avg / 80,103 allocs/op avg; shipped (pre-sized)
`repoDependencyRowMapCapacity`/`repoRelationshipRowMapCapacity` (14/15 after
the destination_namespace removal) measures 5,051,662 ns/op avg / 7,335,095
B/op avg / 85,102 allocs/op avg (+4.4%/+27.9%/+6.2% vs OLD, or +42.5 ns /
+319.9 B / +1.0 allocs per edge row) — the residual matches two
`interface{}`-boxed string values per row (source_revision,
first_party_ref_version), one allocation each. No new `MATCH`, traversal, or
Cypher statement was added on either side, so the query plan shape is
unchanged; this is pure in-process Go row-shaping cost measured with a no-op
executor (zero DB/network time), negligible next to this repo's measured
backend graph-write latencies (milliseconds to seconds per statement
elsewhere in this evidence history) — do not read any of these percentages as
end-to-end graph-write latency. The row map is still pre-sized with
`make(map[string]any, N)`, but not as a measured performance win: a fresh
apples-to-apples re-check (temporarily reverting the pre-sizing, not
committed) found pre-sized and unsized `rowMap` construction statistically
indistinguishable on this repo's pinned go1.26.5 toolchain (swiss-table maps
by default), so the originally-claimed bucket-growth regression and its
recovery are a superseded hypothesis, not current fact — kept only for
capacity-explicitness and to avoid reverting a change that measures no
worse either way. The `TerraformResource` node write path is proven correct
by `TestCanonicalNodeWriterBuildsTerraformStateStatements` (additive `SET`
clause, same batch/MERGE shape); a dedicated benchmark was judged not
load-bearing since `promoteTerraformResourceAttributes` does bounded
`O(len(allowlist))` work (at most 4 attribute lookups) well under the cost of
the JSON decode that already produces its input upstream.
`BenchmarkResolveCandidateAggregation` (2000 candidates x 5 facts,
`-benchtime=10x -benchmem -count=7`, Apple M1 Max, OLD and NEW both re-run at
the current true merge-base `a5350625f` under lighter machine contention than
the round-2 run) isolates the resolver-side hot path
(`relationships.aggregateCandidate`) the writer benchmark does not cover:
deterministic allocation metrics show +356,695 B/op (+2.9%) and +10,000
allocs/op (+12.8%) for 2000 candidates against OLD; `ns/op` remains noisy on
this shared machine (OLD avg 5.62M, NEW-shipped avg 6.64M ns/op across
matched-methodology runs) but `B/op`/`allocs/op` are deterministic and are the
trustworthy signal. The reviewer-suggested `evidenceFieldWinner.settled()`
short-circuit was implemented, proven correctness-preserving
(`TestAggregateCandidateSourceRevisionSettledWinnerSkipsLaterFacts`), and
measured (not assumed) to be a no-op for this benchmark's realistic
0.80-0.88 confidence range — confirmed again on this re-run (before/after
`settled()` B/op and allocs/op are byte-identical: 12,792,330/88,078 vs
12,792,369/88,078) — accepted anyway as a zero-downside safety net, not as
the fix. The regression itself is accepted, not further optimized: this
benchmark deliberately forces the `ExtractTerraformRefPin` fallback on every
fact (real corpora hit it far less often), the magnitude matches the
already-accepted writer-side delta, and `aggregateCandidate` runs on a
bounded per-resolver-pass candidate set, not a per-request path.

No-Observability-Change: both writers reuse the existing
`EdgeWriter`/`CanonicalNodeWriter` executor call paths, statement summaries,
and graph-write duration telemetry. No new metric, span, queue, worker, or
runtime knob is introduced; the SET clauses carry additional scalar
properties, not new instrumentation. The resolver's `Resolve`/
`aggregateCandidate` path carries no telemetry before or after this change;
the two new typed fields do not introduce any.

## Golden-Corpus Proof (B-7 Gate, #5441 Review Round 2 P1-1)

The gate was run inline (single blocking call per attempt, no detached
background monitoring across turns), with a unique Compose project and port
overrides since another agent's stack (`infra-predicate-fix-nornic`) was
running concurrently on the same shared machine:

```
export COMPOSE_PROJECT_NAME="eshu-gate-5441"
export ESHU_POSTGRES_PORT=25441
export NEO4J_BOLT_PORT=25442
export NEO4J_HTTP_PORT=25443
export GATE_API_PORT=25444
export GATE_MCP_PORT=25445
export GATE_COLLECTOR_SETTLE_SECONDS=45   # default 20s was too tight under machine load
bash scripts/verify-golden-corpus-gate.sh
```

Two prior attempts with the default 20s settle window aborted early
("only 7", then "only 13", credentialed collector sources landed; want
>= 17") under real contention from other agents' Docker stacks on this
shared machine — an infra timing issue unrelated to this branch's code,
correctly not reported as a result. Widening the settle window to 45s
produced a complete, passing run:

```
summary: 436 pass, 0 required-fail, 1 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 63s, budget ceiling 1800s) ===

[PASS] rc-154: (Repository)-[:DEPLOYS_FROM]->(Repository) evidence_kinds⊇[ARGOCD_APPLICATION_SOURCE] count=2, want >= 1 [DEPLOYS_FROM]
[PASS] rc-154_edge_prop_source_tool: (Repository)-[:DEPLOYS_FROM]->(Repository) edge property "source_tool" must be in [argocd]: 0/2 matching edges offending
[PASS] rc-154_edge_prop_source_revision: (Repository)-[:DEPLOYS_FROM]->(Repository) edge property "source_revision" must be non-empty: 0/2 matching edges offending
[PASS] rn-terraform-resource-attribute-promotion_node_prop_tf_attr_instance_type: (TerraformResource) node property "tf_attr_instance_type" (non-empty) present on 1 node(s), want >= 1
```

(`rc-154_edge_prop_source_revision` read `2/2 matching edges offending`
before the second-P0 fix — see "The second P0" below — and `0/2` after; this
is the direct before/after proof the fix landed, not just a passing gate.)
The single advisory-warn is `phase_collect: observed=45.0s` against the
default 20s baseline/25s ceiling — the direct, expected consequence of this
run's own `GATE_COLLECTOR_SETTLE_SECONDS=45` override, non-blocking by the
gate's own classification, not a real finding.

**Note on the branch history:** an earlier commit's message
(`#5441: golden-corpus proof for source_revision (P1 finding 3) + second-P0
fix`) stated the fix was "NOT YET independently confirmed by a clean,
uncontended live gate run" — that line was written before the successful run
above landed and was never updated, so the committed record understated what
was actually proven. This section is the correction: the gate output above
is the actual, complete, passing run, and it is now committed (discoverable
on the branch, not only reported in review conversation).

**Note on the ID (review round 7):** the gate output above, and the prose
through the rest of this "Golden-Corpus Proof" section, all use the
correlation ID `rc-154`, because that was this branch's ID at the time this
run was captured. `rc-154` was later found to collide with a second,
unrelated, pre-existing entry (issue #5483's Flux HelmRelease
`RECONCILES_FROM` correlation, added to `origin/main` non-sequentially in a
`rc-140`..`rc-155` block far from where this branch's entries were inserted,
so a local scan of nearby IDs never surfaced it) and was renamed a second
time to the true final ID, `rc-156` — see "ID collision found and fixed"
below for the full story. This transcript is kept verbatim as the historical
record of the run that proved the fix; look up this correlation in the
current snapshot by `rc-156`, not `rc-154`.

### The second P0: found by the gate, not assumed fixed

The first live gate run (fully complete, 18,635 lines of output) genuinely
failed `rc-154_edge_prop_source_revision` at `2/2 matching edges offending`
even after the reducer-side #5441 P0 fix (typed `Candidate`/
`ResolvedRelationship` fields) had already landed and passed every unit
test. Root cause: `discoverArgoCDDocumentEvidence`
(`go/internal/relationships/yaml_iac_evidence.go`), not
`discoverStructuredArgoCDEvidence`, is the evidence path that actually fires
for a bare top-level ArgoCD `Application` YAML manifest — the structured path
requires a parser to have already populated
`parsedFileData["argocd_applications"]`, which a plain `Application` YAML
file does not trigger in this corpus. `discoverArgoCDDocumentEvidence`'s
`matchCatalog` call passed a hard-coded `nil` for `extraDetails`, so every
fact it produced carried no `source_revision` key at all, regardless of the
reducer-side fix.

Fixed by `argocdApplicationSourceRevisionDetails`
(`go/internal/relationships/argocd_document_source_revision.go`), which
reads the declared `targetRevision` off `spec.source` (or the first
`spec.sources[]` entry carrying one) and threads it through as
`extraDetails` instead of `nil`. Proven failing-first, then green, with 3
unit tests (`yaml_iac_evidence_source_revision_test.go`) driving
`DiscoverEvidence` directly: single-source with revision, absent revision
(negative), and multi-source (first non-empty revision wins).

### first_party_ref_version golden-corpus coverage (#5441 review round 2, P1-1)

`first_party_ref_version` had zero golden-corpus coverage before this round:
the only in-corpus fixture with a `?ref=` module source
(`tests/fixtures/ecosystems/terraform_comprehensive/modules.tf`'s `eks`
module) pointed at `github.com/example/terraform-aws-eks.git`, an org not in
the 20-repo catalog, so it never resolved to a real `USES_MODULE` edge —
exactly the same "fixture points outside the catalog" gap that let
`source_revision` sit uncovered until the live gate caught it. Repointed at
the in-corpus `deployable-source` repository
(`git::https://github.com/acme/deployable-source.git?ref=v1.0.0`, the same
established cross-fixture target every other tool fixture in this corpus
uses) and added `rc-155`, mirroring the ArgoCD `source_revision` correlation's
pattern (`rc-156` as of review round 7's rename; `rc-154` in the run captured
above): `evidence_kinds` narrowed to `TERRAFORM_MODULE_SOURCE`,
`required_edge_properties` on `source_tool` (pinned to `terraform`) and
`first_party_ref_version` (non-empty). Confirmed alongside the ArgoCD
correlation and `rn-terraform-resource-attribute-promotion` in the same
passing run above.

### ID collision found and fixed (twice — read this before trusting a local scan)

**First collision, review round 2:** while adding this branch's ArgoCD
`source_revision` correlation, `origin/main` had advanced past this branch's
original base and merged its own new `rc-153` (RUNS_IMAGE, issue #5432) — an
unrelated correlation that happened to reuse the same next-available ID this
branch had already claimed. Two array entries shared `"id": "rc-153"`, which
would have silently conflated two different correlations under any ID-keyed
lookup (the gate's `-required-correlations=all` blocking-set resolution,
`blockingCorrelations[rc.ID]`). Renamed this branch's entry to `rc-154`,
updated the Go comment references, and reported it fixed. The pre-existing
`rc-153` (RUNS_IMAGE) and its `AGENTS.md` evidence citations were left
untouched and remain correct today.

**Second collision, found in review round 7 — the `rc-154` rename above was
itself wrong.** `rc-154` was NOT actually free: a pre-existing Flux
HelmRelease `RECONCILES_FROM` correlation from issue #5483 already held
`"id": "rc-154"` (`testdata/golden/e2e-20repo-snapshot.json` ~line 1595).
`rg -c '"id": "rc-154"'` against the committed snapshot returned 2, and the
"already fixed" claim in the previous version of this section was false.

**Why a local scan missed it both times:** this branch's correlation entries
were inserted non-sequentially, near the unrelated `rc-19`/`rc-20` block from
an older base, while `#5483`'s Flux entries sit in a sequential `rc-140`..
`rc-155` block added later by a different PR. Scanning the IDs adjacent to
where THIS branch's own entries live never surfaces a collision that exists
somewhere else entirely in a 700+-entry file. The correct check is a
whole-file duplicate scan, not a local eyeball of the surrounding lines:

```
rg -o '"id": "rc-[0-9]+"' testdata/golden/e2e-20repo-snapshot.json | sort | uniq -d
```

An empty result from that command is the only thing that actually proves no
duplicate exists — "the ID after the one I collided with" is an assumption,
not a check, and this branch made that exact assumption twice.

**Final ID: `rc-156`.** Before choosing it, ran
`rg '"id": "rc-1[0-9][0-9]"' testdata/golden/e2e-20repo-snapshot.json | sort -u`
and confirmed `rc-155` (this branch's own USES_MODULE `first_party_ref_version`
correlation) was the highest ID in use and `rc-156` did not already exist.
Renamed the ArgoCD `source_revision` entry from `rc-154` to `rc-156`,
updated its self-reference inside `rc-155`'s own description text (which had
cited `rc-154` by name), and updated every Go comment reference
(`yaml_iac_evidence.go`, `yaml_iac_evidence_source_revision_test.go`,
`argocd_document_source_revision.go`). Confirmed with the same whole-file
duplicate scan (empty result, see "Verify and report" below) and a re-run of
the golden-corpus unit tests and `scripts/test-verify-golden-corpus-gate.sh`.
`#5483`'s `rc-154` (Flux HelmRelease `RECONCILES_FROM`) is unrelated to
#5441 and was left untouched throughout.

## Stale `tf_attr_*` Removal: A P0 Found By Review, Not By Tests (#5441 review round 9)

Round 8's fix for the additive-merge staleness bug (a `MATCH (r) WHERE r.uid
IN $uids REMOVE ...` clause fused into the same `MERGE ... SET` upsert
statement, per allowlisted resource type) did not work on the pinned
NornicDB executor, and it made things worse, not better. An independent
reviewer built a throwaway Go module with a `replace` directive at the
pinned NornicDB-New fork checkout (path per user-local config) and ran the
exact shipped Cypher template through the real `StorageExecutor`. Both of
the following were true on **every write**, not only refreshes:

1. The staleness bug the round-8 fix was meant to close was NOT closed --
   `tf_attr_instance_type` survived a re-projection without it, unchanged
   from the original bug.
2. NEW: `r.evidence_source` was corrupted with literal Cypher source text
   (`"'projector/tfstate'\nREMOVE r.tf_attr_instance_type"` instead of the
   clean `"projector/tfstate"`), on all five allowlisted resource types.

### Root cause, traced through the pinned executor

1. `pkg/cypher/clauses.go` `executeUnwind` splits the `MERGE...REMOVE...SET`
   tail as a `restQuery`.
2. The batched UNWIND fast path, `parseUnwindMergeChainPattern`
   (`clauses.go` ~1810), bails out (`return plan` with `supported: false`)
   whenever `REMOVE` appears anywhere in the mutation text -- confirmed by
   reading the bail-out keyword list directly (`CALL`, `DELETE`, `DETACH`,
   `REMOVE`, `FOREACH`, `UNWIND`, `RETURN`).
3. That falls through to the per-row execution loop, which sees `+=` and
   calls `executeInternal` with the WHOLE statement text, once per row --
   itself a real performance regression (every allowlisted-type row falls
   off the batched path into per-row execution; not measured or disclosed
   in the round-8 evidence).
4. Top-level routing (`pkg/cypher/executor_query_routing.go` ~234) sees a
   leading `MERGE` with no `WITH`/`WHERE`/`OPTIONAL MATCH` and no second
   `MERGE`, so it calls `executeMerge`.
5. `pkg/cypher/merge.go` `executeMerge` (~448-672) has **zero** knowledge of
   `REMOVE`. Verified directly: its standalone-SET clause's end boundary
   (`setEnd`) is computed by scanning only for `withIdx` and `returnIdx`
   (merge.go ~639-646) -- never a `removeIdx`. With no `WITH`/`RETURN` in
   the statement, `setEnd = len(cypher)`, so the entire `REMOVE ...` clause
   and everything after it is swallowed into the SET clause's text.
6. That corrupted text then glues onto the immediately preceding property
   assignment (`r.evidence_source = 'projector/tfstate'` in the shipped
   round-8 shape), producing the corrupted value observed.

### Real-backend reproduction and fix verification

Built the reviewer's exact recommended harness: a throwaway Go module
(`go.mod` with `replace github.com/orneryd/nornicdb => <pinned NornicDB-New
fork checkout, path per user-local config>`) constructing a real
`cypher.NewStorageExecutor` over an in-memory `storage.Engine`, running the
production statement text through `Execute`. (Building against this fork
requires the `nolocalllm` build tag -- `go run -tags nolocalllm .` -- to
select the pure-Go localllm stub instead of the CGo `llama.go` binding,
which needs a prebuilt native library this environment does not have; this
is unrelated to the Cypher bug and does not affect correctness, only local
buildability of the throwaway harness.)

**Scenario 1 (baseline reproduction of the round-8 shape, reproduced 3x):**

```
after first projection:  tf_attr_instance_type="t3.micro"  evidence_source="'projector/tfstate'\nREMOVE r.tf_attr_instance_type"
after second projection: tf_attr_instance_type="t3.micro"  evidence_source="'projector/tfstate'\nREMOVE r.tf_attr_instance_type"
staleness bug reproduced (attr survived): true
evidence_source corrupted: true
```

Matches the reviewer's report exactly, confirming the diagnosis before
touching any fix code.

**Scenario 2 (the round-9 two-statement fix, reproduced 3x, deterministic
across all runs):**

```
after first projection:  tf_attr_instance_type="t3.micro"  tf_attr_ami="ami-0abcdef1234567890"  evidence_source="projector/tfstate"
after second projection: tf_attr_instance_type=<nil>  tf_attr_ami="ami-0abcdef1234567890"  evidence_source="projector/tfstate"

ASSERTION 1 (stale tf_attr_instance_type gone): true
ASSERTION 2 (tf_attr_ami still present, untouched): true
ASSERTION 3 (evidence_source exactly "projector/tfstate", uncorrupted): true
RESULT: PASS
```

### Why the round-8 fake test passed anyway

`fakeTerraformResourceGraph.Execute()` (the round-8
`tfstate_canonical_writer_stale_attrs_test.go`) regex-extracted the REMOVE
key list from the Cypher TEXT and manually `delete()`d from a Go map. It
never invoked NornicDB's parser or executor, so it proved the semantics the
round-8 fix INTENDED, not that the pinned backend executes the shape as
written -- it stayed green while the real backend failed both assertions.
The golden-corpus gate would not have caught it either: the cassette adds
the `aws_instance` resource in a single projection and never re-projects the
same UID with the attribute removed, and the B-12 snapshot floor does not
assert `evidence_source`'s exact value.

### The precedent the round-8 fix cited did not support the shape it built

Round 8 cited `rds_posture_node_writer.go` /
`ec2_block_device_kms_posture_node_writer.go` as precedent for a
REMOVE-then-SET shape, but read too loosely: both writers ship **two
separate statements** -- a plain `UNWIND ... MATCH/MERGE ... SET` upsert,
and a standalone `MATCH (resource:CloudResource) WHERE ... REMOVE r.x, r.y`
retract with no trailing SET (exactly the shape `pkg/cypher/executor_mutations.go`
`executeRemove` supports). Nothing in this codebase combines
MERGE + SET + REMOVE + SET in one statement. Re-read correctly, the
precedent validates the two-statement design below, not the fused one round
8 shipped.

### The fix: two statements, REMOVE before SET

- `canonicalTerraformStateResourceUpsertCypher` (`tfstate_canonical_writer.go`)
  reverts to exactly what it was before round 8's fix attempt -- one unified
  `UNWIND $rows AS row MERGE ... SET ..., r += row.attrs` template for every
  resource type, byte-identical to the pre-round-8 shape. No REMOVE clause,
  no per-type variants.
- `terraformStateResourceAttributeRemoveCypherByType` builds a genuinely
  standalone `MATCH (r:TerraformResource) WHERE r.uid IN $uids REMOVE ...`
  statement per allowlisted resource type -- no MERGE, no SET, no UNWIND --
  still derived from `terraformAttributePromotionAllowlist` via
  `terraformAttributePromotionKeysForType` so the REMOVE list can never
  drift from the allowlist it must fully cover (this part of round 8's
  design was correct and is unchanged).
- `buildTerraformStateStatements` returns the REMOVE statements **before**
  the upsert statement, in that order.

**Ordering reasoning:** REMOVE-then-SET is the only correct order. The
REMOVE statement unconditionally clears the allowlisted type's FULL closed
set of possible `tf_attr_*` properties for every UID in the batch; the
upsert's additive `r += row.attrs` merge then re-establishes only the
subset the current row promotes. If the upsert ran first and REMOVE ran
second, REMOVE's unconditional full-key-set clear would immediately strip
every `tf_attr_*` property the upsert just wrote -- corrupting every write,
not only refreshes. SET-then-REMOVE was considered and rejected for exactly
this reason.

**Partial-failure reasoning:** these are two separate statements, not one
atomic transaction -- matching this repo's own precedent
(`rds_posture_node_writer.go` / `ec2_block_device_kms_posture_node_writer.go`
do not bind their upsert/retract pair atomically either). If REMOVE succeeds
and the following upsert fails before completing, the affected nodes
temporarily carry no `tf_attr_*` properties until the next successful run of
the same generation reapplies both statements. Both statements are
idempotent (REMOVE on an already-absent property, or SET to an
already-current value, are no-ops), so a retry of the whole generation is
self-healing. Judged an acceptable, visibly-empty gap under a transient
failure -- not the silently-wrong-forever state the original bug produced,
and not a new failure mode this repo's existing retry model doesn't already
handle for its other two-statement writers.

### Test strategy: the fake alone is not sufficient, kept two additional guards

The fake fixture is not worthless -- it cheaply documents the Go-level
sequencing contract (REMOVE built and ordered before the upsert, with the
right UIDs and property names) and still catches a regression in that
contract fast, without a live backend. But per the review finding above, it
must not be the only guard for backend-execution correctness. Updated it to
handle both statement shapes (a `uids`-keyed REMOVE statement and a
`rows`-keyed upsert statement) with an explicit doc comment stating its
limitation and pointing at the two guards below, and added a
`TestTerraformStateStatementsEmitRemoveBeforeUpsert` unit test asserting the
ordering contract directly.

Chose to add a real-backend regression test to the committed suite
(`tfstate_canonical_writer_stale_attrs_live_test.go`), following this
package's existing `_live_test.go` convention exactly:
`openBoltTestRunner`/`boltTestExecutor` (shared with
`edge_writer_retract_repo_live_test.go` and others), gated on
`ESHU_CYPHER_BOLT_DSN`, skipped by default, opt-in against a real running
Bolt-speaking backend (NornicDB or Neo4j-compatible). It runs the exact
production `buildTerraformStateStatements` output through a real backend
for both projections and asserts both the stale-attribute removal and the
uncorrupted `evidence_source` value -- the same two assertions proven above
against the throwaway harness, now available as an opt-in CI/local lane
without needing a separate ad hoc harness.

Did **not** extend the golden-corpus cassette to re-project the same UID
twice in this round. That would require establishing whether/how the B-7
cassette-replay pipeline models a same-UID re-observation at all within one
gate run (the `eshu-golden-corpus-rigor` skill describes one fixed-input
replay per run, not a temporal sequence) -- a real architectural question in
its own right, not a small data edit, and not one to answer under this
finding's time pressure against a fixture shared by every PR. The
committed live test already provides real-backend proof without that
additional, less-understood fixture surgery. Flagging this as a deliberate
scope decision, not an oversight: a cassette-level refresh-replay guard
remains a legitimate follow-up if the golden-corpus pipeline turns out to
support it cleanly.

### Fast-path and performance measurement

The upsert statement's own Cypher text is byte-identical to its pre-round-8
shape, so its fast-path eligibility (`parseUnwindMergeChainPattern`) is
unaffected by this fix -- fast-path selection is determined by parsing a
statement's own text, and the two statements are parsed and routed
completely independently. The REMOVE statement is not UNWIND-based at all
(a single `WHERE r.uid IN $uids` list-membership match, not a per-row loop),
so it is one added statement per (allowlisted type, batch) -- not a
per-resource cost multiplied across the corpus.

Quantified the REMOVE statement's own added cost with the same throwaway
harness used for the correctness proof (in-process `StorageExecutor` timing,
no network -- production cost additionally includes one Bolt round trip per
added statement, not captured here), `-benchtime=100x -benchmem`, Apple M1
Max, 500-UID/500-row batches (matching `DefaultBatchSize`):

| Benchmark | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkUpsertOnly_500` (upsert alone, unchanged shape) | 4,489,910 | 3,852,073 | 62,732 |
| `BenchmarkRemoveOnly_500` (the new REMOVE statement alone) | 14,930,090 | 9,782,397 | 142,966 |
| `BenchmarkRemoveThenUpsert_500` (shipped: REMOVE then upsert) | 37,566,329 | 25,665,898 | 363,031 |

Per 500-row/UID batch: the added REMOVE statement costs ~29.9μs/UID
in-process (`14,930,090 / 500`); the shipped combined sequence adds
~66.2μs/row over the upsert-alone baseline (`(37,566,329 - 4,489,910) /
500`), somewhat more than the isolated REMOVE-only number, consistent with
ordinary run-to-run variance on a shared machine rather than a distinct
cost. This is in-process Go/data-structure cost only, not end-to-end
graph-write latency; the real production cost per (allowlisted type, batch)
additionally includes one extra Bolt round trip, bounded by the number of
allowlisted-type batches in a materialization, not the total resource
count. No live Compose stack was stood up to measure the network-inclusive
cost in this round; the in-process numbers above are the theory-proof this
repo's Prove-The-Theory-First discipline requires before shipping, not a
substitute for a full-corpus remote proof if the coordinator wants one.
