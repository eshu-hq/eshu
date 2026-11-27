# Drain watch: fresh read-only QA census, 2026-11-27 ~19:10Z

Refs eshu-hq/eshu#10728. Run `qa-canary-20261119`. All reads against the
QA Postgres read replica via `kubectl -n eshu exec eshu-postgresql-read-0`
(psql in-pod, `eshu` role, password from the pod-mounted secret file),
inside one `BEGIN READ ONLY` transaction with
`SET LOCAL statement_timeout='15s'`. No writes, no deploys, no code
changes. Every query returned inside the 15s budget; transaction closed
with `ROLLBACK`.

Sampled at **2026-11-27 19:10:09Z**.

Supersedes `10728-drain-watch-qa-2026-11-26-census.md`
(2026-11-26 ~02:16Z + ~02:40Z amendment).

## Summary

We re-measured whether the NVD baseline years are complete and whether the
supply-chain queue is draining on QA. No year is complete and the dead-letter
total rose by 44, so the year-complete watch stays NOT READY and the Pin-1
zero-delta bar stays unmet.

## 1. Year-complete watch

Checkpoints (`vulnerability_nvd_baseline_checkpoints`), run
`qa-canary-20261119`:

| year | resume_utc | year_complete | records | updated_at |
|---|---|---|---|---|
| 2024 | 2026-06-17 06:52:56 | f | 0/0 | 2026-11-21 09:53:54Z |
| 2025 | 2026-06-17 08:26:18 | f | 0/0 | 2026-11-20 13:53:57Z |
| 2026 | 2026-06-17 10:36:25 | f | 0/0 | 2026-11-26 12:56:25Z |

Notes:

- `resume_cve_id` is empty for all three years; the cursor is
  `resume_unix_micro` (rendered above as `resume_utc`).
- The 2026 resume cursor advanced 10:27:44 -> 10:36:25 UTC and
  `updated_at` advanced 2026-11-25 06:43:42Z -> 2026-11-26 12:56:25Z:
  the drain is still moving.
- The table carries no `drained_at` / `projecting` columns; the closest
  freshness stamps are `updated_at` above plus `last_scope_id`
  (`vuln-intel://nvd/_baseline/qa-canary-20261119/<year>/<mint>`).
- No year reports complete. Issue stays open.

## 2. `supply_chain_impact` reducer queue (`fact_work_items`, domain filter)

| status | n (2026-11-27) | n (task baseline) |
|---|---|---|
| pending | 6,965 | 6,523 |
| retrying | 997 | 1,774 |
| claimed | 8 | 10 |
| dead_letter | 34 | — |
| succeeded | 2,854 | — |
| superseded | 2,097 | — |

Open (`claimed + pending + retrying`): **7,970** vs baseline **8,307**
(-337). Mix shifted: pending +442, retrying -777, claimed -2.

## 3. All-domain queue totals (`fact_work_items`, all stages/domains)

| status | n (2026-11-27) | n (2026-11-26) |
|---|---|---|
| pending | 7,799 | 8,917 |
| retrying | 4,255 | 2,690 |
| claimed | 16 | 22 |
| running | 5 | 3 |
| dead_letter | 3,641 | 3,597 |
| succeeded | 254,055 | 254,304 |
| superseded | 9,646 | 9,605 |

All-domain `dead_letter` total: **3,641** (+44 in ~41h).

## 4. Trip latch (`vulnerability_nvd_baseline_trip_latches`)

Run `qa-canary-20261119`: `open=f`, `consecutive_high=0`,
`soft_quiet_streak=26` (was 16), `page=f`,
`re_enable_eligible=t` (`re_enable_samples=26`), `revision=157`,
sampled 2026-11-27 18:10:06Z. Quiet — drain gate open.

## Verdict: NOT READY

- No year meets `year_complete` (all three `f`).
- Pin-1 bar (`dead_letter` delta 0) NOT MET: all-domain dead_letter
  3,597 -> 3,641 (+44). Storm bound #11595 remains in force.
- Open sci backlog drained 8,307 -> 7,970 but remains large; no close.
