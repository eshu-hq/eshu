# Security-alert target set per generation (#8838)

**A security-alert generation now records the repositories it collected, and
an empty answer for a repository outside the latest committed generation's set
reads `no_committed_generation` instead of `complete`.**

## Problem

The empty-answer read (#8803) took target membership from the current
collector configuration and committed state from "any committed generation".
A repository added to an allowlist, or a repository target whose `scope_id` was
reused for another repository, inherited the existing generation and read
`complete`, exact and fresh, before the collector had read it.

A second cause kept the window open for good in one case. The freshness hint
covered only the provider answer. When the added repository had no alerts, the
next claim produced the same hint, so `backfill.DecideUnchangedGeneration`
skipped the generation as unchanged. No generation would ever record the new
repository. `TestSecurityAlertTargetSetAllowlistChangeLive` proves the skip:
an equal-hint commit with a new set leaves one generation row.

## Finding against arbiter-secalert2 Q2

Q2 put the target set in the generation payload and did not touch the
freshness digest. Payload-only cannot close the window: the ingestion store
decides whether to store a generation from the hint alone, before the payload
is written, so an allowlist change that leaves the provider answer unchanged
never reaches the payload. This change adds the set's hash to the digest to
carry out Q2's intent. The coordinator accepted it as implementing Q2, with no
new ruling.

`TestTargetSetDrivesTheUnchangedGenerationDecision` feeds collected generations
through `backfill.DecideUnchangedGeneration`:

- An unchanged allowlist still skips every cycle, so there is no churn.
- Adding or removing a repository with no alerts stores a new generation.
- `TestRepositoryTargetScopeIDReuseStoresANewGeneration` covers a reused
  repository `scope_id`.
- `TestTargetSetMovesTheHintOnceAtUpgrade` pins the one extra generation per
  target after the upgrade.

With the set removed from the digest, every store case returns skip.

## Change

- `scope.SecurityAlertTargetSet` is the sorted, deduplicated slug set, kept as
  canonical JSON so `ScopeGeneration` stays comparable.
- The collector sets it on each generation: `[repository]` for a repository
  target, or the allowlisted repositories of the organization for an org
  target. The set's hash enters the freshness digest, so every target commits
  one extra generation after the upgrade.
- The ingestion store writes `security_alert_target_set` and
  `security_alert_target_set_hash` to `scope_generations.payload`. There is no
  DDL.
- The collection-state read replaces the committed `EXISTS` with a top-1
  lateral over the latest committed generation and returns the requested slugs
  missing from its set. It reads the set only for an anchored read. The classifier treats a target as uncommitted for the
  read when a slug the target covers is missing.
- A generation that recorded no set keeps the earlier answer.

## Known limit (pre-existing)

An anchored request that names several repositories takes the best covering
target. It can read `complete` while another requested repository's target is
still collecting. main behaves the same; this change does not alter it.

## Performance Evidence

Performance Evidence: an `EXPLAIN (ANALYZE, BUFFERS)` shim on PostgreSQL 18.6
(the CI image) with origin/main's migrations applied (through 232, without the
#8840 index). The data was 1,024 security-alert
targets with 2,000 committed generations each, plus 3 pending or failed
generations at the head of every fourth target, plus 20,000 git scopes with 50
generations each: 3,048,769 `scope_generations` rows, vacuumed and analyzed.
The new lateral walks `scope_generations_scope_latest_lookup_idx` newest first.
It removes at most 3 rows by filter and stops at the first committed row.

| Read | Before (EXISTS) | After (lateral) |
| --- | --- | --- |
| 1 target, warm | 18 buffers, 0.160 ms | 21 buffers, 0.105 ms |
| 1,024 targets, warm | 10,741 buffers, 19.032 ms | 12,522 buffers, 20.168 ms |

Those rows use 50-slug sets, which fit inline. The added cost there is 1,781
buffers for 1,024 targets, about 1.7 per target: one heap fetch of the
committed row. It does not grow with generation history. The first cold run of
each query is not comparable, because the earlier query warmed the shared
work-item and scope pages for the later one. With migration 233 (#9051) also
applied, the same delta held: 10,581 to 12,362 buffers.

### Large target sets

The payload is one TOASTed value, so reading the set decompresses all of it.
The worst case was re-seeded on the same table: every target's latest
generation holds 5,000 incompressible (md5-derived) slugs, 82,375 bytes stored
and 134,659 bytes as text. The table below is the shipped query text, printed
from the constant and run as a prepared statement, third warm run.

| Read | main (EXISTS) | This change |
| --- | --- | --- |
| Unanchored, 1,024 targets | 17,555 buffers, 19.878 ms | 18,344 buffers, 19.752 ms |
| Unanchored, 1 target | 18 buffers, 0.103 ms | 21 buffers, 0.103 ms |
| Anchored, 1 covering target | 18 buffers, 0.093 ms | 34 buffers, 0.341 ms |
| Anchored, all 1,024 targets covering | 11,754 buffers, 19.179 ms | 25,841 buffers, 227.590 ms |

An unanchored read never touches the set: a `CASE` on `cardinality($2)` skips
it. Without the guard, review measured the unanchored read at 165.7 ms, and
this table measured it at 210.524 ms. An anchored read pays about 14 buffers
and 0.2 ms per covering target. Only targets that cover a requested repository
are read, usually one or two. Containment per slug
(`payload @> jsonb_build_object(...)`) was slower: 52,465 buffers and
516.102 ms for 1,024 covering targets, because it decompresses once per slug.
A compressible 5,000-slug set (22,660 bytes stored) reads in 0.34 ms for one
target.

The read runs only on an empty answer, inside the existing 1 s probe deadline.
Pages with rows skip it.

## Observability

Observability Evidence: no new signal. The existing
`eshu_dp_query_security_alert_empty_answers_total{coverage_state, reason}`
counter and the `security_alert_collection_state` stage log
(`collector_state=collecting`) report the new outcome as
`target_incomplete` / `no_committed_generation`. Neither carries a repository
name. The payload holds slugs the collector already stores in its facts.
