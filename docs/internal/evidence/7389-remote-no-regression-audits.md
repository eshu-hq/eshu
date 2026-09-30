# #7389 remote runs: pair 2 audits and the ingester slot control

This file carries the audits behind the pair 2 verdict in
[7389-remote-no-regression.md](7389-remote-no-regression.md). The design and
the local proofs are in
[7389-superseded-writer-overlay.md](7389-superseded-writer-overlay.md). Labels
follow the parent file: **measured**, **derived**, **reported**. Sources are
the operator-local pair 2 and slot-control archives listed there.

## Postgres: +22% CPU and +17.8% statement time on HEAD

Arbiter disposition: settled. The difference is a timing difference (the
reducer's sweep phase), not the seeded workload and not the change. Per-unit
costs match.

**Mechanism.** The search-document sweeper enqueues work for a scope's active
generation every 30 s (`defaultSearchDocumentSweepInterval`). Sweeps
completed at about :12 past each 30 s on HEAD and :06 on BASE (measured;
interval p50 30.007 s on both). Bursts repeat every 300 s, a multiple of
30 s, so the phase is fixed within a run. A superseded G_B generation gets a
search-document item only when a sweep lands in its 7.5-16.5 s active window
(G_B activated → heal activated). That happened for 4 of 5 G_B on HEAD and 1
of 5 on BASE, and it matches all 10 bursts one to one (measured).

| burst | HEAD active window | HEAD G_B item | BASE active window | BASE G_B item |
| --- | --- | --- | --- | --- |
| 1 | 8.296 s, no sweep | none | 8.393 s, no sweep | none |
| 2 | 10.403 s, sweep at +6.1 s | 45,002 docs, 107.2 s | 9.608 s, no sweep | none |
| 3 | 7.569 s, sweep at +6.0 s | 44,462 docs, 78.8 s | 7.495 s, no sweep | none |
| 4 | 8.078 s, sweep at +4.1 s | 85,801 docs, 289.1 s | 16.505 s, no sweep | none |
| 5 | 8.084 s, sweep at +6.0 s | 85,005 docs, 201.0 s | 15.838 s, sweep | 85,005 docs, 105.7 s |

Windows are derived from measured activation times; items are measured. Each
HEAD item was claimed 1.3-3.6 s before its heal generation activated
(derived), so these are timing effects. No item was claimed after its heal
activated.

**Attribution.** `pg_stat_statements` counts nested FK checks both in their
own row and inside their parent statement (track all), so the comparison uses
top-level time:

| component | derived value |
| --- | --- |
| top-level statement delta, HEAD − BASE (raw delta including nested FK: +725.2 s) | +614.2 s |
| extra superseded-G_B search-document cycles (HEAD 4 items, 668.8 s of sub-phase statement time; BASE 1 item, 103.2 s) | +565.6 s |
| other search-document cycles: extra sweep-captured cycles, plus the slowdown of heal cycles that overlapped a G_B item on the same scope | +67.4 s |
| near-miss completed projections | 0 (both sides completed all 5 large projections) |
| **residual** | **−18.8 s (−3.1% of the delta, under the 10% bar)** |

**Unit costs** match within 3% (measured statement time per 1,000 search
documents written; 1,548,107 on HEAD vs 1,344,802 on BASE):

| statement | HEAD | BASE |
| --- | --- | --- |
| `copy eshu_search_index_terms` | 1,240.7 ms | 1,265.9 ms |
| `INSERT eshu_search_index_documents` | 90.1 ms | 91.7 ms |
| `INSERT fact_records` | 181.1 ms | 186.1 ms |
| FK `FOR KEY SHARE ingestion_scopes` (nested) | 648.1 ms | 663.5 ms |

**The 19× `DELETE eshu_search_index_documents`** (126.6 s over 586 calls vs
6.7 s over 568, measured) comes from two calls, both HEAD G_B cycles: 90.74 s
(burst 4) and 24.87 s (burst 2). The median call is 3.5 ms vs 3.2 ms, so not
every call slowed.

**`DELETE fact_records`** (523.5 s vs 284.8 s): each retire on a large repo
cycle costs about 91-92 s. HEAD has 4 such retires and BASE has 2; the 2
extra are HEAD G_B cycles (measured).

**The change's own statements** total 1.3 s of HEAD statement time. Per
activated generation (589 on both sides):
- marker: 1.000 calls, 0.10 ms;
- probe: 95.6 calls, 2.08 ms (100 per collector cycle);
- heartbeat supersede: 0.015 calls on HEAD vs 0.007 on BASE, under 1 ms in
  total;
- refusal: 1 per race on both sides.

No statement of the change runs more often per generation on HEAD than a
BASE statement it replaces (measured, derived).

**Postgres CPU tracks statement time one to one:** +716 core-s during the
mutation phase against +725.2 s of statement time. Core-seconds per top-level
statement second were 1.085 on HEAD and 1.064 on BASE (derived). The slot
control run corroborates this: its sweep landed at about :22 and ran 4 of 4
superseded-G_B cycles, and its Postgres CPU was 3,913 core-s (cgroup series
3,944), against HEAD 4,061 and BASE 3,345 in pair 2 (measured). BASE code
running the same redundant cycles uses HEAD-level Postgres CPU.

**Per-service CPU and memory, pair 2** (mutation phase, 5 s sampler):

| service | HEAD mean CPU / peak / peak mem | BASE mean CPU / peak / peak mem |
| --- | --- | --- |
| postgres | 229.3% / 712.3% / 5.46 GiB | 187.9% / 684.0% / 5.43 GiB |
| ingester | 124.6% / 1094.2% / 0.95 GiB | 96.9% / 988.6% / 1.06 GiB |
| neo4j | 67.9% / 343.7% / 10.29 GiB | 69.4% / 388.0% / 10.01 GiB |
| resolution-engine | 30.4% / 356.1% / 0.40 GiB | 31.7% / 363.0% / 0.42 GiB |
| projector | 1.4% / 49.9% / 0.50 GiB | 0.6% / 25.1% / 0.49 GiB |

## Lock-timeout cancels on Ack's scope activation

Arbiter disposition: closed as Ack-only retries.

| side | burst | cancels | G_B activated | same-scope burst-b collector commit finished |
| --- | --- | --- | --- | --- |
| HEAD | 1 | 2 | 00:31:44.158Z | 00:31:44.156Z |
| HEAD | 2 | 2 | 00:36:35.760Z | 00:36:35.758Z |
| HEAD | 3 | 1 | 00:41:35.923Z | 00:41:35.895Z |
| HEAD | 4 | 2 | 00:46:37.860Z | 00:46:37.818Z |
| HEAD | 5 | 2 | 00:51:36.069Z | 00:51:36.068Z |
| BASE | 1 | 2 | 01:15:13.005Z | 01:15:13.000Z |
| BASE | 2 | 1 | 01:20:08.656Z | 01:20:08.655Z |
| BASE | 3 | 1 | 01:25:08.695Z | 01:25:08.693Z |

All values are measured.

- **Counts:** 9 cancels on HEAD (0.0153 per activated generation) and 4 on
  BASE (0.0068), derived.
- **Retries only:** every cancel was a retry of the Ack UPDATE that succeeded
  within the same attempt. Each G_B projection ran exactly once, so there
  was no re-projection.
- **Holder:** by timing, the holder is the same-scope burst-b collector
  commit (an upsert of 44,009 facts). G_B activated 2-42 ms after it
  finished in all 8 cases; the holder was not matched by process id.
- **Why BASE had fewer:** in BASE bursts 4 and 5, G_B's Ack ran before the
  collector transaction held the scope row. That is timing.
- **Not the marker or heartbeat:** no cancel coincides with the marker, which
  commits in its own short transaction about 15 s earlier. None coincides
  with the heartbeat either, whose scope lock is SKIP LOCKED.

## Ingester: +29% CPU on HEAD, and the slot control

Arbiter disposition: closed under the slot rule.

**Artifact test (pair 2 only; measured, ratios derived).**
- Outside the superseded-G_B cycle windows, over the same T0-relative mask on
  both sides, HEAD ingester core-s per collector commit was 4.068 vs 2.913
  (ratio 1.396). Over the whole mutation phase it was 3.727 vs 2.857 per
  commit (1.304) and 4.753 vs 3.431 per projection (1.385).
- A per-sample regression of ingester on Postgres CPU gave slopes of −0.263
  (HEAD) and −0.087 (BASE), with a pooled HEAD offset of +0.347 cores. So the
  ingester delta did not follow Postgres load.
- The mid-phase 30 s profiles were about 95% collector parsing. The change's
  collector code (`UncoveredProjectionWriters`) showed 0.01-0.02 s per 30 s
  profile.
- The host has 1 thread per core (measured, `lscpu`), so SMT contention is
  ruled out.

**Slot control.** One BASE run (`7389-promo3-base-20260930T022256Z`, after
preflight `7389-promo3-pre-20260930T021358Z`) ran in the FIRST slot with the
identical schedule, payload, image, gitconfig and knobs. Its `mutations.tsv`
matches pair 2 BASE on kind, repo, commit SHA, counts and seq (measured,
`diff` rc 0). Ingester core-s over the mutation phase, same sampler method as
pair 2:

| metric | base, slot 1 | head, slot 1 (pair 2) | base, slot 2 (pair 2) | base(s1) / head(s1) | base(s1) / base(s2) |
| --- | --- | --- | --- | --- | --- |
| per collector commit | 3.586 | 3.727 | 2.857 | 0.962 | 1.255 |
| per ingester projection | 4.530 | 4.753 | 3.431 | 0.953 | 1.320 |

Values are measured and ratios derived. Base in slot 1 is within 10% of head
in slot 1, so the difference is the run slot: **settled**. At equal slot,
head/base is 1.040 per commit and 1.049 per projection (derived), both
within the ≤ 1.10 pass rule.

Slot-control health (measured): 4 refusals, 0 ack refusals, 0 deadlocks, 0
heartbeat ERROR, 0 stuck rows, graph clean after all 6 bursts. Drain was
19.974 s (19.974s) from the last commit to the DB terminal. The box used
35m25.145s of 120 minutes.

The run also captured a per-second cgroup CPU series for five services and
30 back-to-back 58 s CPU profiles each for the ingester and the projector,
covering the whole mutation phase. Continuous profiling adds a small overhead
to that run only.

## Stale-scope reclaim check (base drift #7454)

#7454 changed the value written to `failure_details` by the stale-scope reclaim
CTEs of the projector claim statement (`reclaimed_stale_projector_duplicates`
and `reclaimed_claim_siblings`). The question is whether any measured run
exercised that path. The reclaim sets `failure_class =
'projector_stale_scope_reclaim'` on a projector row whose lease expired while a
live sibling claim exists; no Go caller logs it, and a successful Ack clears
the class, so the row readback cannot see a reclaimed row that later succeeded.

| run | rows with the reclaim class at drain terminal | expired claims at terminal | log mentions (class, message, claim rejected) | container restarts |
| --- | --- | --- | --- | --- |
| pair 1 head | 0 (measured) | 0 | 0 / 0 / 0 | 0 |
| pair 1 base | 0 (measured) | 0 | 0 / 0 / 0 | 0 |
| pair 1 aborted head attempt | NOT_CHECKED (aborted before any readback) | NOT_CHECKED | 0 / 0 / 0 | not read |
| pair 2 head | 0 (measured) | 0 | 0 / 0 / 0 | 0 |
| pair 2 base | 0 (measured) | 0 | 0 / 0 / 0 | 0 |
| slot control base | 0 (measured) | 0 | 0 / 0 / 0 | 0 |

Restarts were 0 for all 50 containers of the five timed runs (measured). The
replay probe stopped and recreated the ingester and projector only after the
drain terminal, with an empty queue, so it left no claimed row to reclaim. The
log searches are corroboration only, because the reclaim emits no log line.
NOT_CHECKED: projector logs from launch to T0 for pairs 1 and 2, because the
probe recreated the projector container afterwards (the T0 to drain-terminal
window was captured); the slot control has the complete projector log. No measured run shows evidence of the
stale-scope reclaim path within those limits.
