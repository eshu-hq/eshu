# #7140 projector: proof-driver (built binary) backlog proof of the shipped lock-first claim

Child of #8018. Follow-up to #7108 (lock-first rewrite), proving the shipped
claim at a real-size backlog with an operator-local proof driver — a built
binary linking the exact shipped `ProjectorQueue` code, not the ingester
binary itself — instead of EXPLAIN-only.

Note on the moot comparison: the issue text asks to compare the shipped SQL
with lock-first under identical inputs. Lock-first (#7108) and the per-scope
claim fence (#7115) both ship at this base — the claim SQL contains the
`claim_fence` lock step and the maintenance lock CTEs — so there is no
pre-lock-first shipped statement left to compare against. This note proves the
operator-local proof driver — linking the exact shipped `ProjectorQueue` claim
code, not the ingester binary — holds zero deadlocks and zero overlapping
leases at this scale instead, and says so explicitly.

## Setup

- Source HEAD `13bd60652` (== `origin/main` at the run), clean tree.
- Proof driver built from this worktree with `go build ./scratch7140proof`
  (operator-local, removed after the run, never committed). It links the same
  `ProjectorQueue.Claim/Ack/Fail/Heartbeat/Enqueue` code the ingester and
  bootstrap binaries link. Binary sha256
  `3242cfe9d4aabe340138521a16aa5f1b43969db6b22e557d47d953cd83932d7a`.
- Backend: Homebrew postgresql@18.6 on localhost, disposable database
  `proof7140` (dropped after the run). Prior evidence used postgres:16
  containers; no container runtime exists on this host, so PG18-local is the
  stated substitute. Full bootstrap applies cleanly on PG18 (196/196
  migrations, ~0.6 s).
- Backlog (bulk SQL, `ANALYZE`d): 20,000 scopes, 200,000 `scope_generations`
  (10/scope), 200,000 projector `fact_work_items` (1/generation, 59,000
  pending), 20,000 fence rows via the migration-130 trigger, plus bootstrap's
  own `eshu:global` genesis scope. Seed time 14 s. No reducer rows: the claim
  never reads them.
- Load (mirrors `TestProjectorClaimConcurrentLoadHasNoDeadlock`, which defaults
  to 40 scopes and widens via `ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_SCOPES`;
  the `1000` in that file is only an `lpad` zero-padding branch for scope-id
  formatting, not a scope cap): 8 workers — the production default
  (`ESHU_PROJECTOR_WORKERS` = NumCPU capped at 8; host has 10 CPUs) — with
  150 ms leases, a producer bursting 1–3 generations on uniform-random scopes
  over all 20,000 plus occasional #7130 refinalize rows on superseded
  generations, and the #7115 overlapping-lease auditor every 20 ms. Work mix
  per claim: 10% expire-stall then heartbeat+ack, 10% fail (retryable),
  80% ack. Fixed 720 s run.
- Conflict domain: projector queue rows and per-scope fence rows
  (`projector_scope_claim_fences`), with `ClaimSourceSystem` unset so the
  maintenance CTEs scan the full backlog (the `$4 = ''` shape from #7108).

## Verdict

PROOF HELD (binary exit 0; any nonzero invariant below fails the run by
construction).

| signal | measured |
| --- | --- |
| claims (non-vacuous, bar >= 300) | 638 |
| `pg_stat_database.deadlocks` delta | 0 (result.json `server_deadlocks`; driver's `pg_stat_database` read) |
| raw 40P01 escaping `Claim` | 0 |
| `ErrWorkClaimConflict` (retries exhausted) | 0 |
| other claim/ack/fail errors | 0 |
| distinct overlapping lease pairs (auditor) | 0 (no log lines; closing multi-lease query: 0 scopes) |
| `eshu_dp_queue_claim_conflict_retries_total` increments | none, by any `failure_class` (OTEL ManualReader sum empty) |
| claim latency, claimed subset (n=638) | p50 7.75 s, p95 16.32 s |
| claim latency, all attempts (n=646) | p50 7.77 s, p95 16.32 s |
| refinalize rows on superseded generations claimed (#7130) | 0 of 12,926 seeded |
| scopes with claimable work but no fence row | 0 |
| superseded-generation refusals (documented deferral) | 2 |

Warmup (excluded from percentiles): first cold claim 95.1 s, then 5.03 s and
5.00 s. Claim p50 7.75 s sits inside the 7–12 s band reported in the issue;
p95 16.3 s is above it on this non-isolated laptop host. No speedup or
regression is claimed: this run is a hold-proof, and the host was not quiet
enough for a timing comparison.

## Proof ladder record

Stage: `ProjectorQueue.Claim` with interleaved Ack/Fail/Heartbeat in the
operator-local proof driver (a built binary linking the shipped code, not the
ingester binary). Start event: `Claim()` invocation; terminal event: its return.
Invariants: zero server deadlocks, zero overlapping pairs, zero unexpected
errors, claims >= 300. Stop threshold: the first deadlock, overlap, or
unexpected error fails the proof (none occurred). Full ladder, timebox
(90 min wall, deadline met), and retention mode (destroy, done: database
dropped, driver removed) are in the operator-local run manifest, which is not
committed.

Performance Evidence: 720 s proof-driver (built binary) run at 20,000 scopes / 200,000
projector rows / 8 production-default workers; 638 claims; server deadlocks 0;
overlapping lease pairs 0; conflict-retry counter increments 0;
claim-latency p50 7.75 s p95 16.32 s; warmup cold 95.1 s then ~5 s;
`retired_claimed` 0; run log and `result.json` held operator-local.

Observability Evidence: server deadlocks read from
`pg_stat_database.deadlocks` (delta 0, per the driver's read recorded in
result.json `server_deadlocks`; no independent psql re-query); the #7115
auditor query ran every 20 ms for the full load (0 pairs) with the closing
`scopes_multi_lease_now` gauge-equivalent at 0 and `scopes_missing_fence` at 0;
per-attempt `Claim()` timings give the
`eshu_dp_queue_claim_duration_seconds{queue="projector"}`-equivalent p50/p95;
the OTEL-collected `eshu_dp_queue_claim_conflict_retries_total` stayed empty,
so an operator watching that counter and the Postgres deadlock log would have
seen nothing to investigate. In production the same signals are
`eshu_dp_projector_scopes_multiple_live_leases` (should read 0) and the
5-minute gauge snapshot.

## Residual notes

- PG18-local stands in for the postgres:16 container the earlier evidence
  used; SQL is standard and bootstrap applies cleanly, but a reviewer wanting
  the exact container still needs a host with a container runtime.
- The driver (worker/producer/auditor shapes plus pre-seed, percentiles, and
  retry-counter collection) was deliberately not committed; the durable
  harness remains `TestProjectorClaimConcurrentLoadHasNoDeadlock`.
- No production-code change was made or needed.
- Full ingester deployment behavior (full deployment with Neo4j etc.) is
  NOT_CHECKED: the subject that ran was the proof driver, not the ingester
  binary.
