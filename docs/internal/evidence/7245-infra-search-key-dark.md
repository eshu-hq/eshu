# #7245 PR-1: infra search key, dark write side

Issue #7245 (infra resource search has a flat warm floor) is cut into two PRs.
This one (PR-1) adds the data, schema, writers, and backfill. It changes no
reader: `go/internal/query/infra.go` and `infra_scope.go` are untouched. PR-2
moves the Neo4j search onto the key after a cold-plan check.

## What this adds

- `infra_search_key`: the twelve properties
  `deployment.InfraResourceFreeTextPredicate` reads, joined with U+001F, built
  in Cypher by one clause (`search.Clause`) from the node's own properties in
  the same statement that writes them. Each part is the property when it is a
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
  batched, idempotent backfill and an index-state check before the schema
  marker.

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
  `ESHU_INFRA_SEARCH_KEY_SCALE=1`): 181,929 unkeyed nodes in the label shape
  measured in the QA environment (TerraformVariable 65,257, TerraformResource
  35,018, TerraformLocal 30,414, CloudResource 180, 2,220 on each other infra
  label) keyed in 13.9 s (13.940 s; two earlier runs on the same shape 10.4 s
  and 10.9 s), with the index ONLINE afterwards. TerraformVariable took 3.4 s.
  The idempotent re-run, which reads every node and writes none, took 0.96 s.
- Writer clause overhead, wall time: 5,000 TerraformVariable entity rows
  through the real canonical writer, alternating dialects on fresh nodes:

  | Rep | Keyed (Neo4j dialect) | Unkeyed (NornicDB shape) |
  | --- | ---: | ---: |
  | 0 | 2.828 s (first run of the new statement text, cold plan) | 0.745 s |
  | 1 | 0.817 s | 0.637 s |
  | 2 | 0.583 s | 0.567 s |

  Warm, the keyed write costs between 0.016 s and 0.180 s more per 5,000 rows
  on this host; the spread is host noise, not a resolved delta.
- Writer clause overhead, deterministic: `PROFILE` of one 500-row
  `canonicalNodeEntityUpsertTemplate` batch on TerraformVariable reports 3,000
  db hits unkeyed and 19,000 keyed: +32 db hits per row. The clause reads the
  twelve properties for the overflow flag, the bound check, and the key value,
  and writes two properties that maintain the FULLTEXT index entry. The
  measured wall-time cost is small next to the rest of a canonical write, so
  the clause keeps the simpler single-SET form. A form that computes the
  joined key once would hold an over-cap key in transaction state before
  nulling it; with a FAILED index as the failure mode, that is not taken
  without its own proof.
- Correctness on the same backend (`TestLiveInfraSearchKey`, 9.1 s): a 40 KB
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
check `search.CountUnkeyed` before it trusts the key. The NornicDB fingerprint
does not move.

## Not checked here

- NornicDB live legs beyond the probes above; the NornicDB statements are
  byte-pinned to main by unit goldens instead.
- The QA environment; any read latency (PR-2).
