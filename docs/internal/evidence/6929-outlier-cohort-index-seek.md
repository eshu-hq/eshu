# #6929 — outlier cohort-read index seek

Issue #6929 reports that the convention-outlier cohort sweep does not scale
with repository size. The root cause: `outlierScopePredicates`
(`go/internal/query/codequery/outlier.go`) wrapped the repo-scope predicate
in `coalesce(member.repo_id, '') = $repo_id` on the anchoring
`MATCH (member:Function)` read `BuildOutlierCohortsCypher` renders for all
three cohort sources (interface, router, package). `coalesce()` defeats the
`function_repo_id` RANGE index, forcing a `NodeByLabelScan` of every
`Function` node in the whole graph — a cost proportional to total platform
Function population, not to the size of the repository being swept. The fix
drops the `coalesce()` wrapper: `member.repo_id = $repo_id` is safe because
`Function.repo_id` is proven non-null on both the ops-qa deployment and this
proof's own seeded graph, so the two forms select identical rows.

## Identity

- Fix commit (this branch): `ea6a69f8d` (`go/internal/query/codequery/outlier.go`,
  `outlier_cypher_test.go`, `go/internal/queryplan/testdata/statement-builders.yaml`).
- Live-proof commit: `26d38a026` (`outlier_repo_scale_live_test.go`,
  `specs/live-tests.v1.yaml`).
- Base: `origin/main` `049be7161`.

## ops-qa before/after (diagnosis phase, read-only, no code change on that host)

Backend: Neo4j Community 2026.08.1, Cypher 25, slotted runtime. Corpus: 804
repositories, 539,933 total `Function` nodes, 349,186 `CALLS` edges.
`function_repo_id` confirmed `ONLINE` (RANGE index) throughout — the shipped
statement never used it because of the `coalesce()` wrapper, not because the
index was missing or unpopulated.

Package-cohort seed enumeration (`BuildOutlierCohortsCypher`,
`CohortPackage`), three repeated warm runs per statement, same repo per row:

| repo | Functions | shipped (`coalesce`, `NodeByLabelScan`) | fixed (bare equality, `NodeIndexSeek`) |
| --- | ---: | ---: | ---: |
| websites-php-youboat (`r_8946df89`) | 45,495 | 700 / 725 / 662 ms, 1,506,338 db accesses | 247 / 252 / 242 ms, 471,967 db accesses |
| portal-java-ycm (`r_a09c7db8`) | 44,991 | 582 ms, 1,563,839 db accesses | 228 ms, 528,964 db accesses |
| wordpress (`r_957cd853`) | 42,714 | 552 ms, 1,456,170 db accesses | 201 ms, 419,018 db accesses |
| (`r_144b09b9`, smallest repo) | 1 | 484 / 476 / 447 ms | 1 / 0 / 0 ms |

The 1-Function repo row is the sharpest proof that the shipped statement's
cost is decoupled from the queried repository's own size: it pays the same
~450-730 ms scan of the whole 539,933-node `:Function` population that the
45,495-Function repo pays, because both statements scan every `Function` in
the graph before the `coalesce()`-wrapped filter drops the non-matching
rows. Rows-equal: full sorted row dump of shipped vs. fixed for
websites-php-youboat, 45,495 rows, byte-identical (`diff` produced no
output). Full narrative, EXPLAIN/PROFILE plan text, and the growth-threshold
arithmetic (~8-9M total Functions before this defect alone reaches the 10s
per-statement deadline at ops-qa's measured per-node cost) are recorded in
the diagnosis this fix acts on.

## Repo-scale Neo4j live proof (this PR, seeded graph)

`go/internal/query/codequery/outlier_repo_scale_live_test.go`
(`TestLiveOutlierRepoScaleNeo4j`, env-gated by `ESHU_OUTLIER_NEO4J_LIVE=1`,
registered in `specs/live-tests.v1.yaml` as `scheduled`) seeds a disposable
Neo4j container to platform scale, applies the real production schema
(`graph.EnsureSchemaWithBackendStrict`, including `function_repo_id`), and
proves the fix against the real backend rather than only the diagnosis's
read-only ops-qa access.

Container: `docker run -d --name eshu-6929-neo4j -p 17929:7687 -e
NEO4J_AUTH=neo4j/eshu-6929-pass neo4j:2026-community`, image digest
`sha256:91fb0bf237c41b7b3dcbe84703aa0b82e0d7d067b16e1c8ab21f03fc679edf4e`,
Neo4j Kernel `2026.09.0`, Cypher versions `5`/`25`. `function_repo_id`
confirmed `ONLINE`, 100% populated (`SHOW INDEXES`) before every measured
read.

Seeded shape: 45,061 `Function` nodes in the swept repository (901 files x
50 members each, `CONTAINS`-anchored, plus an 11-node signal fixture with
real `CALLS`/`IMPLEMENTS`/`HANDLES_ROUTE` edges so the sweep produces real
findings, not an empty scan) and 460,000 `Function` nodes across 460 other
repositories (no relationships — the point of the fix is that these rows
are no longer scanned at all). Verified via `MATCH (f:Function) WHERE
f.repo_id = $repo_id RETURN count(f)` (45,061) and the whole-label count
(505,061), both meeting the handoff's floors (>=45,000 in-repo, >=450,000
elsewhere).

### (a) Old vs. new statement text, rows-equal

For every one of the three cohort sources (interface, router, package), the
pre-fix statement text (`coalesce(member.repo_id, '') = $repo_id`,
reconstructed verbatim in the test as `legacyBuildOutlierCohortsCypher` for
comparison only — production no longer emits it) and the current
`BuildOutlierCohortsCypher` output were run against the same seeded graph
and their row sets sorted and compared. All three sources returned
identical, nonempty row sets (package: 45,061 rows; interface: 3 rows;
router: 4 rows). `TestLiveOutlierRepoScaleNeo4j` fails on any row-set
mismatch or an all-empty old statement; it passed.

### (b) EXPLAIN plan shape

| source | old plan (coalesce) | new plan (bare equality) |
| --- | --- | --- |
| package | `...Filter(coalesce...)Expand(All)...NodeByLabelScan` | `...Filter...Expand(All)...NodeIndexSeek RANGE INDEX member:Function(repo_id)` |
| interface | `...DirectedRelationshipTypeScan` (unchanged) | `...DirectedRelationshipTypeScan` (unchanged) |
| router | `...DirectedRelationshipTypeScan` (unchanged) | `...DirectedRelationshipTypeScan` (unchanged) |

Matches the ops-qa diagnosis exactly: only the package-cohort read's anchor
plan changes (`NodeByLabelScan` -> `NodeIndexSeek` on `function_repo_id`);
interface and router already anchor on the rare relationship type
(`IMPLEMENTS`/`HANDLES_ROUTE`) regardless of the predicate form, so their
plans are identical before and after and the fix carries no regression risk
for those two sources.

### (c) Wall-time: cohort-read only, and the full production sweep

The package-cohort read's own wall-time delta at this proof's total corpus
size (505,061 Functions, close to ops-qa's 539,933) was measured directly
(cold run dropped, warm runs sorted, median reported) in two separate test
executions on the same seeded data:

| run | package cohort-read warm median, old | new | delta (old-new) |
| --- | ---: | ---: | ---: |
| contended (another branch's `make pre-push` running concurrently on the same host) | 335.4 ms | 332.5 ms | 2.8 ms |
| clean (uncontended) | 166.4 ms | 157.6 ms | 8.7 ms |

This delta is far smaller in absolute terms than ops-qa's ~300-500 ms
per-statement saving at a comparable total corpus size. The plan-shape fix
(`NodeByLabelScan` -> `NodeIndexSeek`) is identically proven on both hosts;
what differs is the absolute per-node scan cost, which is far cheaper on
this proof's disposable, fully-cached, empty-container local dataset than
on ops-qa's real deployment. The mechanism this fix removes — cost coupled
to total platform Function population instead of the swept repository's
size — is the same on both hosts; only the current severity differs by
environment, and ops-qa's PROFILE numbers above remain the more
representative real-world figure.

The full production sweep (`CodeHandler.assembleOutlierTrack`, the current,
fixed code — the pre-fix code path no longer exists to replay end-to-end,
so "before" is not separately measured for the full sweep; only the
isolated cohort-read delta above is) was timed cold plus ten warm calls in
each of the two runs:

| run | cold | warm p50 | warm p95 |
| --- | ---: | ---: | ---: |
| contended | 3.10 s | 1.70 s | 2.19 s |
| clean | 566 ms | 504 ms | 542 ms |

Phase breakdown (seed-enumeration reads / CALLS-fanout over the union of
cohort members, chunked 50 ids per statement / mediation+hydration+assembly
remainder), measured immediately after the ten-warm-call series in each run:

| run | seed reads | CALLS-fanout (~902 chunks over 45,058 members) | remainder |
| --- | ---: | ---: | ---: |
| contended | 242 ms | 1,009 ms | 449 ms |
| clean | 158 ms | 317 ms | 29 ms |

**The full sweep does not reliably fit inside a 1 s budget at this scale.**
It was under budget in the clean run and over budget in the contended run
for the identical query and identical seeded data — the difference is host
load, not the query shape. In both runs the CALLS-fanout stage (the
per-diagnosis NOT_CHECKED item: `readOutlierCalleeEdges`'s 50-id-per-UNWIND
chunking, `wrapperEvidenceKeyBatchSize` in `wrapper_bypass_track.go`, driven
here across ~902 chunks for 45,058 distinct cohort members) is the largest
or co-largest phase: 59% of the contended-run total, 63% of the clean-run
total. This confirms the diagnosis's flagged hypothesis that the chunk
*count*, not any single chunk's server-side execution cost, is a real and
now-measured contributor to sweep wall time at repository scale — this PR
does not change that behavior (out of scope per the handoff: raising the
batch size needs its own proved-first theory, not a guess folded into this
fix), and the finding is reported here for the coordinator's decision
rather than acted on.

Findings sanity: `assembleOutlierTrack` returned 3 findings each run
(interface `iface-a`/`guard`/outlier `m-b1`; package `signalpkg`
(`api`)/`guard`/outlier `h-5` mediated through `wrap`; package
`signalpkg/impl`/`guard`/outlier `m-b1`), with one `below_min_cohort`
suppression (the single-member `signalpkg/other` file cohort, correctly
dropped under `MinCohortSize=3`). The findings route
(`POST /api/v0/code/divergence/findings`) served HTTP 200 with the same
data through the real `Mount`ed handler.

## NornicDB

Not measured here: the owner rule for graph-read proof is Neo4j, and
`BuildOutlierCohortsCypher`'s comment ("the NornicDB and Neo4j texts are
identical: a repo-wide enumeration has no uid anchor to dialect-split on")
is unchanged and still true after this fix — `backend` remains unused in
that builder. The same class of fix is expected to help NornicDB too, since
both dialects executed the identical `coalesce()`-wrapped text before this
change; this is carried forward as NOT_CHECKED, matching the diagnosis's own
open item.

## Performance Evidence

Performance Evidence: `BuildOutlierCohortsCypher` (`CohortPackage` source),
Neo4j 2026.08.1 (ops-qa, read-only diagnosis) and Neo4j 2026.09.0 (this
proof's disposable container) both plan the pre-fix statement's anchoring
`MATCH (member:Function) WHERE coalesce(member.repo_id, '') = $repo_id` as
`NodeByLabelScan` over the whole `:Function` population (539,933 nodes on
ops-qa, 505,061 on this proof) and the fixed statement's
`WHERE member.repo_id = $repo_id` as `NodeIndexSeek RANGE INDEX
member:Function(repo_id)` scoped to the queried repository alone (45,495 /
44,991 / 42,714 rows on ops-qa's three measured repos, 45,061 rows on this
proof's seeded repo). ops-qa per-statement warm timing: 700/725/662 ms
(shipped) vs. 247/252/242 ms (fixed) on a 45,495-Function repo; 484/476/447
ms (shipped) vs. 1/0/0 ms (fixed) on a 1-Function repo — cost decoupled from
the swept repo's own size before the fix, coupled to it after. Row sets are
proven identical (byte-identical sorted dump, ops-qa; sorted row-string
comparison, this proof) on both hosts. `function_repo_id` (RANGE index on
`Function.repo_id`) was `ONLINE` and 100% populated before every measured
read on both hosts.

## Observability Evidence

No-Observability-Change: this fix changes only Cypher predicate text
(`outlierScopePredicates`); it adds no metric, span, log key, or status
field, and removes none. Existing route-level spans/logs for
`/api/v0/code/divergence/findings` and `/investigate` (already emitted by
the shared query-handler span helper) are unaffected and continue to record
the same attributes for this route before and after the change.
