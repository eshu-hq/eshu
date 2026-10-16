# #7245 PR-1: infra search key, dark write side

Issue #7245 (infra resource search has a flat warm floor) is cut into two PRs.
This one (PR-1) adds the data, schema, writers, and backfill. It changes no
reader: `go/internal/query/infra.go` and `infra_scope.go` are untouched. PR-2
moves the Neo4j search onto the key after a cold-plan check.

## What this adds

- `infra_search_key`: the twelve properties
  `deployment.InfraResourceFreeTextPredicate` reads, joined with U+001F, built
  in Cypher by one clause (`search.Clause`) from the node's own properties in
  the same statement that writes them. The clause computes the joined value
  once into a row variable (`WITH alias, <joined> AS infra_search_joined_key`)
  and sets both properties from it, so an over-cap value never reaches the
  node. Each part is the property when it is a
  non-null string, and the empty string otherwise. The resource type is
  `coalesce(resource_type, data_type)`, as in the predicate.
- `infra_search_ovf`: always written by the clause. It is `'T'` when the
  joined key is longer than 8,000 code points (the key is then null), and
  `'F'` otherwise. A node without it was never keyed.
- `infra_search_key_ft`: one Neo4j-only keyword FULLTEXT index over both
  properties on the 27 infra labels, `eventually_consistent: false`.
- Writers: the canonical writer (five entity templates for infra labels, three
  Terraform state upserts), the CloudResource writer, and the EC2 instance
  writer append the clause through `search.Statement`. Production wiring in
  bootstrap-index, ingester, projector, and reducer passes the dialect resolved
  from `runtimecfg.LoadGraphBackend`.
- Bootstrap (Neo4j only): an oversized-key probe before graph DDL, then a
  batched, idempotent backfill, a `db.awaitIndex` wait, and a required
  `ONLINE` state (read after the wait, and decisive even when the wait
  errored) before the schema marker. Schema adoption runs the same gate
  before its marker.

## Design decisions

These three decisions replace the design first claimed on #7245. The query
shapes were measured by the #7245 cold-plan shim on the pinned
neo4j:2026-community 2026.08.1, on a fixture shaped like the QA environment
(181,953 nodes across the 27 labels).

1. **E2 rejected: 27 TEXT + 27 RANGE indexes behind 27 UNION branches.** Every
   27-branch shape (E2, E2r, E2u) replans in 1.7-2.3 s whenever `$query` falls
   in a new string-length bucket, because the plan cache is keyed partly by
   that bucket. A cold p95 under 1 s is out of reach for that shape, and
   prewarming every bucket is not a fix.
2. **E3d chosen.** One keyword FULLTEXT index, `infra_search_key_ft`, over
   `[infra_search_key, infra_search_ovf]` on the 27 labels
   (`eventually_consistent: false`); a single `db.index.fulltext.queryNodes`
   branch with `infra_search_key:*<escaped query>* OR infra_search_ovf:T`; the
   unchanged twelve-field predicate re-applied to every candidate; and
   `RETURN DISTINCT ... ORDER BY name LIMIT $limit` with no inner top-k. An
   inner top-k breaks the de-duplication at the page edge (13 rows against
   the correct 17). The read side is PR-2; this PR builds what it reads.
3. **The earlier FULLTEXT rejection is answered, not ignored.** A 40,000-byte
   value written to a keyword FULLTEXT index fails its commit, needs a Neo4j
   restart, and leaves the index FAILED. Three things keep that from
   happening: the writer clause stores no key over 8,000 code points (at most
   32,000 bytes, below Lucene's 32,766-byte term limit); two census tests keep
   every writer on that clause and keep anything else from writing the key or
   flag; and bootstrap refuses graph DDL or adoption while any key passes the
   cap. The overflow flag is the string `'T'`/`'F'`, not a boolean, because a
   FULLTEXT index indexes only strings.

## Backend facts behind the design

Measured on the pinned images: `neo4j:2026-community@sha256:eabfbb04...`
(server 2026.08.1) and
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`,
through the HTTP transaction endpoint.

| Probe | Neo4j 2026.08.1 | NornicDB fix-500-e022384c |
| --- | --- | --- |
| `RETURN null IS :: STRING` | `true` (nullable type) | the literal text `null IS :: STRING` |
| `RETURN [1] IS :: STRING` | `false` | the literal text |
| `size('é😀')` | `2` (code points) | `2` |
| list comprehension + `reduce` join | correct | a mangled list |
| full clause in a `MERGE ... SET` | key and flag written | flag holds the clause text, other properties intact |

So each part guards with `IS :: STRING NOT NULL`, the cap counts code points,
and NornicDB never receives the clause: its dialect returns every statement
byte for byte. SHA-256 goldens computed from the origin/main constants at
261ca79b8 pin the eight NornicDB writer statements.

The cap is load-bearing. The keyword analyzer makes the whole key one Lucene
term. The arbiter reproduced a 32,768-byte key written past the cap: the
commit failed after the node was written, every later transaction reported a
critical error until restart, and the index came back FAILED ("immense
term ... 32766"). 8,000 code points is at most 32,000 bytes.

## Performance Evidence

Performance Evidence: pinned neo4j:2026-community 2026.08.1, disposable
container, full production Neo4j schema applied, `infra_search_key_ft` ONLINE,
on a shared host with load average 18-30 (wall times are indicative, not
gating; db hits are deterministic).

- Backfill (`TestLiveInfraSearchKeyBackfillScale`,
  `ESHU_INFRA_SEARCH_KEY_SCALE=1`, run after the single-computation change):
  181,929 unkeyed nodes in the label shape measured in the QA environment
  (TerraformVariable 65,257, TerraformResource 35,018, TerraformLocal 30,414,
  CloudResource 180, 2,220 on each other infra label) keyed in 11.1 s
  (11.112 s; earlier runs of the inline form on the same shape took 10.4 s,
  10.9 s, and 13.9 s), with the index ONLINE afterwards. The idempotent
  re-run, which reads every node and writes none, took 1.3 s.
- Writer clause overhead, wall time: 5,000 TerraformVariable entity rows
  through the real canonical writer, alternating dialects on fresh nodes:

  | Rep | Keyed (Neo4j dialect) | Unkeyed (NornicDB shape) |
  | --- | ---: | ---: |
  | 0 | 2.586 s (first run of the new statement text, cold plan) | 0.766 s |
  | 1 | 0.738 s | 0.573 s |
  | 2 | 0.595 s | 0.997 s |

  Warm, the two dialects are within this host's noise; no wall-time delta is
  resolved.
- Writer clause overhead, deterministic: `PROFILE` of one 500-row
  `canonicalNodeEntityUpsertTemplate` batch on TerraformVariable, full schema
  and the 27-label index present: 3,000 db hits unkeyed and 10,500 keyed, +15
  per row. The same harness measured 19,000 (+32 per row) for the earlier form
  that evaluated the join three times inline; an independent review rerun
  measured 18,000 for that form.
- Why the join is computed once: a focused `PROFILE` on a single-label index
  with only the TerraformVariable uid constraint (500 rows, fresh nodes each):
  4,000 db hits unkeyed, 24,500 inline (+41 per row), 12,500 with the
  row-variable form (+17 per row). Keys and flags were identical on all 500
  node pairs. The joined value lives only in the row variable, so no over-cap
  value is ever written to a node; a 40 KB value still overflows to `'T'`
  with no key, and the index stayed ONLINE.
- Correctness on the same backend (`TestLiveInfraSearchKey`, 9.5 s with the
  single-computation clause): a 40 KB
  name and a 70 KB config_path written through the real writers succeed with
  `infra_search_ovf = 'T'` and no key; an 8,000-code-point key of 4-byte
  characters (about 32,000 bytes) is stored with `'F'`; 8,001 overflows; the
  index stays ONLINE and `infra_search_ovf:T` finds every overflowed node.
  With the writers switched to the NornicDB dialect the same test fails on
  every key assertion.

The read path is unchanged in this PR, so no read latency is claimed. The
#7245 acceptance (cold and warm p95 under 1 s on the QA arguments) stays open
for PR-2.

## Observability Evidence

Observability Evidence: `eshu-bootstrap-data-plane` logs
`bootstrap.graph.infra_search_key.readiness` (`oversized_keys`, `max_chars`)
before graph DDL and `bootstrap.graph.infra_search_key.oversized` at ERROR
with a runbook pointer when it refuses;
`bootstrap.graph.infra_search_key.label_backfilled` per label (`label`,
`nodes_keyed`, `overflow_count`, `batch_size`, `duration_seconds`);
`bootstrap.graph.infra_search_key.backfilled` once; and
`bootstrap.graph.infra_search_key.index_state` (`state`, `population_percent`)
plus `bootstrap.graph.infra_search_key.index_failed` at ERROR. The binary
registers no meter provider, so these structured events are its operator
signal, like its existing `bootstrap.graph.*` events; a counter would never be
exported. Writer statements keep the existing `InstrumentedExecutor` spans and
`eshu_dp_neo4j_query_duration_seconds`; the keyed statements stay readable to
the index-key guard (`graph.UnanalyzedIndexWrites` reports nothing for them),
so `eshu_dp_graph_index_key_guard_unanalyzed_total` does not move.

## Compatibility

The Neo4j schema fingerprint moves; the previous tip stays compatible (the
index changes no MERGE or MATCH identity). A writer on the previous release
does not maintain the key during a rolling upgrade, so PR-2 must re-run the
backfill (which rewrites any key that disagrees with its node's fields) and
check `search.CountUnkeyed` before it trusts the key. The reader must also
check at read time that `infra_search_key_ft` is `ONLINE`: a write that
bypasses the clause can leave it `FAILED`, and nothing in Eshu reports that
until the next bootstrap. The NornicDB fingerprint does not move.

## Not checked here

- NornicDB live legs beyond the probes above; the NornicDB statements are
  byte-pinned to main by unit goldens instead.
- The bootstrap binary end to end against a live Neo4j and Postgres. Its
  probe, backfill, `db.awaitIndex`, and state statements were each run on the
  pinned Neo4j, and the control flow is unit-tested.
- The QA environment; any read latency (PR-2).
