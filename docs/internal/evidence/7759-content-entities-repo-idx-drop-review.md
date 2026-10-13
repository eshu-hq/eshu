# Evidence: #7759 — review dropping content_entities_repo_idx

Scope: `content_entities_repo_idx (repo_id)`, created by immutable migration
`004_content_store.sql`. Migration 164 (#7749) added
`content_entities_repo_language_type_idx (repo_id, language, entity_type)`,
which has the old key as a prefix. The issue asks whether the old index
should be dropped, and when. Proof items 1-4 below are the issue's own bar.

This is a **Prove-The-Theory-First** record. All DDL ran on a scratch
PostgreSQL 16.15 cluster (port 55439, `shared_buffers` default,
`SET jit = off`, warm repeats). No DDL ran on production.

## Verdict

Plans and write cost support the drop, but the migration cannot land as a
plain drop today. `004_content_store.sql` is shipped and immutable (#7002),
and it still creates the name, so a drop migration fails
`TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay`: every untracked
replay (the local supervisor replays the full tree on every start) would
blocking-rebuild the index from 004 and drop it again. Landing needs an
owner decision first: a replay-guard exemption that accepts per-restart
rebuild churn on untracked (local-only) databases, a bootstrap-model change,
or keeping the index. Proof item 4 records the exact mechanics.

## Proof item 1: which queries use the index

`pg_stat_statements` is not installed on the production reader, so the
statement census comes from source, and usage comes from `EXPLAIN` on the
shim. Production's 71 scans reading 18.7M tuples (about 263k tuples per
scan) fit full-repo-slice scans of the Q7 shape below.

| id | statement (`db.operation`) | predicates | shim plan today |
| --- | --- | --- | --- |
| Q1 | `search_entities_by_language_and_type` gate+page | repo + language + type | gate: repo_language_type |
| Q2 | `list_repo_entities` | repo + ORDER BY path | repo_path_start |
| Q3 | `list_repo_entities_by_type` | repo + type + ORDER BY path | repo_path_start |
| Q4 | `list_repo_entities_by_types` | repo + type ANY + ORDER BY path | repo_language_type |
| Q5 | `list_repo_k8s_select_candidates` | repo + K8sResource + ORDER BY path | k8s partial |
| Q6 | `search_entities_by_name` | repo + ILIKE + ORDER BY path | name trigram (rare) / repo_path_start (common) |
| Q7 | code-topic scoped one-term entity probe | repo + ILIKE OR + LIMIT | **repo_idx** (big repo); bitmap AND incl. repo_idx (small repos) |
| Q8 | fingerprint-reap EXCEPT leg | repo, entity_id projection | repo_entity (index-only) |
| Q9 | `search_entities_referencing_component` | repo + jsonb `?` + ORDER BY path | repo_path_start |
| Q10 | `dead_code_candidate_rows` (scoped) | repo + type + language + ORDER BY | repo_path_start |
| Q11 | entities-by-ids leg | repo + entity_id ANY | repo_entity |
| Q12 | `DELETE ... WHERE repo_id AND relative_path` | repo + path | repo_path_start (prefix) |

Only Q7 takes `content_entities_repo_idx` by default on the shim. Q7's
small-repo form ANDs it in a bitmap, which
`codeTopicTermsMaterialization`'s comment calls load-bearing (#7246).

## Proof item 2: plans with the old index hidden

Shim: 585,000 synthetic rows, 7 skewed repositories
(`repository:big` 240k rows with 45,600 JavaScript functions mirrors
repo-B; `repository:k8smono` 60k with 6,000 K8sResource mirrors the #5363
monorepo; php-heavy, medium, small, tiny). Full production btree set plus
both GIN trigrams, `VACUUM ANALYZE`. Battery Q1-Q11 plus supplements S1-S5
(each twice; warm repeat compared). Then `DROP INDEX
content_entities_repo_idx` and the same battery again.

Warm buffers are identical before/after for every query except Q7-big:

| query | before (warm) | after (warm) | shape |
| --- | --- | --- | --- |
| Q1 gate+page | 29 bufs, 0.10 ms | 29 bufs, 0.09 ms | identical |
| Q1b repo-empty | 3 bufs, 0.04 ms | 3 bufs, 0.03 ms | identical |
| Q1c corpus-empty | 3 bufs, 0.04 ms | 3 bufs, 0.02 ms | identical |
| Q2 | 10 bufs, 0.10 ms | 10 bufs, 0.10 ms | identical |
| Q3 | 14,452 bufs, 9.0 ms | 14,452 bufs, 12.3 ms | identical (time is cache noise) |
| Q4 | 4,399 bufs, 11.8 ms | 4,399 bufs, 18.0 ms | identical (time is cache noise) |
| Q5 | 11 bufs, 0.12 ms | 11 bufs, 0.14 ms | identical |
| Q6a rare | 111 bufs, 0.24 ms | 111 bufs, 0.19 ms | identical |
| Q6b common | 74 bufs, 0.06 ms | 74 bufs, 0.07 ms | identical |
| Q7 big | 8,512 bufs, 460.7 ms | 20,199 bufs, 241.8 ms | **index scan to seq scan** |
| Q8 | index-only, repo_entity | index-only, repo_entity | identical |
| Q9 | 35,228 bufs, 15.9 ms | 35,228 bufs, 15.7 ms | identical |
| Q10 | 65 bufs, 0.11 ms | 65 bufs, 0.11 ms | identical |
| Q11 | 10 bufs, 0.05 ms | 10 bufs, 0.03 ms | identical |
| S1/S2 small-repo probe | bitmap AND w/ repo_idx | bitmap AND w/ repo_language_type | AND survives, time unchanged |
| S3/S4 k8s at `random_page_cost=1.1` | k8s partial | k8s partial | identical |
| S5 reap tiny | repo_entity index-only | repo_entity index-only | identical |

The one flip, Q7-big: the planner drops the `content_entities_repo_idx`
Index Scan (239,920 rows filtered, 8,512 buffers, 460.7 ms warm) for a
Seq Scan (584,920 rows filtered, 20,199 buffers, 241.8 ms warm). Faster
here (sequential beats 240k random heap fetches), but O(corpus) instead
of O(repo). With `enable_seqscan = off` the planner finds a
BitmapAnd (name/source trigram OR, repo slice) at 6.4 ms and 284 buffers
in the after arm — an uncommitted knob-divergent probe (no raw output under
`7759/`, directional only). The optimal plan needs no repo_idx in either
arm, so the drop removes a costing trap, not a load-bearing path. Small-repo
probes keep the #7246 bitmap-AND shape on `repo_language_type` with no
time change (S1 0.85 to 0.88 ms, S2 0.56 to 0.58 ms).

## Proof item 3: write-path saving

The s1-wal method from #7749: 105,000-row sample reinserted into a
btree-only copy (GINs excluded; identical cost in both arms),
`CHECKPOINT` before each arm, `EXPLAIN (ANALYZE, WAL)`, arms alternated
twice. Near-identical across rounds (with-arm exact; without-arm ±1 record, ~7 KB):

| arm | WAL records | WAL bytes | insert time |
| --- | --- | --- | --- |
| with repo_idx, r1 / r2 | 1,276,238 / 1,276,238 | 144,551,874 / 144,551,874 | 1623 / 1570 ms |
| without, r1 / r2 | 1,169,951 / 1,169,950 | 136,739,106 / 136,731,994 | 1488 / 1476 ms |

Dropping the index saves about **1.01 WAL records and 74 bytes per
inserted row** (-8.3% records, -5.4% bytes on the btree-only copy; the
relative figure is smaller on production, where the GIN indexes add WAL
in both arms) and about 8% insert time. It mirrors the +1.06 records and
+109 bytes measured for adding the wider 164 index. The 82 MB production
size from the issue is freed on top.

## Proof item 4: the migration shape (blocked)

The wanted migration is the 102/106 shape: one file, one statement,
`DROP INDEX CONCURRENTLY IF EXISTS content_entities_repo_idx`, under the
next free sequence number (171; max shipped is 170). That file alone
fails `TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay`,
verified by replaying the test's exact create/drop regexes over the
migration tree: `content_entities_repo_idx` is created by
`004_content_store.sql` and dropped by the new file, so the rebuilt set
is non-empty while `replayRebuiltIndexNames` is empty. No
004-created index has ever been dropped; this would be the first.

Why no convergent shape exists while 004 creates the name:

- 004 is shipped and checksummed (manifest + golden digest). Editing out
  the CREATE breaks `applyTrackedDefinitions`' checksum guard ("checksum
  changed") on every database that recorded 004. Deleting 004 is not
  possible (it holds the content tables). A `checksum_alias.go` entry is
  explicitly refused for future edits.
- A guarded drop (the 128/129 `replayGuardedRedefinitions` shape) cannot
  converge for a pure removal: on the next untracked replay 004
  recreates the identical definition, so any catalog guard that fired
  once fires again. The 128 header proves this class: `IF NOT EXISTS`
  matches on name only, and a freed name is recreated.
- A same-name tombstone (for example `WHERE false`) to plug the name
  would converge, but it keeps a permanent junk catalog entry and a
  create of the dropped name, against the test's stated rule. Not
  proposed.
- The remaining path is a `replayRebuiltIndexNames` exemption: a
  recorded known defect. Tracked databases (production) converge
  cleanly — 004 is receipt-skipped and the drop applies once. Untracked
  databases (the local supervisor, which replays the full tree on every
  start in `applyLocalBootstrap`) would blocking-rebuild the index from
  004 (non-`CONCURRENTLY`) and drop it again on every restart, scaling
  with the local table. That exemption needs owner and review sign-off;
  it is not taken here.

Unblock options, in order: (a) the owner accepts the local-only churn
and the exemption lands with the drop plus a live no-rebuild proof for
the tracked path; (b) the bootstrap model gains a convergent
remove-a-004-index path; (c) the index stays and this review closes with
the plan evidence above.

## Shim limits

PG 16.15 versus production PG 18.3; 585k synthetic rows versus 2.24M
production rows; short synthetic `repo_id` values (repo_idx 4 MB here
versus 82 MB on production — plan shapes transfer, sizes do not);
default `random_page_cost` except S3/S4; the clone that proved 164 is a
different dataset. Production confirmation stays pending: after any drop
lands, run read-only `EXPLAIN (ANALYZE, BUFFERS)` of the big-repo
code-topic probe and the #6540 gate on the read replica.

## Hypothesis ledger

| candidate | cheapest proof | old | new | disposition |
| --- | --- | --- | --- | --- |
| Drop is plan-safe | shim battery before/after | 11/12 identical | Q7-big 461 to 242 ms, AND shapes survive | proven on the shim, production confirmation pending |
| Drop saves write cost | s1-wal WAL arms | baseline | -1.01 rec/row, -74 B/row | proven |
| Plain drop migration converges | replay-guard replay | guard green | guard red (004 recreates) | disproven; owner decision needed |

## Performance Evidence

Performance Evidence: warm buffers identical before/after dropping
`content_entities_repo_idx` for 11 of 12 probe queries; the code-topic
big-repo probe flips from an index scan (8,512 buffers, 460.7 ms) to a
seq scan (20,199 buffers, 241.8 ms), and its optimal bitmap-AND plan
(284 buffers, 6.4 ms, uncommitted knob-divergent probe, directional only)
is available without the index. Write path saves
1.01 WAL records and 74 bytes per row. No migration lands here; the
replay-guard analysis above blocks item 4 pending an owner decision.

## Observability Evidence

No-Observability-Change: this review adds one evidence note and no code,
index, worker, lease, route, metric, or runtime knob. Index use stays
visible through `pg_stat_user_indexes`; the touched reads keep their
existing `postgres.query` spans.

## Decision (2026-10-12, arbiter-eshu)

This supersedes the "needs an owner decision" lines in Verdict and Proof
item 4. Verdict: **(c) keep `content_entities_repo_idx` and close the
review with the documented reason.** No owner act is needed for (c).

Weight of evidence: items 1-3 support the drop on the shim but do not
clear production (shim PG 16.15 with 585k rows and a 4 MB index versus
production PG 18.3 with 2.24M rows and 82 MB; read-replica confirmation
never ran; Q7-big defaults to an O(corpus) seq scan; the 284-buffer
optimal bitmap-AND is note-reported only). Item 4 is mechanically
blocked (004 immutable per #7002; replay guard red; no guard converges;
tombstone rejected). Option (a) would record a known defect in
`replayRebuiltIndexNames` against its own comment and inflict
per-restart blocking rebuild plus drop on local databases; option (b)
is disproportionate platform work for 82 MB and about 5% WAL.

Close-out: keep the index. Revisit only if the bootstrap gains a retire
path or production evidence demands it. Raw battery inputs and outputs
are committed under `docs/internal/evidence/7759/`. Refs #7759.
