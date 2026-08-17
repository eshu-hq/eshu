# Evidence: rationale edges read the collector's path key (#5998)

## What changed

`ExtractRationaleEdgeRows` read `payload["path"]` for `target_path`. No
content-entity fact carries that key. `contentEntityFactEnvelope`
(`go/internal/collector/git_content_fact_envelopes.go`) emits `relative_path`,
and every sibling extractor reads that one — `semantic_entity_materialization`,
`sql_relationship_embedded_query`, and `sql_relationship_materialization` (twice).

So `target_path` was the empty string on every rationale edge projected from real
collector output. The extractor now reads `relative_path`.

## Why it matters

`target_path` is not provenance decoration. Each edge intent hashes the
repo-relative `target_path` together with its edge identity, so same-file edges
still have distinct partition keys. Delta retraction uses a separate shape:
`buildRationaleDeltaScope` combines the repository `local_path` with changed or
deleted relative paths and emits repository-qualified delta retract paths for
matching canonical `target.path` values. Conflating those two path shapes would
either lose the partition anchor or miss stale-edge retraction.

## How it survived

Every test exercising the extractor supplied `path` — the key the extractor
wanted, not the key the collector sends. Two of them asserted `target_path`
came back populated, which it did, because the fixture handed it the key it was
already reading. A proof built from the same misunderstanding as the code cannot
find the bug.

Those two fixtures (`rationale_edge_materialization_test.go`) now carry the
production shape and fail against the old read instead of passing beside it.

## No-Regression

The proof now covers the collector-to-reducer admission path, not only the map
lookup. Every admitted full and delta repository generation adds one
`shared_followup` fact for `rationale_materialization`. The reducer performs a
bounded repository/content-entity fact-kind load, scans the returned facts,
persists one refresh intent per admitted repository, and persists one edge
intent per current rationale edge. A zero-rationale full generation still emits
the refresh so stale EXPLAINS edges can be removed; a deletion-only delta emits
one refresh with the exact repository-qualified deleted path.

The 12-fact rationale cassette, a saved fact stream used for replay, contains
one typed repository, five typed Python files, five valid content entities, and
the production-shaped follow-up. Its five source files pass through the native
Python parser and derive exactly three EXPLAINS edges. Duplicate WHY, empty
TODO, lowercase why, unsupported CAVEAT, blank-line-detached FIXME, and a
no-comment function derive none. Malformed-envelope and precedence guards stay
in hermetic reducer tests rather than inventing collector-unreachable facts.

Focused proof on this branch, with exit codes captured directly:

- `go test ./internal/ifa ./internal/reducer ./internal/collector ./internal/replay/schema -count=1`
  — exit 0 (`ifa 8.027s`, `reducer 3.986s`, `collector 11.278s`,
  `replay/schema 1.144s`).
- The collector follow-up helper benchmark, five one-second samples, measured
  `1.462–1.481 us/op`, `2315 B/op`, and `34 allocs/op`.
- Full/delta collector generation benchmarks, three one-second samples,
  measured `50.977–53.721 us/op` for full and `27.053–32.032 us/op` for delta.
- The production `Handle` path over 5,000 content entities uses
  `ListFactsByKind` while a deliberately failing `ListFacts` fallback proves the
  bounded loader path. Ten 500 ms samples measured the zero-rationale case at
  `260.505–282.743 us/op`, `3997–4002 B/op`, and `52 allocs/op`; it emits exactly
  one refresh intent. The current positive case measured
  `13,570,020–15,365,751 ns/op`, `16,610,794–16,611,761 B/op`, and
  `225,154–225,160 allocs/op`; it emits exactly 5,001 intents.
- A separate detached-worktree A/B used the same nested-comment benchmark
  fixture for the pre-change and current positive paths. Benchstat measured
  `15.67 ms/op` before and `14.06 ms/op` current (`-10.27%`, `p=0.000`, `n=10`),
  with unchanged bytes and allocation counts at the displayed precision. The
  runs were not interleaved, so this is no-regression evidence, not a speedup
  claim.

These are in-process measurements. The benchmark uses an in-memory
`ListFactsByKind` test double and includes the interface call, extraction, and
durable intent construction. It excludes the Postgres query and decode, intent
persistence, and graph-backend I/O. The small replay cassette is correctness
evidence rather than repo-scale performance evidence; the live matrices below
cover the supported backend and durable recovery path.

## Supported-backend live proof

The post-rebase determinism matrix passed at N=1, N=2, and N=4 on commit
`25f325fac347ef984be2546fb20c278d1d50253f`. This is the first accepted run
after #6137 changed the materialized-edge identity oracle. The invoking shell
recorded `LIVE_5998_DETERMINISM_RC=0` after the command returned. The retained
transcript SHA-256 is
`fd9bb6155a0703cfb8242c60db4826a918a1dd0a8f0364215fefc0de9e399f5f`.

Every baseline cell matched the exact three DOCUMENTS edges and the three
complete EXPLAINS records. After the delta, every cell kept the DOCUMENTS set
exact and matched the exact one-record Invoice edge set plus the required
generation-2 Charge node. The matrix checked the exact three-record baseline
and exact one-record delta sets, not only endpoint identities. The
baseline rationale durable tuple was `1|1|0|4|3|1|4|0`; the delta tuple was
`1|1|0|1|0|1|1|0`. Each drain ended with zero residual work, zero nonterminal
shared intents, and zero dead letters. All three post-delta graph digests were
`aa0904cc09da0b95bf78a0f27dd1b5b0e2aec15c371e0077edb81312360a4998`.
Cell wall times were 15 s, 18 s, and 18 s.

The 15-cell fault matrix passed after the rebase on commit
`25f325fac347ef984be2546fb20c278d1d50253f`. All fifteen cells started from a
fresh stack. The invoking shell recorded `LIVE_5998_FAULT_RC=0` after the
command returned. The retained transcript SHA-256 is
`7ca78272a54664b6e6f153722e5f4e321b7ebecb296eeb8e19becce3b3361140`.

The baseline cell recorded four domain-specific retry counters. The
domain-specific fault-free documentation and rationale retry baselines were
both `0`.

The documentation worker-kill cell first matched its exact three DOCUMENTS
edges, then observed one claimed row whose ACK backend was blocked by the
session holder. The attempt-1 lease snapshot remained byte-identical after the
host process and orphaned ACK backend stopped, while the holder still fenced
the row. The replacement worker then converged to the same exact records with
zero residual work and zero dead letters. The documentation retry count was
`1`, strictly above baseline `0`. Its wall time was 74 s.

The rationale worker-kill cell observed one blocked claimed/running rationale
row. Its replacement worker recovered the exact three complete EXPLAINS records
and durable tuple `1|1|0|4|3|1|4|0`; the rationale retry count was `1`, strictly
above baseline `0`. The cell took 74 s.

The rationale one-shot graph-write cell recovered the same exact records and
tuple in 11 s. Its strict durable marker check for the targeted EXPLAINS MERGE
returned success. That check is silent on success, so the transcript does not
print the marker contents; the cell's continuation to its exact graph, digest,
and terminal assertions is the recorded result.

Fourteen cells produced the fault-free digest
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052`.
The delta-retract cell produced its expected changed-graph digest
`b1ef9c70490174a4f3893568709063c7d0c1f51591efef165f031b814ad612c2`.
The terminal cell summary recorded these wall times:

| Cell | Wall time |
| --- | ---: |
| baseline | 19 s |
| deltaretract | 53 s |
| duplicatedelivery | 14 s |
| expirelease | 10 s |
| failgraphwrite | 72 s |
| failgraphwritecodecalls | 11 s |
| failgraphwritedocumentation | 72 s |
| failgraphwriterationale | 11 s |
| failgraphwritesql | 10 s |
| killworker | 70 s |
| killworkercodecalls | 82 s |
| killworkerdocumentation | 74 s |
| killworkerrationale | 74 s |
| killworkersql | 72 s |
| restartbackend | 12 s |

Every cell ended with zero residual work and zero dead letters. The final host
and Compose residue check was separate from the retained transcripts. A
read-only check of the exact Compose project labels after both runs found zero
containers, volumes, and networks for `eshu-ifa-determinism-34547` and
`eshu-ifa-fault-injection-48958`; no process command line retained either
project name or either live-gate driver.

The earlier accepted determinism run on
`fb245bd3af9375b2cd86b23ec52cdd0550791088` (transcript SHA-256
`7a140b96e26f994357c2ecafa820f8b60dfdd9dea087280f53df78e4dd9319bc`)
and fault run on `69de2944287e5c5ca8f5ec68160596628902aa70`
(transcript SHA-256
`0323ada77cf58d3b53ab6ed875bb9d817e9fa0fd8f5ed793804889c3d862a234`)
are superseded historical evidence from before the #6137 rebase. They remain
useful for chronology, but they do not prove the final assertion oracle.

The earlier live runs on commit `48dc7ebafcb80f82bf3cf4edbc28ce49fb1f442e`
are diagnostic evidence, not accepted proof. The initial combined fault run on
commit `fb245bd3af9375b2cd86b23ec52cdd0550791088` ended with
`LIVE_COMBINED_FAULT_RC=1` and transcript SHA-256
`b0b59991c460b21facd98382ddfb650be59ee27f0bdacc19fc200b1e6084c08f`.
It exposed the original documentation worker-kill false green: the exact graph
was present, but documentation materialization showed no retry above baseline.
The next three post-ACK-barrier runs exposed narrower gate defects in
PostgreSQL boolean parsing, Bash output propagation, and `EXTRACT` SQL syntax.
Focused regressions cover all four failures.

The PostgreSQL 18 ACK-barrier lifecycle probe is a separate, environment-gated
proof. It ran before the accepted fault matrix, but the full matrix does not
invoke it automatically.

These runs prove the supported Postgres/shared-intent/graph path for the small
fixture. They do not turn the fixture into a repository-scale performance
benchmark.

## Behavior change worth naming

This populates a field that was empty in production, so it is not invisible:

- `target_path` on projected rationale edges goes from `""` to the entity's
  repo-relative path.
- `rationaleFilePartitionKey(repoID, targetPath, edgeIdentity)` therefore hashes
  a different value, so existing rationale intents redistribute across the
  partition ring on the next projection.
- The redistribution is a one-time reshuffle, not a correctness risk: the key
  still mixes the repo first and the per-edge identity last, so distinct edges
  stay collision-free and two repositories still cannot collide. What changes is
  that the key finally includes the repo-relative file path.

The edge row keeps that repo-relative path. The repository fact separately
qualifies changed/deleted paths before the delta retract matches canonical
`target.path`; tests pin both shapes.

## Observability

No telemetry schema, span, or concurrency setting changes. The unconditional
marker schedules one rationale handler execution per admitted repository
generation. That handler emits one refresh intent plus the current edge intents;
shared workers may then process several partition-scoped cycles. Handler
completion already reports `intent_count`, `edge_count`, and `repo_count`; the
shared worker retains its existing domain, partition, completion, and
dead-letter signals.

## Reproduce

```
cd go && go test ./internal/ifa -run TestRationaleFamily -count=1
cd go && go test ./internal/reducer -run 'TestExtractRationaleEdgeRows|TestRationaleHandler' -count=1
```

The guard bites, in this order:

1. Switch `odu:ifa-rationale-family` to the collector's `relative_path` shape
   while the extractor still reads `path` — RED, every expected edge `MISSING`,
   because `target_path` comes back empty.
2. Apply the extractor fix — GREEN.
3. Revert the extractor fix — RED again, same failure.
