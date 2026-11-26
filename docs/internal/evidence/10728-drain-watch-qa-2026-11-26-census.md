# Drain watch: QA read-only measurements, 2026-11-26 ~02:16Z

Refs #10728. Run `qa-canary-20261119`. All reads against the QA Postgres
read replica (`default_transaction_read_only=on`, per-statement
`statement_timeout='15s'`), via port-forward of the in-cluster read
service. Authenticated as the `eshu` role with the cluster-managed
read secret. No writes, no deploys, no code changes. Every query below
returned inside the 15s budget.

Supersedes the 2026-11-24 ~22:35Z fallback figures carried in
`10728-drain-watch-qa-2026-11-24.md` and the same-day note.

## 1. Reducer queue (fact_work_items)

| scope | status | n (2026-11-26) | n (2026-11-24) |
|---|---|---|---|
| all reducer domains | succeeded | 254,304 | 226,537 |
| all reducer domains | pending | 8,917 | 8,533 |
| all reducer domains | superseded | 9,605 | 8,484 |
| all reducer domains | retrying | 2,690 | 1,882 |
| all reducer domains | dead_letter | 3,597 | 958 |
| all reducer domains | claimed | 22 | 16 |
| all reducer domains | running | 3 | 8 |
| supply_chain_impact only | pending | 8,270 | 8,248 |
| supply_chain_impact only | superseded | 1,962 | 1,740 |
| supply_chain_impact only | succeeded | 1,731 | 1,581 |
| supply_chain_impact only | retrying | 620 | 236 |
| supply_chain_impact only | dead_letter | 42 | 30 |
| supply_chain_impact only | claimed | 12 | 10 |

Baseline-scoped items (`scope_id LIKE '%/_baseline/%'`): projector 8
succeeded + 1 retrying (was 7 + 1); reducer 8 succeeded, 0 pending
(was 5 pending + 2 succeeded). The previously draining 2026 chunk has
cleared the reducer.

Oldest open fact item: pending created 2026-10-22 (~35d);
retrying oldest 2026-10-23 (~34d); claimed oldest 2026-11-10 (~16d).

## 2. Year-complete watch

Checkpoints (`vulnerability_nvd_baseline_checkpoints`), run
`qa-canary-20261119`:

| year | resume_utc | year_complete | records | updated_at |
|---|---|---|---|---|
| 2024 | 2026-06-17 06:52:56 | f | 0/0 | 2026-11-21 09:53:54Z |
| 2025 | 2026-06-17 08:26:18 | f | 0/0 | 2026-11-20 13:53:57Z |
| 2026 | 2026-06-17 10:27:44 | f | 0/0 | 2026-11-25 06:43:42Z |

The 2026 checkpoint advanced 2026-11-25 06:43:42Z (drain is moving).
No year reports complete. Issue stays open.

Trip latch (`vulnerability_nvd_baseline_trip_latches`): `open=f`,
`consecutive_high=0`, `soft_quiet_streak=16` (was 5), sampled
2026-11-26 01:18:59Z -- quiet, drain gate open.

## 3. "How much of NVD do we hold" (15s-compliant count query)

Recorded query (index-only scan on
`fact_records_vulnerability_cve_active_scan_idx`):

```sql
SET statement_timeout='15s';
SELECT count(*) FROM fact_records
WHERE fact_kind='vulnerability.cve' AND is_tombstone=false;
-- 190137
```

- Active `vulnerability.cve` fact rows held: **190,137** (was 183,549).
- Baseline contribution (`AND scope_id LIKE '%/_baseline/%'`): **5,638**
  (was 3,366).
- NVD published total at read time
  (`services.nvd.nist.gov/rest/json/cves/2.0?resultsPerPage=1`,
  2026-11-26T02:16Z): **totalResults=432,517** (was 431,756).
- Row-based hold ratio: 190,137 / 432,517 ~= 44.0% (was ~42.5%).

Close-readiness verdict: NOT READY. No year meets `year_complete`.
